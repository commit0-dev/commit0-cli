package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/sdk"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage indexed repositories",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all indexed repositories",
	RunE: func(cmd *cobra.Command, _ []string) error {
		c := sdk.New(serverURL(cmd))

		repos, err := c.ListRepos(cmd.Context())
		if err != nil {
			return fmt.Errorf("list repos: %w", err)
		}

		if len(repos) == 0 {
			fmt.Println("No repositories indexed yet. Run: commit0 index <path>")
			return nil
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SLUG\tPATH\tLANGUAGES\tLAST COMMIT")
		for _, r := range repos {
			langs := strings.Join(r.Languages, ",")
			if langs == "" {
				langs = "-"
			}
			commit := r.LastCommit
			if commit == "" {
				commit = "-"
			} else if len(commit) > 8 {
				commit = commit[:8]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Slug, r.Path, langs, commit)
		}
		return w.Flush()
	},
}

var repoGetCmd = &cobra.Command{
	Use:   "get <slug>",
	Short: "Show details for a repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))

		repo, err := c.GetRepo(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("get repo: %w", err)
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Slug:          %s\n", repo.Slug)
		fmt.Fprintf(out, "Path:          %s\n", repo.Path)
		if repo.RemoteURL != "" {
			fmt.Fprintf(out, "Remote:        %s\n", repo.RemoteURL)
		}
		if len(repo.Languages) > 0 {
			fmt.Fprintf(out, "Languages:     %s\n", strings.Join(repo.Languages, ", "))
		}
		if repo.LastCommit != "" {
			fmt.Fprintf(out, "Last commit:   %s\n", repo.LastCommit)
		}
		if !repo.CreatedAt.IsZero() {
			fmt.Fprintf(out, "Created:       %s\n", repo.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		return nil
	},
}

var repoDeleteCmd = &cobra.Command{
	Use:   "delete <slug>",
	Short: "Delete a repository and all its indexed nodes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))

		repo, err := c.DeleteRepo(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("delete repo: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Deleted repository %q (%s)\n", repo.Slug, repo.Path)
		return nil
	},
}

var repoCreateCmd = &cobra.Command{
	Use:   "create <slug>",
	Short: "Register a repository (without indexing)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))

		path, _ := cmd.Flags().GetString("path")
		remote, _ := cmd.Flags().GetString("remote")
		langsRaw, _ := cmd.Flags().GetString("languages")

		var langs []string
		for _, l := range strings.Split(langsRaw, ",") {
			if l = strings.TrimSpace(l); l != "" {
				langs = append(langs, l)
			}
		}

		repo, err := c.CreateRepo(cmd.Context(), sdk.CreateRepoRequest{
			Slug:      args[0],
			Path:      path,
			RemoteURL: remote,
			Languages: langs,
		})
		if err != nil {
			return fmt.Errorf("create repo: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Created repository %q at %s\n", repo.Slug, repo.Path)
		return nil
	},
}

// configPath reads the --config persistent flag from the root command.
func configPath(cmd *cobra.Command) string {
	if f := cmd.Root().PersistentFlags().Lookup("config"); f != nil {
		return f.Value.String()
	}
	return ""
}

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoGetCmd)
	repoCmd.AddCommand(repoDeleteCmd)
	repoCmd.AddCommand(repoCreateCmd)

	repoCreateCmd.Flags().String("path", "", "Filesystem path to the repository (required)")
	repoCreateCmd.Flags().String("remote", "", "Remote URL (optional)")
	repoCreateCmd.Flags().String("languages", "", "Comma-separated language list (optional)")
	_ = repoCreateCmd.MarkFlagRequired("path")
}
