package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
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
		reparse, _ := cmd.Flags().GetBool("reparse")
		fast, _ := cmd.Flags().GetBool("fast")
		fmt.Printf("Indexing %s as %q...\n", repoPath, repoSlug)

		c := sdk.New(serverURL(cmd))
		progress, err := c.StartIndex(cmd.Context(), sdk.StartIndexRequest{
			RepoPath:  repoPath,
			RepoSlug:  repoSlug,
			Languages: languages,
			Force:     force,
			Reparse:   reparse,
			Fast:      fast,
		}, func(p sdk.IndexProgress) {
			stage := string(p.CurrentStage)
			if stage == "" {
				stage = "init"
			}
			detail := ""
			if sp, ok := p.Stages[p.CurrentStage]; ok && sp != nil {
				if sp.ItemsTotal > 0 {
					detail = fmt.Sprintf(" %d/%d", sp.ItemsDone, sp.ItemsTotal)
				} else if sp.ItemsDone > 0 {
					detail = fmt.Sprintf(" %d", sp.ItemsDone)
				}
			}
			fmt.Printf("\r  [%s%s] %d files, %d nodes, %d errors  %ds",
				stage, detail, p.FilesIndexed, p.NodesCreated, p.TotalErrors, p.ElapsedMS/1000)
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
	indexCmd.Flags().Bool("reparse", false, "Re-parse all files with current resolver (no delete, no ContentHash skip)")
	indexCmd.Flags().Bool("fast", false, "Skip LLM summarization and neighborhood re-embedding (10x faster)")
}
