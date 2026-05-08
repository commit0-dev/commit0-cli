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

	"github.com/commit0-dev/commit0/pkg/types"
	httpAdapter "github.com/commit0-dev/commit0/server/internal/adapters/http"
	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
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
			svcs.graph,
			svcs.agent,
			svcs.flow,
			nil, // temporal: enterprise feature
			svcs.rootCause,
			svcs.apiSurface,
			svcs.identity,
			&cfg.Server,
			cfg,
		)

		// Mount the MCP server on /mcp using the streamable-HTTP transport.
		// Sharing the same service instances as the HTTP API closes #56:
		// index jobs started via POST /api/v1/index become observable from
		// MCP clients via commit0_index_status because both layers query the
		// same in-memory IndexService.trackerRegistry.
		server.SetMCPHandler(mcpadapter.Deps{
			QueryService:      svcs.query,
			TraceService:      svcs.trace,
			BlastService:      svcs.blast,
			FieldFlowService:  svcs.flow,
			RootCauseService:  svcs.rootCause,
			DiffImpactService: svcs.diffImpact,
			IndexService:      svcs.index,
			RepoService:       svcs.repo,
			AnalysisService:   svcs.analysis,
			APISurfaceService: svcs.apiSurface,
			Graph:             svcs.graph,
			DBAddr:            cfg.Surreal.URL,
		})

		// Start peer discovery (Consul or mDNS).
		if svcs.discovery != nil {
			if err := svcs.discovery.Register(ctx, cfg.Sync.InstanceName, cfg.Sync.QUICPort, cfg.Server.Port); err != nil {
				slog.Warn("discovery register failed", "err", err)
			} else {
				defer func() { _ = svcs.discovery.Deregister(context.Background()) }()
				if cfg.Sync.AutoDiscover {
					go func() {
						_ = svcs.discovery.Watch(ctx, func(peers []types.PeerInfo) {
							for i := range peers {
								_ = svcs.peerStore.UpsertPeer(ctx, &peers[i])
							}
						})
					}()
				}
			}
		}

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
