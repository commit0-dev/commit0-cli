package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
)

// QueryRequest is the request body for semantic code search.
type QueryRequest struct {
	Question string  `json:"question"`
	RepoSlug string  `json:"repo_slug"`
	TopK     int     `json:"top_k,omitempty"`
	MinScore float64 `json:"min_score,omitempty"`
}

// Query performs a semantic code search (non-agent direct mode).
func (c *Client) Query(ctx context.Context, req QueryRequest) (*types.QueryResult, error) {
	var result types.QueryResult
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/query")
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}
