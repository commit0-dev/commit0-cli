package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0/pkg/types"
)

// HistoryRequest is the request body for temporal history queries.
type HistoryRequest struct {
	Symbol     string `json:"symbol"`
	RepoSlug   string `json:"repo_slug"`
	FromCommit string `json:"from_commit,omitempty"`
	ToCommit   string `json:"to_commit,omitempty"`
}

// HistoryResult wraps the temporal changes returned by the server.
type HistoryResult struct {
	Changes []types.TemporalChange `json:"changes"`
}

// History queries the temporal history of a code element.
func (c *Client) History(ctx context.Context, req HistoryRequest) (*HistoryResult, error) {
	var result HistoryResult
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post("/api/v1/history")
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &result, nil
}
