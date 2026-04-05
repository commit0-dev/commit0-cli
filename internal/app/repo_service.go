package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// RepoService manages repository operations
type RepoService struct {
	store domain.GraphStore
}

// NewRepoService creates a new repository service
func NewRepoService(store domain.GraphStore) *RepoService {
	return &RepoService{store: store}
}

// CreateRepoRequest represents a request to create a repository
type CreateRepoRequest struct {
	Slug      string
	Path      string
	RemoteURL string
	Languages []string
}

// CreateRepo creates a new repository
func (rs *RepoService) CreateRepo(ctx context.Context, req CreateRepoRequest) (*types.Repo, error) {
	// Validate inputs
	if req.Slug == "" {
		return nil, domain.Validation("repository slug cannot be empty")
	}
	if req.Path == "" {
		return nil, domain.Validation("repository path cannot be empty")
	}

	// Check if repo already exists
	existing, err := rs.store.GetRepo(ctx, req.Slug)
	if err == nil && existing != nil {
		return nil, domain.Conflict(fmt.Sprintf("repository %s already exists", req.Slug))
	}

	// Create repo
	repo := &types.Repo{
		Slug:          req.Slug,
		Path:          req.Path,
		RemoteURL:     req.RemoteURL,
		Languages:     req.Languages,
		DefaultBranch: "main",
		CreatedAt:     time.Now(),
	}

	// Persist
	if err := rs.store.UpsertRepo(ctx, repo); err != nil {
		return nil, fmt.Errorf("upsert repo: %w", err)
	}

	return repo, nil
}

// GetRepo retrieves a repository by slug
func (rs *RepoService) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	repo, err := rs.store.GetRepo(ctx, slug)
	if err != nil {
		var domainErr *domain.DomainError
		if errors.As(err, &domainErr) && domainErr.Code == domain.ErrNotFound {
			return nil, domain.NotFound(fmt.Sprintf("repository %s not found", slug))
		}
		return nil, fmt.Errorf("get repo: %w", err)
	}
	return repo, nil
}

// ListRepos lists all repositories
func (rs *RepoService) ListRepos(ctx context.Context) ([]types.Repo, error) {
	repos, err := rs.store.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return repos, nil
}

// DeleteRepo deletes a repository by slug
func (rs *RepoService) DeleteRepo(ctx context.Context, slug string) (*types.Repo, error) {
	// Get repo first to return it
	repo, err := rs.GetRepo(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Delete all nodes in repo
	if err := rs.store.DeleteNodesByRepo(ctx, slug); err != nil {
		return nil, fmt.Errorf("delete repo nodes: %w", err)
	}

	return repo, nil
}

// UpdateRepoRequest represents a request to update a repository
type UpdateRepoRequest struct {
	Slug          string
	Languages     []string
	LastCommit    string
	LastIndexedAt *types.Repo
}

// UpdateRepo updates repository metadata
func (rs *RepoService) UpdateRepo(ctx context.Context, req UpdateRepoRequest) (*types.Repo, error) {
	// Get existing repo
	repo, err := rs.GetRepo(ctx, req.Slug)
	if err != nil {
		return nil, err
	}

	// Update fields
	if len(req.Languages) > 0 {
		repo.Languages = req.Languages
	}
	if req.LastCommit != "" {
		repo.LastCommit = req.LastCommit
	}

	// Persist
	if err := rs.store.UpsertRepo(ctx, repo); err != nil {
		return nil, fmt.Errorf("update repo: %w", err)
	}

	return repo, nil
}
