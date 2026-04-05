package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	httpAdapter "github.com/commit0-dev/commit0/internal/adapters/http"
	"github.com/commit0-dev/commit0/internal/config"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		ctx := cmd.Context()

		// Wire all services.
		indexSvc, indexCleanup, err := wireIndexService(ctx, cfg)
		if err != nil {
			return fmt.Errorf("wire index service: %w", err)
		}
		defer indexCleanup()

		querySvc, queryCleanup, err := wireQueryService(ctx, cfg)
		if err != nil {
			return fmt.Errorf("wire query service: %w", err)
		}
		defer queryCleanup()

		traceSvc, traceCleanup, err := wireTraceService(ctx, cfg)
		if err != nil {
			return fmt.Errorf("wire trace service: %w", err)
		}
		defer traceCleanup()

		blastSvc, blastCleanup, err := wireBlastService(ctx, cfg)
		if err != nil {
			return fmt.Errorf("wire blast service: %w", err)
		}
		defer blastCleanup()

		repoSvc, repoCleanup, err := wireRepoService(ctx, cfg)
		if err != nil {
			return fmt.Errorf("wire repo service: %w", err)
		}
		defer repoCleanup()

		server := httpAdapter.NewServer(
			indexSvc,
			querySvc,
			traceSvc,
			blastSvc,
			repoSvc,
			&cfg.Server,
		)

		// Handle OS signals for graceful shutdown.
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		serverErr := make(chan error, 1)
		go func() {
			serverErr <- server.Start()
		}()

		select {
		case err := <-serverErr:
			if err != nil {
				return fmt.Errorf("server error: %w", err)
			}
		case sig := <-quit:
			slog.Info("shutting down", "signal", sig)
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutCtx); err != nil {
				return fmt.Errorf("shutdown: %w", err)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().Int("port", 0, "Override server port from config")
}
