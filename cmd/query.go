package cmd

import (
	"context"
	"fmt"
	"log/slog"
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

		if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
			fmt.Printf("Found %d results in %dms\n\n", len(result.Nodes), result.Timing.TotalMS)
			for i, node := range result.Nodes {
				n := node.Node
				switch n.Kind {
				case types.NodeModule:
					version := ""
					if n.Docstring != "" {
						version = " " + n.Docstring
					}
					fmt.Printf("%d. [MODULE] %s%s (score: %.3f)\n   import \"%s\"\n\n",
						i+1, n.Name, version, node.FusedScore, n.Qualified)
				default:
					label := strings.ToUpper(string(n.Kind))
					fmt.Printf("%d. [%s] %s (score: %.3f)\n   %s:%d\n\n",
						i+1, label, n.Qualified, node.FusedScore, n.FilePath, n.StartLine)
				}
			}
		}
		if result.Explanation != "" {
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
