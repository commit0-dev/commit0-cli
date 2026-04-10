package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
)

// AddRemoteRequest is the request body for registering a remote peer.
type AddRemoteRequest struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	APIURL   string `json:"api_url,omitempty"`
}

// AddRemote registers a remote peer.
func (c *Client) AddRemote(ctx context.Context, req AddRemoteRequest) (*types.PeerInfo, error) {
	var result types.PeerInfo
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/sync/remotes")
	if err != nil {
		return nil, fmt.Errorf("add remote: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// ListRemotes returns all registered remote peers.
func (c *Client) ListRemotes(ctx context.Context) ([]types.PeerInfo, error) {
	var result []types.PeerInfo
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/api/v1/sync/remotes")
	if err != nil {
		return nil, fmt.Errorf("list remotes: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return result, nil
}

// RemoveRemote deletes a registered remote peer.
func (c *Client) RemoveRemote(ctx context.Context, name string) error {
	resp, err := c.rc.R().
		SetContext(ctx).
		Delete("/api/v1/sync/remotes/" + name)
	if err != nil {
		return fmt.Errorf("remove remote: %w", err)
	}
	if resp.IsError() {
		return mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return nil
}

// Handshake tests connectivity with a remote peer.
func (c *Client) Handshake(ctx context.Context, name string) error {
	resp, err := c.rc.R().
		SetContext(ctx).
		Post("/api/v1/sync/remotes/" + name + "/handshake")
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	if resp.IsError() {
		return mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return nil
}
