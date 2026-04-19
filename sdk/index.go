package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// StartIndexRequest is the request body for starting an index operation.
type StartIndexRequest struct {
	RepoPath  string   `json:"repo_path"`
	RepoSlug  string   `json:"repo_slug"`
	Languages []string `json:"languages,omitempty"`
	Exclude   []string `json:"exclude,omitempty"`
	Force     bool     `json:"force,omitempty"`
	Reparse   bool     `json:"reparse,omitempty"`
	Fast      bool     `json:"fast,omitempty"`
}

// IndexProgress is an alias for the comprehensive progress type.
// The server returns the full types.IndexProgress with stage-level detail.
type IndexProgress = types.IndexProgress

// ReEmbed triggers background re-embedding for a repo (after provider switch).
func (c *Client) ReEmbed(ctx context.Context, repoSlug string) error {
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(map[string]string{"repo_slug": repoSlug}).
		Post("/api/v1/reembed")
	if err != nil {
		return fmt.Errorf("reembed: %w", err)
	}
	if resp.IsError() {
		return mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return nil
}

// StartIndex starts an async index and polls until completion.
// onProgress is called on each poll with the current job state.
func (c *Client) StartIndex(ctx context.Context, req StartIndexRequest, onProgress func(IndexProgress)) (*IndexProgress, error) {
	// Start the index job.
	var startResp struct {
		JobID string `json:"job_id"`
	}
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&startResp).
		Post("/api/v1/index")
	if err != nil {
		return nil, fmt.Errorf("start index: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}

	if startResp.JobID == "" {
		return nil, fmt.Errorf("server returned empty job_id")
	}

	// Poll until done with exponential backoff (1s, 2s, 4s, capped at 5s).
	wait := 1 * time.Second
	const maxWait = 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		var progress IndexProgress
		pollResp, err := c.rc.R().
			SetContext(ctx).
			SetResult(&progress).
			Get("/api/v1/index/" + startResp.JobID)
		if err != nil {
			return nil, fmt.Errorf("poll index: %w", err)
		}
		if pollResp.IsError() {
			return nil, mapHTTPError(pollResp.StatusCode(), pollResp.Bytes())
		}

		if onProgress != nil {
			onProgress(progress)
		}

		switch progress.Status {
		case "completed":
			return &progress, nil
		case "failed":
			return &progress, fmt.Errorf("index failed: %s", progress.Error)
		}

		// Exponential backoff.
		wait *= 2
		if wait > maxWait {
			wait = maxWait
		}
	}
}
