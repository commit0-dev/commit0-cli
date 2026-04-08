package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

var queryCmd = &cobra.Command{
	Use:   "query <question>",
	Short: "Semantic code search — uses agent when available",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		repoSlug, _ := cmd.Flags().GetString("repo")
		noAgent, _ := cmd.Flags().GetBool("no-agent")

		// Wire all services from a single shared deps instance.
		svcs, err := wireServeServices(cmd.Context(), cfg)
		if err != nil {
			return fmt.Errorf("wire services: %w", err)
		}
		defer svcs.cleanup()

		// Agent mode: multi-tool investigation with streaming output.
		if !noAgent && svcs.agent != nil {
			return runAgentQuery(cmd, svcs.agent, args[0], repoSlug)
		}

		// Fallback: direct query service.
		return runDirectQuery(cmd, svcs.query, args[0], repoSlug)
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
	queryCmd.Flags().String("repo", "", "Repository slug to search")
	queryCmd.Flags().Int("top-k", 10, "Number of results (direct mode only)")
	queryCmd.Flags().String("kind", "", "Filter by node kind: function, class, file, module (comma-separated)")
	queryCmd.Flags().Bool("no-agent", false, "Skip agent, use direct search only")
}

const maxAgentRetries = 2

// runAgentQuery runs the agent loop. If the agent doesn't call write_report,
// it gets a follow-up message asking it to present findings — same session,
// so context is preserved. Max 2 retries before giving up.
func runAgentQuery(cmd *cobra.Command, agentRunner domain.AgentRunner, question, repoSlug string) error {
	fmt.Fprintf(os.Stderr, gray("Agent investigating: %q\n\n"), question)

	message := question
	for attempt := 0; attempt <= maxAgentRetries; attempt++ {
		reported, err := runAgentTurn(cmd, agentRunner, message, repoSlug)
		if err != nil {
			return err
		}
		if reported {
			return nil
		}
		// Agent didn't call write_report — nudge it to retry.
		fmt.Fprintln(os.Stderr, gray("  ◆ Formatting report..."))
		message = "You have completed your analysis but forgot to present it. Call write_report now with your findings structured into sections."
	}

	// Exhausted retries — agent never called write_report.
	fmt.Fprintln(os.Stderr, yellow("  Agent did not produce a structured report."))
	return nil
}

// runAgentTurn executes a single agent turn (Chat call) and streams events.
// Returns true if write_report was called (report rendered).
func runAgentTurn(cmd *cobra.Command, agentRunner domain.AgentRunner, message, repoSlug string) (bool, error) {
	events, err := agentRunner.Chat(cmd.Context(), domain.ChatRequest{
		Message:  message,
		RepoSlug: repoSlug,
	})
	if err != nil {
		return false, fmt.Errorf("agent: %w", err)
	}

	reported := false
	suppressNextResult := false

	for event := range events {
		switch event.Type {
		case "thinking":
			fmt.Fprintf(os.Stderr, gray("  ◆ %s\n"), event.Content)

		case "tool_call":
			if event.ToolName == "write_report" {
				fmt.Fprintln(os.Stderr, cyan("  ▶ write_report"))
				renderReport(event.Content)
				reported = true
				suppressNextResult = true
				continue
			}
			fmt.Fprintf(os.Stderr, cyan("  ▶ %s"), event.ToolName)
			args := event.Content
			if len(args) > 100 {
				args = args[:100] + "..."
			}
			fmt.Fprintf(os.Stderr, gray(" %s\n"), args)

		case "tool_result":
			if suppressNextResult {
				suppressNextResult = false
				continue
			}
			content := event.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Fprintf(os.Stderr, gray("    ← %s\n"), content)

		case "message":
			// Suppress raw text — agent should use write_report.

		case "error":
			fmt.Fprintf(os.Stderr, "\n"+bold("Error: %s")+"\n", event.Content)

		case "done":
			fmt.Println()
		}
	}

	return reported, nil
}

// runDirectQuery does a simple search + explain via the QueryService.
func runDirectQuery(cmd *cobra.Command, svc *app.QueryService, question, repoSlug string) error {
	topK, _ := cmd.Flags().GetInt("top-k")
	kindFlag, _ := cmd.Flags().GetString("kind")

	var nodeKinds []types.NodeKind
	if kindFlag != "" {
		for _, k := range strings.Split(kindFlag, ",") {
			nodeKinds = append(nodeKinds, types.NodeKind(strings.TrimSpace(k)))
		}
	}

	result, err := svc.Query(cmd.Context(), app.QueryRequest{
		Question:  question,
		RepoSlug:  repoSlug,
		TopK:      topK,
		NodeKinds: nodeKinds,
	})
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}

	fmt.Printf(bold("Found %d results")+" %s\n\n",
		len(result.Nodes), gray(fmt.Sprintf("embed:%dms search:%dms explain:%dms",
			result.Timing.EmbedMS, result.Timing.SearchMS, result.Timing.ExplainMS)))

	tbl := tablewriter.NewTable(os.Stdout,
		tablewriter.WithHeaderAlignment(tw.AlignLeft),
		tablewriter.WithConfig(tablewriter.Config{
			Row: tw.CellConfig{
				Formatting: tw.CellFormatting{AutoWrap: tw.WrapNone},
			},
		}),
	)
	tbl.Header([]string{"#", "Kind", "Qualified Name", "Location", "Score"})
	for i, node := range result.Nodes {
		n := node.Node
		var kind, name, location string
		switch n.Kind {
		case types.NodeModule:
			version := ""
			if n.Docstring != "" {
				version = " " + n.Docstring
			}
			kind = "MODULE"
			name = n.Name + version
			location = fmt.Sprintf("import %q", n.Qualified)
		default:
			kind = strings.ToUpper(string(n.Kind))
			name = n.Qualified
			location = fmt.Sprintf("%s:%d", n.FilePath, n.StartLine)
		}
		tbl.Append([]string{ //nolint:errcheck
			fmt.Sprintf("%d", i+1),
			kind,
			name,
			location,
			fmt.Sprintf("%.3f", node.FusedScore),
		})
	}
	tbl.Render() //nolint:errcheck

	if result.Explanation != "" {
		fmt.Println(cyan("─────────────────────────────────────"))
		fmt.Println(result.Explanation)
	}
	return nil
}
