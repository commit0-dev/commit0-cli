package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/sdk"
)

var historyCmd = &cobra.Command{
	Use:   "history <symbol>",
	Short: "Show temporal history of a code element",
	Long:  `Query when a function/class was introduced, last modified, and how its relationships changed over time.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		fromCommit, _ := cmd.Flags().GetString("from")
		toCommit, _ := cmd.Flags().GetString("to")

		c := sdk.New(serverURL(cmd))
		result, err := c.History(cmd.Context(), sdk.HistoryRequest{
			Symbol:     args[0],
			RepoSlug:   repoSlug,
			FromCommit: fromCommit,
			ToCommit:   toCommit,
		})
		if err != nil {
			return fmt.Errorf("history: %w", err)
		}

		if len(result.Changes) == 0 {
			fmt.Println(dim("  No temporal changes found."))
			return nil
		}

		fmt.Printf("%s for %s\n\n", bold("History"), bold(args[0]))
		for _, change := range result.Changes {
			fmt.Printf("  %s %s %s\n",
				cyan(change.CommitHash[:8]),
				gray(change.Timestamp.Format("2006-01-02")),
				bold(change.CommitMessage),
			)
			fmt.Printf("    Author: %s\n", change.Author)
			if len(change.NodesAdded) > 0 {
				fmt.Printf("    + %d nodes added\n", len(change.NodesAdded))
			}
			if len(change.NodesModified) > 0 {
				fmt.Printf("    ~ %d nodes modified\n", len(change.NodesModified))
			}
			if len(change.NodesRemoved) > 0 {
				fmt.Printf("    - %d nodes removed\n", len(change.NodesRemoved))
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.Flags().String("repo", "", "Repository slug")
	historyCmd.Flags().String("from", "", "Start commit (default: earliest)")
	historyCmd.Flags().String("to", "", "End commit (default: HEAD)")
}
