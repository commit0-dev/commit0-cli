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

// serverURL returns the commit0 server URL from --server-url flag, env var, or default.
func serverURL(cmd *cobra.Command) string {
	if u, _ := cmd.Flags().GetString("server-url"); u != "" {
		return u
	}
	if u := os.Getenv("COMMIT0_SERVER_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

func init() {
	rootCmd.PersistentFlags().String("config", "", "Path to a JSON config file (optional, overridden by env vars)")
	rootCmd.PersistentFlags().String("log-level", "WARN", "Log level: DEBUG, INFO, WARN, ERROR")
	rootCmd.PersistentFlags().String("server-url", "", "commit0 server URL (env: COMMIT0_SERVER_URL, default: http://localhost:8080)")

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
