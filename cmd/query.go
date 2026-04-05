package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
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

		svc, cleanup, err := wireQueryService(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer cleanup()

		result, err := svc.Query(cmd.Context(), app.QueryRequest{
			Question: args[0],
			RepoSlug: repoSlug,
			TopK:     topK,
		})
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}

		fmt.Printf("Found %d results in %dms\n\n", len(result.Nodes), result.Timing.TotalMS)
		for i, node := range result.Nodes {
			fmt.Printf("%d. %s (score: %.3f)\n   %s:%d\n\n",
				i+1,
				node.Node.Qualified,
				node.FusedScore,
				node.Node.FilePath,
				node.Node.StartLine,
			)
		}
		if result.Explanation != "" {
			fmt.Printf("Explanation:\n%s\n", result.Explanation)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
	queryCmd.Flags().String("repo", "", "Repository slug to search (searches all repos if empty)")
	queryCmd.Flags().Int("top-k", 10, "Number of results to return")
}
