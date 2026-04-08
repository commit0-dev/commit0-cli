package cmd

import (
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/tui"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive TUI for code analysis and investigation",
	Long: `Launch the commit0 interactive terminal interface.

Converse with the AI agent to analyze, trace, and investigate your codebase.
The agent has access to semantic search, call tracing, blast radius analysis,
data flow tracing, and more — all through natural language.

Type /help inside the TUI for available commands.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		repoSlug, _ := cmd.Flags().GetString("repo")

		// Wire all services from a single shared connection.
		svcs, err := wireServeServices(cmd.Context(), cfg)
		if err != nil {
			return fmt.Errorf("wire services: %w", err)
		}

		// Build API surface service for /api command.
		apiSvc := app.NewAPISurfaceService(svcs.db, svcs.flow, nil, cfg)

		tuiSvcs := &tui.Services{
			Agent:      svcs.agent,
			Store:      svcs.db,
			Trace:      svcs.trace,
			Blast:      svcs.blast,
			Index:      svcs.index,
			Query:      svcs.query,
			Flow:       svcs.flow,
			APISurface: apiSvc,
			Cfg:        cfg,
			Cleanup:    svcs.cleanup,
		}

		model := tui.NewModel(tuiSvcs, repoSlug)

		p := tea.NewProgram(&model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		// Store program reference so goroutines can send messages.
		model.SetProgram(p)

		slog.Info("starting TUI", "repo", repoSlug)

		if _, err := p.Run(); err != nil {
			svcs.cleanup()
			return fmt.Errorf("TUI error: %w", err)
		}

		svcs.cleanup()
		return nil
	},
}

func init() {
	chatCmd.Flags().String("repo", "", "Repository slug for analysis context")
	rootCmd.AddCommand(chatCmd)
}
