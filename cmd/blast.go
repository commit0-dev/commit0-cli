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

		fmt.Printf("%s %s%s %s\n\n",
			bold("Blast radius for"),
			bold(cyan(result.Target.Qualified)),
			gray(":"),
			gray(fmt.Sprintf("%d affected nodes (%dms)", len(result.Affected), result.Timing.TotalMS)))
		for i, aff := range result.Affected {
			fmt.Printf("%s %s %s\n   %s\n",
				gray(fmt.Sprintf("%d.", i+1)),
				bold(aff.Node.Qualified),
				dim(yellow(fmt.Sprintf("hop %d", aff.HopCount))),
				gray(aff.Node.FilePath),
			)
		}
		if result.Summary != "" {
			fmt.Printf("\n%s\n%s\n", bold("Summary:"), result.Summary)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(blastCmd)
	blastCmd.Flags().String("repo", "", "Repository slug")
	blastCmd.Flags().Int("max-depth", 10, "Maximum traversal depth")
}
