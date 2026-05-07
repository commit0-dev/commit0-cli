package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// SyncExportRequest is the request body for exporting a graph bundle.
type SyncExportRequest struct {
	RepoSlug string `json:"repo_slug"`
}

// SyncExport builds and returns a graph bundle for a repo.
func (c *Client) SyncExport(ctx context.Context, req SyncExportRequest) (*types.GraphBundle, error) {
	var result types.GraphBundle
	resp, err := c.syncRequest(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/sync/export")
	if err != nil {
		return nil, fmt.Errorf("sync export: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// SyncImport uploads a CBOR-encoded bundle for import.
func (c *Client) SyncImport(ctx context.Context, bundleData []byte) (*types.SyncResult, error) {
	var result types.SyncResult
	resp, err := c.syncRequest(ctx).
		SetBody(bundleData).
		SetHeader("Content-Type", "application/cbor").
		SetResult(&result).
		Post("/api/v1/sync/import")
	if err != nil {
		return nil, fmt.Errorf("sync import: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// SyncPullRequest is the request body for pulling from a remote.
type SyncPullRequest struct {
	PeerName string `json:"peer_name"`
	RepoSlug string `json:"repo_slug"`
}

// SyncPull triggers a pull from a remote peer.
func (c *Client) SyncPull(ctx context.Context, req SyncPullRequest) (*types.SyncResult, error) {
	var result types.SyncResult
	resp, err := c.syncRequest(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/sync/pull")
	if err != nil {
		return nil, fmt.Errorf("sync pull: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// SyncPush triggers a push to a remote peer.
func (c *Client) SyncPush(ctx context.Context, req SyncPullRequest) (*types.SyncResult, error) {
	var result types.SyncResult
	resp, err := c.syncRequest(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/sync/push")
	if err != nil {
		return nil, fmt.Errorf("sync push: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// SyncManifest returns the sync manifest for a repo.
func (c *Client) SyncManifest(ctx context.Context, repoSlug string) (*types.SyncManifest, error) {
	var result types.SyncManifest
	resp, err := c.syncRequest(ctx).
		SetResult(&result).
		Get("/api/v1/sync/manifest/" + repoSlug)
	if err != nil {
		return nil, fmt.Errorf("sync manifest: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}
