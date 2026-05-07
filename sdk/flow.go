package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// FlowRequest is the request body for field-level data flow tracing.
type FlowRequest struct {
	Symbol        string `json:"symbol"`
	FieldPath     string `json:"field_path,omitempty"`
	RepoSlug      string `json:"repo_slug"`
	Direction     string `json:"direction"`
	Depth         int    `json:"depth,omitempty"`
	ShowMutations bool   `json:"show_mutations,omitempty"`
}

// Flow performs a field-level data flow trace.
func (c *Client) Flow(ctx context.Context, req FlowRequest) (*types.FieldFlowResult, error) {
	var result types.FieldFlowResult
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/flow")
	if err != nil {
		return nil, fmt.Errorf("flow: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}
