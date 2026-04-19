package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// ShowNode retrieves a single node by its qualified name.
// Returns the full CodeNode including Body, Signature, etc.
func (c *Client) ShowNode(ctx context.Context, repoSlug, qualified string) (*types.CodeNode, error) {
	var result types.CodeNode
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&result).
		SetQueryParam("repo", repoSlug).
		SetQueryParam("qualified", qualified).
		Get("/api/v1/nodes/lookup")
	if err != nil {
		return nil, fmt.Errorf("show node: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}

// ListNodesByFile retrieves all nodes in a specific file.
func (c *Client) ListNodesByFile(ctx context.Context, repoSlug, filePath string) ([]types.CodeNode, error) {
	var result []types.CodeNode
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&result).
		SetQueryParam("repo", repoSlug).
		SetQueryParam("path", filePath).
		Get("/api/v1/nodes/by-file")
	if err != nil {
		return nil, fmt.Errorf("list nodes by file: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return result, nil
}
