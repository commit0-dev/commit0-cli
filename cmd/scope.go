package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
)

var scopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "Manage sync scope (which repos to sync)",
}

var scopeAddCmd = &cobra.Command{
	Use:   "add <repo-slug>",
	Short: "Add a repo to the sync scope",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		if err := c.AddScope(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Added %q to sync scope.\n", args[0])
		return nil
	},
}

var scopeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repos in the sync scope",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		scopes, err := c.ListScope(cmd.Context())
		if err != nil {
			return err
		}
		if len(scopes) == 0 {
			fmt.Println("Sync scope is empty. Use 'scope add <repo>' to add repos.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "REPO\tADDED")
		for _, s := range scopes {
			fmt.Fprintf(w, "%s\t%s\n", s.RepoSlug, s.AddedAt.Format("2006-01-02 15:04"))
		}
		w.Flush()
		return nil
	},
}

var scopeRemoveCmd = &cobra.Command{
	Use:   "remove <repo-slug>",
	Short: "Remove a repo from the sync scope",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		if err := c.RemoveScope(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed %q from sync scope.\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(scopeCmd)
	scopeCmd.AddCommand(scopeAddCmd)
	scopeCmd.AddCommand(scopeListCmd)
	scopeCmd.AddCommand(scopeRemoveCmd)
}
