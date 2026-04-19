package cmd

import (
	"fmt"
	"text/tabwriter"
	"os"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage remote peers for P2P sync",
}

var remoteAddCmd = &cobra.Command{
	Use:   "add <name> <endpoint>",
	Short: "Register a remote peer",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		apiURL, _ := cmd.Flags().GetString("api-url")

		c := sdk.New(serverURL(cmd))
		peer, err := c.AddRemote(cmd.Context(), sdk.AddRemoteRequest{
			Name:     args[0],
			Endpoint: args[1],
			APIURL:   apiURL,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Remote %q added (endpoint: %s)\n", peer.Name, peer.Endpoint)
		return nil
	},
}

var remoteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered remotes",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		peers, err := c.ListRemotes(cmd.Context())
		if err != nil {
			return err
		}
		if len(peers) == 0 {
			fmt.Println("No remotes configured.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tENDPOINT\tLAST SYNC")
		for _, p := range peers {
			lastSync := "never"
			if p.LastSyncAt != nil {
				lastSync = p.LastSyncAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Endpoint, lastSync)
		}
		w.Flush()
		return nil
	},
}

var remoteRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a remote peer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		if err := c.RemoveRemote(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Remote %q removed.\n", args[0])
		return nil
	},
}

var remoteHandshakeCmd = &cobra.Command{
	Use:   "handshake <name>",
	Short: "Test connectivity with a remote peer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		if err := c.Handshake(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Handshake with %q successful.\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(remoteCmd)
	remoteCmd.AddCommand(remoteAddCmd)
	remoteCmd.AddCommand(remoteListCmd)
	remoteCmd.AddCommand(remoteRemoveCmd)
	remoteCmd.AddCommand(remoteHandshakeCmd)

	remoteAddCmd.Flags().String("api-url", "", "HTTP API URL for the remote (optional)")
}
