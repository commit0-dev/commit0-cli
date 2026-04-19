package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// BlastRequest is the request body for blast radius operations.
type BlastRequest struct {
	Symbol     string   `json:"symbol"`
	RepoSlug   string   `json:"repo_slug"`
	MaxDepth   int      `json:"max_depth"`
	NoExplain  bool     `json:"no_explain,omitempty"`
	EdgeLabels []string `json:"edge_labels,omitempty"`
}

// Blast performs a blast radius analysis.
func (c *Client) Blast(ctx context.Context, req BlastRequest) (*types.BlastResult, error) {
	var result types.BlastResult
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/blast")
	if err != nil {
		return nil, fmt.Errorf("blast: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}
