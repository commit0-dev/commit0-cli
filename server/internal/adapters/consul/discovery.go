// Package consul provides Consul-based LAN peer discovery for commit0 P2P sync.
package consul

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface check.
var _ domain.PeerDiscovery = (*Discovery)(nil)

// Discovery implements PeerDiscovery using HashiCorp Consul's service registry.
// Designed for LAN/VPN usage — never exposes discovery to the public internet.
type Discovery struct {
	client    *api.Client
	serviceID string
	log       *slog.Logger
}

// New creates a Consul discovery adapter. addr is the Consul agent address (default: 127.0.0.1:8500).
func New(addr, token string) (*Discovery, error) {
	cfg := api.DefaultConfig()
	if addr != "" {
		cfg.Address = addr
	}
	if token != "" {
		cfg.Token = token
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("consul client: %w", err)
	}

	return &Discovery{
		client: client,
		log:    slog.Default().With("adapter", "consul"),
	}, nil
}

// Register announces the local commit0 server as a Consul service.
func (d *Discovery) Register(ctx context.Context, name string, quicPort, httpPort int) error {
	d.serviceID = fmt.Sprintf("commit0-%s", name)

	hostname, _ := os.Hostname()

	service := &api.AgentServiceRegistration{
		ID:   d.serviceID,
		Name: "commit0",
		Port: quicPort,
		Tags: []string{"quic", "p2p-sync"},
		Meta: map[string]string{
			"instance-name": name,
			"http-port":     fmt.Sprintf("%d", httpPort),
			"hostname":      hostname,
		},
		Check: &api.AgentServiceCheck{
			TCP:                            fmt.Sprintf("localhost:%d", quicPort),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	if err := d.client.Agent().ServiceRegister(service); err != nil {
		return fmt.Errorf("register service: %w", err)
	}

	d.log.Info("registered with Consul",
		"service_id", d.serviceID,
		"quic_port", quicPort,
		"http_port", httpPort,
	)
	return nil
}

// Discover returns all healthy commit0 peers from Consul, excluding self.
func (d *Discovery) Discover(ctx context.Context) ([]types.PeerInfo, error) {
	entries, _, err := d.client.Health().Service("commit0", "", true, queryOpts(ctx))
	if err != nil {
		return nil, fmt.Errorf("discover peers: %w", err)
	}

	var peers []types.PeerInfo
	for _, entry := range entries {
		if entry.Service.ID == d.serviceID {
			continue // skip self
		}
		peers = append(peers, entryToPeerInfo(entry))
	}
	return peers, nil
}

// Watch continuously monitors for peer changes using Consul blocking queries (long-poll).
// Calls handler whenever the peer list changes. Blocks until ctx is cancelled.
func (d *Discovery) Watch(ctx context.Context, handler func([]types.PeerInfo)) error {
	var lastIndex uint64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		opts := &api.QueryOptions{
			WaitIndex: lastIndex,
			WaitTime:  5 * time.Minute,
		}
		opts = opts.WithContext(ctx)

		entries, meta, err := d.client.Health().Service("commit0", "", true, opts)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.log.Error("watch error", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if meta.LastIndex != lastIndex {
			lastIndex = meta.LastIndex

			var peers []types.PeerInfo
			for _, entry := range entries {
				if entry.Service.ID == d.serviceID {
					continue
				}
				peers = append(peers, entryToPeerInfo(entry))
			}

			d.log.Info("peer list changed", "count", len(peers))
			handler(peers)
		}
	}
}

// Deregister removes the local server from Consul.
func (d *Discovery) Deregister(ctx context.Context) error {
	if d.serviceID == "" {
		return nil
	}
	if err := d.client.Agent().ServiceDeregister(d.serviceID); err != nil {
		return fmt.Errorf("deregister: %w", err)
	}
	d.log.Info("deregistered from Consul", "service_id", d.serviceID)
	return nil
}

func entryToPeerInfo(entry *api.ServiceEntry) types.PeerInfo {
	addr := entry.Service.Address
	if addr == "" {
		addr = entry.Node.Address
	}

	httpPort := entry.Service.Meta["http-port"]
	name := entry.Service.Meta["instance-name"]
	if name == "" {
		name = entry.Service.ID
	}

	return types.PeerInfo{
		Name:     name,
		Endpoint: fmt.Sprintf("%s:%d", addr, entry.Service.Port),
		APIURL:   fmt.Sprintf("http://%s:%s", addr, httpPort),
		AddedAt:  time.Now(),
	}
}

func queryOpts(ctx context.Context) *api.QueryOptions {
	opts := &api.QueryOptions{}
	return opts.WithContext(ctx)
}
