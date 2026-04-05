package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "commit0",
	Short: "Graph-based source code analyzer",
	Long: `commit0 indexes your codebase into a graph and lets you query,
trace, and blast-radius analyze it using AI-powered semantic search.`,
}

// SetVersion wires the build-time version and commit into the root command.
// Called from main() with values injected by -ldflags.
func SetVersion(version, commit string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s)", version, commit)
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "Path to a JSON config file (optional, overridden by env vars)")
	rootCmd.PersistentFlags().String("log-level", "INFO", "Log level: DEBUG, INFO, WARN, ERROR")

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		lvl, _ := cmd.Flags().GetString("log-level")
		var level slog.Level
		switch strings.ToUpper(lvl) {
		case "DEBUG":
			level = slog.LevelDebug
		case "WARN":
			level = slog.LevelWarn
		case "ERROR":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	}
}
