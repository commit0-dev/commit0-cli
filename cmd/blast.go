package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
)

var blastCmd = &cobra.Command{
	Use:   "blast <symbol>",
	Short: "Analyze blast radius of a code change",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		repoSlug, _ := cmd.Flags().GetString("repo")
		maxDepth, _ := cmd.Flags().GetInt("max-depth")

		svc, cleanup, err := wireBlastService(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer cleanup()

		result, err := svc.Blast(cmd.Context(), app.BlastRequest{
			Symbol:   args[0],
			RepoSlug: repoSlug,
			MaxDepth: maxDepth,
		})
		if err != nil {
			return fmt.Errorf("blast: %w", err)
		}

		fmt.Printf("Blast radius for %s: %d affected nodes (%dms)\n\n",
			result.Target.Qualified, len(result.Affected), result.Timing.TotalMS)
		for i, aff := range result.Affected {
			fmt.Printf("%d. %s (hop %d)\n   %s\n",
				i+1,
				aff.Node.Qualified,
				aff.HopCount,
				aff.Node.FilePath,
			)
		}
		if result.Summary != "" {
			fmt.Printf("\nSummary:\n%s\n", result.Summary)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(blastCmd)
	blastCmd.Flags().String("repo", "", "Repository slug")
	blastCmd.Flags().Int("max-depth", 10, "Maximum traversal depth")
}
