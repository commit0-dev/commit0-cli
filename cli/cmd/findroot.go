package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/sdk"
)

var findRootCmd = &cobra.Command{
	Use:   "find-root <description>",
	Short: "Find the commit that introduced a bug (commit zero)",
	Long: `Automated root cause analysis: traces data flow backward from a failure,
queries temporal graph for when relationships changed, and identifies the
commit that most likely introduced the bug.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		since, _ := cmd.Flags().GetString("since")

		c := sdk.New(serverURL(cmd))
		events, err := c.FindRoot(cmd.Context(), sdk.FindRootRequest{
			Description: args[0],
			RepoSlug:    repoSlug,
			Since:       since,
		})
		if err != nil {
			return fmt.Errorf("find-root: %w", err)
		}

		for evt := range events {
			switch evt.Type {
			case "status":
				fmt.Printf("%s %s\n", dim("[status]"), evt.Status)
			case "error":
				fmt.Printf("%s %s\n", bold("[error]"), evt.Error)
			case "result":
				if evt.Result == nil {
					continue
				}
				r := evt.Result
				fmt.Printf("\n%s %s\n", bold("Commit Zero:"), cyan(r.CommitHash))
				fmt.Printf("  %s %s\n", bold("Author:"), r.Author)
				fmt.Printf("  %s %s\n", bold("Message:"), r.CommitMessage)
				fmt.Printf("  %s %.0f%%\n", bold("Confidence:"), r.Confidence*100)

				if len(r.CausalChain) > 0 {
					fmt.Printf("\n%s\n", bold("Causal Chain:"))
					for i, hop := range r.CausalChain {
						fmt.Printf("  %d. %s (%s:%d)\n",
							i+1,
							bold(hop.Node.Qualified),
							gray(hop.Node.FilePath),
							hop.Node.StartLine,
						)
					}
				}

				if r.Explanation != "" {
					fmt.Printf("\n%s\n%s\n", bold("Explanation:"), r.Explanation)
				}
				if r.SuggestedFix != "" {
					fmt.Printf("\n%s\n%s\n", bold("Suggested Fix:"), r.SuggestedFix)
				}
			case "done":
				// Stream complete
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(findRootCmd)
	findRootCmd.Flags().String("repo", "", "Repository slug")
	findRootCmd.Flags().String("since", "", "Only search commits since this date (e.g., '3 days ago')")
}
