package client

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
)

// APIDiscoverRequest is the request body for API surface discovery.
type APIDiscoverRequest struct {
	RepoSlug string `json:"repo_slug"`
}

// APIDiscover discovers all API endpoints from the code graph.
func (c *Client) APIDiscover(ctx context.Context, req APIDiscoverRequest) (*types.APISurface, error) {
	var result types.APISurface
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/api/discover")
	if err != nil {
		return nil, fmt.Errorf("api discover: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// APISpec generates an OpenAPI 3.0 specification from discovered endpoints.
func (c *Client) APISpec(ctx context.Context, repoSlug string) ([]byte, error) {
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(APIDiscoverRequest{RepoSlug: repoSlug}).
		Post("/api/v1/api/spec")
	if err != nil {
		return nil, fmt.Errorf("api spec: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return resp.Bytes(), nil
}
