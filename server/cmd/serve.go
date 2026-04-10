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

	httpAdapter "github.com/commit0-dev/commit0/server/internal/adapters/http"
	"github.com/commit0-dev/commit0/server/internal/config"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Apply --port flag override.
		if port, _ := cmd.Flags().GetInt("port"); port > 0 {
			cfg.Server.Port = port
		}

		ctx := cmd.Context()

		// Wire all services from a single shared deps instance.
		svcs, err := wireServeServices(ctx, cfg)
		if err != nil {
			return fmt.Errorf("wire services: %w", err)
		}
		defer svcs.cleanup()

		server := httpAdapter.NewServer(
			svcs.index,
			svcs.query,
			svcs.trace,
			svcs.blast,
			svcs.repo,
			svcs.db,
			svcs.agent,
			svcs.flow,
			svcs.temporal,
			svcs.rootCause,
			svcs.apiSurface,
			&cfg.Server,
		)

		// Register sync routes if the sync service is available.
		if svcs.syncSvc != nil {
			server.SetSyncService(svcs.syncSvc, svcs.peerStore, svcs.scopeStore, cfg.Sync.Passphrase)

			// Start QUIC transport for P2P data plane.
			if svcs.transport != nil {
				svcs.syncSvc.SetTransport(svcs.transport, svcs.peerStore, svcs.scopeStore)
				quicAddr := fmt.Sprintf(":%d", cfg.Sync.QUICPort)
				go func() {
					if err := svcs.transport.Serve(ctx, quicAddr, svcs.syncSvc); err != nil {
						slog.Error("QUIC transport error", "err", err)
					}
				}()
			}
		}

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
