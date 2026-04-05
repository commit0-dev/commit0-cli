package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
)

var indexCmd = &cobra.Command{
	Use:   "index <path>",
	Short: "Index a repository into the graph",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		repoSlug, _ := cmd.Flags().GetString("slug")
		if repoSlug == "" {
			repoSlug = filepath.Base(args[0])
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

		fmt.Printf("Indexing %s as %q...\n", args[0], repoSlug)

		result, err := svc.Index(cmd.Context(), app.IndexRequest{
			RepoPath:  args[0],
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
	indexCmd.Flags().String("slug", "", "Repository slug (defaults to directory name)")
	indexCmd.Flags().String("languages", "", "Comma-separated list of languages to index (e.g. go,python)")
}
