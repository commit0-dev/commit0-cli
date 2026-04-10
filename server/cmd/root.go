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
	Short: "commit0 server — graph-based source code analysis engine",
	Long: `commit0 server indexes your codebase into a graph and exposes it
via an HTTP API for querying, tracing, and blast-radius analysis.`,
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

// configPath returns the --config flag value.
func configPath(cmd *cobra.Command) string {
	p, _ := cmd.Flags().GetString("config")
	return p
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "Path to a JSON config file (optional, overridden by env vars)")
	rootCmd.PersistentFlags().String("log-level", "WARN", "Log level: DEBUG, INFO, WARN, ERROR")

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		// Flag takes precedence; fall back to LOG_LEVEL env var.
		lvl, _ := cmd.Flags().GetString("log-level")
		if !cmd.Flags().Changed("log-level") {
			if envLvl := os.Getenv("LOG_LEVEL"); envLvl != "" {
				lvl = envLvl
			}
		}
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
