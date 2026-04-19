// Package mdns provides mDNS-based LAN peer discovery for commit0 P2P sync.
// Kept as a fallback when Consul is not available.
package mdns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

const (
	ServiceType   = "_commit0._udp"
	MulticastAddr = "224.0.0.251:5353"
)

// Compile-time interface check.
var _ domain.PeerDiscovery = (*Discovery)(nil)

// Discovery implements PeerDiscovery using mDNS multicast on the LAN.
type Discovery struct {
	instanceName string
	quicPort     int
	httpPort     int
	log          *slog.Logger
}

// New creates an mDNS discovery service.
func New(instanceName string) *Discovery {
	return &Discovery{
		instanceName: instanceName,
		log:          slog.Default().With("adapter", "mdns"),
	}
}

func (d *Discovery) Register(ctx context.Context, name string, quicPort, httpPort int) error {
	d.instanceName = name
	d.quicPort = quicPort
	d.httpPort = httpPort
	d.log.Info("mDNS registered", "name", name, "quic_port", quicPort)

	// Start periodic announcements.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.sendAnnouncement()
			}
		}
	}()
	return nil
}

func (d *Discovery) Discover(ctx context.Context) ([]types.PeerInfo, error) {
	addr, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve mDNS addr: %w", err)
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast: %w", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	var peers []types.PeerInfo
	buf := make([]byte, 4096)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		parts := strings.Split(string(buf[:n]), "|")
		if len(parts) < 4 || parts[0] != "commit0" {
			continue
		}
		peers = append(peers, types.PeerInfo{
			Name:     parts[1],
			Endpoint: fmt.Sprintf("%s:%s", src.IP.String(), parts[2]),
			APIURL:   fmt.Sprintf("http://%s:%s", src.IP.String(), parts[3]),
			AddedAt:  time.Now(),
		})
	}
	return peers, nil
}

func (d *Discovery) Watch(ctx context.Context, handler func([]types.PeerInfo)) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			peers, err := d.Discover(ctx)
			if err == nil && len(peers) > 0 {
				handler(peers)
			}
		}
	}
}

func (d *Discovery) Deregister(ctx context.Context) error {
	return nil // mDNS has no explicit deregistration
}

func (d *Discovery) sendAnnouncement() {
	addr, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()
	msg := fmt.Sprintf("commit0|%s|%d|%d|%s", d.instanceName, d.quicPort, d.httpPort, ServiceType)
	_, _ = conn.Write([]byte(msg))
}
