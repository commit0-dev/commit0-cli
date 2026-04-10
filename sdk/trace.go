package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
)

// TraceRequest is the request body for trace operations.
type TraceRequest struct {
	Symbol    string `json:"symbol"`
	RepoSlug  string `json:"repo_slug"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
}

// Trace performs a call graph trace and returns the full result as JSON.
func (c *Client) Trace(ctx context.Context, req TraceRequest) (*types.TraceResult, error) {
	var result types.TraceResult
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/trace/json")
	if err != nil {
		return nil, fmt.Errorf("trace: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}
