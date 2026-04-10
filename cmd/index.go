package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/adapters/client"
)

var indexCmd = &cobra.Command{
	Use:   "index [github-url|local-path]",
	Short: "Index a repository into the graph",
	Long: `Index a repository into the commit0 graph.

The repository slug is always derived from the GitHub remote URL, ensuring
consistent naming across all commit0 commands.

Examples:
  commit0 index https://github.com/commit0-dev/commit0.git   # clone and index
  commit0 index https://github.com/owner/repo                # .git suffix optional
  commit0 index /path/to/local/repo                          # must have git remote origin
  commit0 index                                              # current directory`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		arg := "."
		if len(args) > 0 {
			arg = args[0]
		}

		// Resolve GitHub URLs or local paths — this runs locally (git clone).
		repoPath, repoSlug, err := resolveRepoSource(cmd.Context(), arg)
		if err != nil {
			return err
		}

		langsRaw, _ := cmd.Flags().GetString("languages")
		var languages []string
		if langsRaw != "" {
			for _, l := range strings.Split(langsRaw, ",") {
				l = strings.TrimSpace(l)
				if l != "" {
					languages = append(languages, l)
				}
			}
		}

		force, _ := cmd.Flags().GetBool("force")
		fmt.Printf("Indexing %s as %q...\n", repoPath, repoSlug)

		c := client.New(serverURL(cmd))
		progress, err := c.StartIndex(cmd.Context(), client.StartIndexRequest{
			RepoPath:  repoPath,
			RepoSlug:  repoSlug,
			Languages: languages,
			Force:     force,
		}, func(p client.IndexProgress) {
			fmt.Printf("\r  %d files, %d nodes...", p.FilesIndexed, p.NodesCreated)
		})
		if err != nil {
			return fmt.Errorf("index: %w", err)
		}

		fmt.Printf("\rIndexed %d files, %d nodes\n", progress.FilesIndexed, progress.NodesCreated)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().String("languages", "", "Comma-separated list of languages to index (e.g. go,python)")
	indexCmd.Flags().Bool("force", false, "Delete existing nodes before indexing (removes stale data)")
}
