package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
)

var showCmd = &cobra.Command{
	Use:   "show <qualified.Name>",
	Short: "Show a function's source code from the graph",
	Long: `Look up a node by its qualified name and print its source code.
Use commit0-cli query to find qualified names, then show to read the code.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		if repoSlug == "" {
			return fmt.Errorf("--repo is required")
		}

		c := sdk.New(serverURL(cmd))
		node, err := c.ShowNode(cmd.Context(), repoSlug, args[0])
		if err != nil {
			return err
		}

		// Header: kind, qualified name, location
		kind := strings.ToUpper(string(node.Kind))
		fmt.Fprintf(os.Stderr, "%s %s\n", bold(kind), cyan(node.Qualified))
		fmt.Fprintf(os.Stderr, "%s:%d-%d\n", gray(node.FilePath), node.StartLine, node.EndLine)
		if node.Signature != "" {
			fmt.Fprintf(os.Stderr, "%s\n", dim(node.Signature))
		}
		fmt.Fprintln(os.Stderr)

		// Body
		if node.Body != "" {
			fmt.Println(node.Body)
		} else {
			fmt.Fprintln(os.Stderr, gray("(no body stored — run 'commit0-cli index .' to populate)"))
		}

		return nil
	},
}

var lsCmd = &cobra.Command{
	Use:   "ls <file-path>",
	Short: "List all functions and classes in a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		if repoSlug == "" {
			return fmt.Errorf("--repo is required")
		}

		c := sdk.New(serverURL(cmd))
		nodes, err := c.ListNodesByFile(cmd.Context(), repoSlug, args[0])
		if err != nil {
			return err
		}

		if len(nodes) == 0 {
			fmt.Println("No nodes found in this file.")
			return nil
		}

		for _, n := range nodes {
			kind := strings.ToUpper(string(n.Kind))
			fmt.Printf("  %-10s %-50s %s:%d\n",
				kind, n.Qualified, n.FilePath, n.StartLine)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(lsCmd)

	showCmd.Flags().String("repo", "", "Repository slug (required)")
	lsCmd.Flags().String("repo", "", "Repository slug (required)")
}
