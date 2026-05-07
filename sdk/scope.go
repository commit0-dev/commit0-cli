package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// AddScopeRequest is the request body for adding a repo to sync scope.
type AddScopeRequest struct {
	RepoSlug string `json:"repo_slug"`
}

// AddScope adds a repo to the sync scope.
func (c *Client) AddScope(ctx context.Context, repoSlug string) error {
	resp, err := c.syncRequest(ctx).
		SetBody(AddScopeRequest{RepoSlug: repoSlug}).
		Post("/api/v1/sync/scope")
	if err != nil {
		return fmt.Errorf("add scope: %w", err)
	}
	if resp.IsError() {
		return mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return nil
}

// ListScope returns all repos in the sync scope.
func (c *Client) ListScope(ctx context.Context) ([]types.SyncScope, error) {
	var result []types.SyncScope
	resp, err := c.syncRequest(ctx).
		SetResult(&result).
		Get("/api/v1/sync/scope")
	if err != nil {
		return nil, fmt.Errorf("list scope: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return result, nil
}

// RemoveScope removes a repo from the sync scope.
func (c *Client) RemoveScope(ctx context.Context, repoSlug string) error {
	resp, err := c.syncRequest(ctx).
		Delete("/api/v1/sync/scope/" + repoSlug)
	if err != nil {
		return fmt.Errorf("remove scope: %w", err)
	}
	if resp.IsError() {
		return mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return nil
}
