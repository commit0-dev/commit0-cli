package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "commit0",
	Short: "Graph-based source code analyzer",
	Long: `commit0 indexes your codebase into a graph and lets you query,
trace, and blast-radius analyze it using AI-powered semantic search.`,
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("repo", "", "Repository slug")
	rootCmd.PersistentFlags().String("config", "", "Config file path (optional)")
}
