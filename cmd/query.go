package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/pkg/types"
	"github.com/commit0-dev/commit0-cli/sdk"
)

var queryCmd = &cobra.Command{
	Use:   "query <question>",
	Short: "Semantic code search — uses agent when available",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		noAgent, _ := cmd.Flags().GetBool("no-agent")

		c := sdk.New(serverURL(cmd))

		// Agent mode: multi-tool investigation with streaming output.
		if !noAgent {
			return runAgentQuery(cmd, c, args[0], repoSlug)
		}

		// Fallback: direct query service.
		return runDirectQuery(cmd, c, args[0], repoSlug)
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
	queryCmd.Flags().String("repo", "", "Repository slug to search")
	queryCmd.Flags().Int("top-k", 10, "Number of results (direct mode only)")
	queryCmd.Flags().String("kind", "", "Filter by node kind: function, class, file, module (comma-separated)")
	queryCmd.Flags().Bool("no-agent", false, "Skip agent, use direct search only")
	queryCmd.Flags().Bool("no-explain", false, "Skip LLM explanation (faster, table only)")
	queryCmd.Flags().String("file", "", "Filter results to this file/directory path prefix")
}

const maxAgentRetries = 2

// runAgentQuery runs the agent loop via SSE streaming.
func runAgentQuery(cmd *cobra.Command, c *sdk.Client, question, repoSlug string) error {
	fmt.Fprintf(os.Stderr, gray("Agent investigating: %q\n\n"), question)

	message := question
	for attempt := 0; attempt <= maxAgentRetries; attempt++ {
		reported, err := runAgentTurn(cmd, c, message, repoSlug)
		if err != nil {
			return err
		}
		if reported {
			return nil
		}
		fmt.Fprintln(os.Stderr, gray("  ◆ Formatting report..."))
		message = "You have completed your analysis but forgot to present it. Call write_report now with your findings structured into sections."
	}

	fmt.Fprintln(os.Stderr, yellow("  Agent did not produce a structured report."))
	return nil
}

// runAgentTurn executes a single agent turn via SSE and streams events.
func runAgentTurn(cmd *cobra.Command, c *sdk.Client, message, repoSlug string) (bool, error) {
	events, err := c.AgentChat(cmd.Context(), sdk.AgentChatRequest{
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
func runDirectQuery(cmd *cobra.Command, c *sdk.Client, question, repoSlug string) error {
	topK, _ := cmd.Flags().GetInt("top-k")
	kindFlag, _ := cmd.Flags().GetString("kind")
	noExplain, _ := cmd.Flags().GetBool("no-explain")
	filePath, _ := cmd.Flags().GetString("file")

	var nodeKinds []string
	if kindFlag != "" {
		for _, k := range strings.Split(kindFlag, ",") {
			nodeKinds = append(nodeKinds, strings.TrimSpace(k))
		}
	}

	result, err := c.Query(cmd.Context(), sdk.QueryRequest{
		Question:  question,
		RepoSlug:  repoSlug,
		TopK:      topK,
		NoExplain: noExplain,
		NodeKinds: nodeKinds,
		FilePath:  filePath,
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

// Ensure domain import is used (for future NodeKinds filter).
var _ = types.ErrNotFound
