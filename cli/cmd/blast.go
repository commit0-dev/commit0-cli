package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/sdk"
)

var blastCmd = &cobra.Command{
	Use:   "blast <symbol>",
	Short: "Analyze blast radius of a code change",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		maxDepth, _ := cmd.Flags().GetInt("max-depth")
		noExplain, _ := cmd.Flags().GetBool("no-explain")

		c := sdk.New(serverURL(cmd))
		result, err := c.Blast(cmd.Context(), sdk.BlastRequest{
			Symbol:    args[0],
			RepoSlug:  repoSlug,
			MaxDepth:  maxDepth,
			NoExplain: noExplain,
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
	blastCmd.Flags().Int("max-depth", 3, "Maximum traversal depth (default 3, max 5)")
	blastCmd.Flags().Bool("no-explain", false, "Skip LLM explanation (faster)")
}
