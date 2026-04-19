package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Graph sync operations (export, import, manifest)",
}

var syncExportCmd = &cobra.Command{
	Use:   "export <repo-slug>",
	Short: "Export a graph bundle to a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = args[0] + ".c0bundle"
		}

		c := sdk.New(serverURL(cmd))
		bundle, err := c.SyncExport(cmd.Context(), sdk.SyncExportRequest{
			RepoSlug: args[0],
		})
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Exported %s: %d nodes, %d edges\n",
			bundle.RepoSlug, len(bundle.Nodes), len(bundle.Edges))
		fmt.Fprintf(os.Stderr, "ContentHash: %s\n", bundle.ContentHash)

		data, err := json.Marshal(bundle)
		if err != nil {
			return fmt.Errorf("marshal bundle: %w", err)
		}
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("write bundle: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Written to %s (%d bytes)\n", output, len(data))
		return nil
	},
}

var syncImportCmd = &cobra.Command{
	Use:   "import <bundle-file>",
	Short: "Import a graph bundle from a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("read bundle: %w", err)
		}

		c := sdk.New(serverURL(cmd))
		result, err := c.SyncImport(cmd.Context(), data)
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Imported %s: %d nodes, %d edges (%d skipped)\n",
			result.RepoSlug, result.NodesImported, result.EdgesImported, result.NodesSkipped)
		if result.ReEmbedQueued {
			fmt.Fprintf(os.Stderr, "Re-embedding queued for imported nodes\n")
		}
		return nil
	},
}

var syncManifestCmd = &cobra.Command{
	Use:   "manifest <repo-slug>",
	Short: "Show sync manifest for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		m, err := c.SyncManifest(cmd.Context(), args[0])
		if err != nil {
			return err
		}

		fmt.Printf("Repo:        %s\n", m.RepoSlug)
		fmt.Printf("Remote URL:  %s\n", m.RemoteURL)
		fmt.Printf("Last Commit: %s\n", m.LastCommit)
		fmt.Printf("Nodes:       %d\n", m.NodeCount)
		fmt.Printf("Edges:       %d\n", m.EdgeCount)
		fmt.Printf("Updated:     %s\n", m.UpdatedAt.Format("2006-01-02 15:04:05"))
		if m.ContentHash != "" {
			fmt.Printf("Hash:        %s\n", m.ContentHash)
		}
		return nil
	},
}

var pullCmd = &cobra.Command{
	Use:   "pull <remote> <repo-slug>",
	Short: "Pull graph data from a remote peer",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		result, err := c.SyncPull(cmd.Context(), sdk.SyncPullRequest{
			PeerName: args[0],
			RepoSlug: args[1],
		})
		if err != nil {
			return err
		}

		if result.NodesImported == 0 && result.NodesSkipped == 0 {
			fmt.Println("Already up to date.")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Pulled %s from %s: %d nodes, %d edges (%d skipped)\n",
			result.RepoSlug, result.PeerName, result.NodesImported, result.EdgesImported, result.NodesSkipped)
		if result.ReEmbedQueued {
			fmt.Fprintln(os.Stderr, "Re-embedding queued for imported nodes.")
		}
		return nil
	},
}

var pushCmd = &cobra.Command{
	Use:   "push <remote> <repo-slug>",
	Short: "Push graph data to a remote peer",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))
		result, err := c.SyncPush(cmd.Context(), sdk.SyncPullRequest{
			PeerName: args[0],
			RepoSlug: args[1],
		})
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Pushed %s to %s: %d nodes imported on remote\n",
			result.RepoSlug, result.PeerName, result.NodesImported)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(pushCmd)
	syncCmd.AddCommand(syncExportCmd)
	syncCmd.AddCommand(syncImportCmd)
	syncCmd.AddCommand(syncManifestCmd)

	syncExportCmd.Flags().StringP("output", "o", "", "Output file path (default: <repo-slug>.c0bundle)")
}
