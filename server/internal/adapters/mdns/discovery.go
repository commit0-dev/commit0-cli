// Package mdns provides mDNS-based LAN peer discovery for commit0 P2P sync.
package mdns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"
)

const (
	// ServiceType is the mDNS service type for commit0 sync.
	ServiceType = "_commit0._udp"
	// MulticastAddr is the standard mDNS multicast address.
	MulticastAddr = "224.0.0.251:5353"
)

// DiscoveredPeer represents a commit0 peer found via mDNS.
type DiscoveredPeer struct {
	Name     string
	Endpoint string // host:port for QUIC
	APIURL   string // HTTP control plane
}

// Discovery handles mDNS service announcement and peer discovery.
type Discovery struct {
	instanceName string
	quicPort     int
	httpPort     int
	log          *slog.Logger
}

// NewDiscovery creates an mDNS discovery service.
func NewDiscovery(instanceName string, quicPort, httpPort int) *Discovery {
	return &Discovery{
		instanceName: instanceName,
		quicPort:     quicPort,
		httpPort:     httpPort,
		log:          slog.Default().With("adapter", "mdns"),
	}
}

// Announce registers the local commit0 server on the LAN via mDNS.
// Runs until ctx is cancelled.
func (d *Discovery) Announce(ctx context.Context) error {
	d.log.Info("mDNS announcing",
		"service", ServiceType,
		"instance", d.instanceName,
		"quic_port", d.quicPort,
	)

	// Simple periodic announcement via multicast UDP.
	// In production, use a proper mDNS library (e.g., hashicorp/mdns).
	// For now, this is a lightweight implementation using raw multicast.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.sendAnnouncement(); err != nil {
				d.log.Debug("mDNS announce failed", "err", err)
			}
		}
	}
}

// Discover scans the LAN for commit0 peers via mDNS.
// Returns discovered peers within the timeout window.
func (d *Discovery) Discover(ctx context.Context, timeout time.Duration) ([]DiscoveredPeer, error) {
	d.log.Info("mDNS scanning for peers", "timeout", timeout)

	addr, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve mDNS addr: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast: %w", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(timeout))

	var peers []DiscoveredPeer
	buf := make([]byte, 4096)

	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break // scan timeout reached
			}
			return peers, nil
		}

		msg := string(buf[:n])
		if !strings.Contains(msg, ServiceType) {
			continue
		}

		// Parse announcement: "commit0|<instance>|<quic_port>|<http_port>"
		parts := strings.Split(msg, "|")
		if len(parts) < 4 || parts[0] != "commit0" {
			continue
		}

		peer := DiscoveredPeer{
			Name:     parts[1],
			Endpoint: fmt.Sprintf("%s:%s", src.IP.String(), parts[2]),
			APIURL:   fmt.Sprintf("http://%s:%s", src.IP.String(), parts[3]),
		}
		peers = append(peers, peer)
		d.log.Info("mDNS peer discovered", "name", peer.Name, "endpoint", peer.Endpoint)
	}

	return peers, nil
}

func (d *Discovery) sendAnnouncement() error {
	addr, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	msg := fmt.Sprintf("commit0|%s|%d|%d|%s", d.instanceName, d.quicPort, d.httpPort, ServiceType)
	_, err = conn.Write([]byte(msg))
	return err
}
