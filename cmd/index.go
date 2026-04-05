package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
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
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		arg := "."
		if len(args) > 0 {
			arg = args[0]
		}

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

		svc, cleanup, err := wireIndexService(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer cleanup()

		fmt.Printf("Indexing %s as %q...\n", repoPath, repoSlug)

		result, err := svc.Index(cmd.Context(), app.IndexRequest{
			RepoPath:  repoPath,
			RepoSlug:  repoSlug,
			Languages: languages,
		})
		if err != nil {
			return fmt.Errorf("index: %w", err)
		}

		fmt.Printf("Indexed %d files, %d nodes in %dms\n",
			result.FilesIndexed, result.NodesCreated, result.Timing.TotalMS)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().String("languages", "", "Comma-separated list of languages to index (e.g. go,python)")
}
