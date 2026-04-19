package sdk

import (
	"context"
	"fmt"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// ListRepos returns all registered repositories.
func (c *Client) ListRepos(ctx context.Context) ([]types.Repo, error) {
	var repos []types.Repo
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&repos).
		Get("/api/v1/repos")
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return repos, nil
}

// GetRepo returns a single repository by slug.
func (c *Client) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	var repo types.Repo
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&repo).
		Get("/api/v1/repos/" + slug)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &repo, nil
}

// CreateRepoRequest is the request body for creating a repository.
type CreateRepoRequest struct {
	Slug      string   `json:"slug"`
	Path      string   `json:"path"`
	RemoteURL string   `json:"remote_url,omitempty"`
	Languages []string `json:"languages,omitempty"`
}

// CreateRepo registers a new repository.
func (c *Client) CreateRepo(ctx context.Context, req CreateRepoRequest) (*types.Repo, error) {
	var repo types.Repo
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&repo).
		Post("/api/v1/repos")
	if err != nil {
		return nil, fmt.Errorf("create repo: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &repo, nil
}

// DeleteRepo removes a repository by slug.
func (c *Client) DeleteRepo(ctx context.Context, slug string) (*types.Repo, error) {
	var repo types.Repo
	resp, err := c.rc.R().
		SetContext(ctx).
		SetResult(&repo).
		Delete("/api/v1/repos/" + slug)
	if err != nil {
		return nil, fmt.Errorf("delete repo: %w", err)
	}
	if resp.IsError() {
		return nil, mapHTTPError(resp.StatusCode(), resp.Bytes())
	}
	return &repo, nil
}
