package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/pkg/types"
)

var queryCmd = &cobra.Command{
	Use:   "query <question>",
	Short: "Semantic code search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		repoSlug, _ := cmd.Flags().GetString("repo")
		topK, _ := cmd.Flags().GetInt("top-k")
		kindFlag, _ := cmd.Flags().GetString("kind")

		var nodeKinds []types.NodeKind
		if kindFlag != "" {
			for _, k := range strings.Split(kindFlag, ",") {
				nodeKinds = append(nodeKinds, types.NodeKind(strings.TrimSpace(k)))
			}
		}

		svc, cleanup, err := wireQueryService(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer cleanup()

		result, err := svc.Query(cmd.Context(), app.QueryRequest{
			Question:  args[0],
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

		for i, node := range result.Nodes {
			n := node.Node
			switch n.Kind {
			case types.NodeModule:
				version := ""
				if n.Docstring != "" {
					version = " " + n.Docstring
				}
				fmt.Printf("%s %s %s\n   %s\n\n",
					gray(fmt.Sprintf("%d.", i+1)),
					kindBadge("MODULE"),
					bold(n.Name+version),
					gray(fmt.Sprintf("import %q  score:%s", n.Qualified, yellow(fmt.Sprintf("%.3f", node.FusedScore)))))
			default:
				label := strings.ToUpper(string(n.Kind))
				fmt.Printf("%s %s %s %s\n   %s\n\n",
					gray(fmt.Sprintf("%d.", i+1)),
					kindBadge(label),
					bold(n.Qualified),
					yellow(fmt.Sprintf("%.3f", node.FusedScore)),
					gray(fmt.Sprintf("%s:%d", n.FilePath, n.StartLine)))
			}
		}

		if result.Explanation != "" {
			fmt.Println(cyan("─────────────────────────────────────"))
			fmt.Println(result.Explanation)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
	queryCmd.Flags().String("repo", "", "Repository slug to search (searches all repos if empty)")
	queryCmd.Flags().Int("top-k", 10, "Number of results to return")
	queryCmd.Flags().String("kind", "", "Filter by node kind: function, class, file, module (comma-separated)")
}
