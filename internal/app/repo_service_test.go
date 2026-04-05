package app

import (
	"context"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestRepoServiceCreateRepo(t *testing.T) {
	store := newStubGraphStore()
	svc := NewRepoService(store)
	ctx := context.Background()

	repo, err := svc.CreateRepo(ctx, CreateRepoRequest{
		Slug:      "my-repo",
		Path:      "/home/user/my-repo",
		RemoteURL: "https://github.com/user/my-repo",
		Languages: []string{"go", "python"},
	})

	if err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	if repo.Slug != "my-repo" {
		t.Errorf("Slug = %s, want my-repo", repo.Slug)
	}

	if repo.Path != "/home/user/my-repo" {
		t.Errorf("Path = %s, want /home/user/my-repo", repo.Path)
	}

	if len(repo.Languages) != 2 {
		t.Errorf("Languages = %v, want [go python]", repo.Languages)
	}
}

func TestRepoServiceCreateRepoEmptySlug(t *testing.T) {
	store := newStubGraphStore()
	svc := NewRepoService(store)
	ctx := context.Background()

	_, err := svc.CreateRepo(ctx, CreateRepoRequest{
		Slug: "",
		Path: "/path",
	})

	if err == nil {
		t.Errorf("CreateRepo should fail with empty slug")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrValidation {
		t.Errorf("error code = %s, want validation", domErr.Code)
	}
}

func TestRepoServiceCreateRepoEmptyPath(t *testing.T) {
	store := newStubGraphStore()
	svc := NewRepoService(store)
	ctx := context.Background()

	_, err := svc.CreateRepo(ctx, CreateRepoRequest{
		Slug: "my-repo",
		Path: "",
	})

	if err == nil {
		t.Errorf("CreateRepo should fail with empty path")
	}
}

func TestRepoServiceCreateRepoConflict(t *testing.T) {
	store := newStubGraphStore()
	store.repos["my-repo"] = &types.Repo{Slug: "my-repo"}

	svc := NewRepoService(store)
	ctx := context.Background()

	_, err := svc.CreateRepo(ctx, CreateRepoRequest{
		Slug: "my-repo",
		Path: "/path",
	})

	if err == nil {
		t.Errorf("CreateRepo should fail with duplicate slug")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrConflict {
		t.Errorf("error code = %s, want conflict", domErr.Code)
	}
}

func TestRepoServiceGetRepo(t *testing.T) {
	store := newStubGraphStore()
	store.repos["my-repo"] = &types.Repo{
		Slug:      "my-repo",
		Path:      "/path",
		CreatedAt: time.Now(),
	}

	svc := NewRepoService(store)
	ctx := context.Background()

	repo, err := svc.GetRepo(ctx, "my-repo")
	if err != nil {
		t.Fatalf("GetRepo failed: %v", err)
	}

	if repo.Slug != "my-repo" {
		t.Errorf("Slug = %s, want my-repo", repo.Slug)
	}
}

func TestRepoServiceGetRepoNotFound(t *testing.T) {
	store := newStubGraphStore()
	svc := NewRepoService(store)
	ctx := context.Background()

	_, err := svc.GetRepo(ctx, "nonexistent")
	if err == nil {
		t.Errorf("GetRepo should fail for non-existent repo")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrNotFound {
		t.Errorf("error code = %s, want not_found", domErr.Code)
	}
}

func TestRepoServiceListRepos(t *testing.T) {
	store := newStubGraphStore()
	store.repos["repo1"] = &types.Repo{Slug: "repo1"}
	store.repos["repo2"] = &types.Repo{Slug: "repo2"}
	store.repos["repo3"] = &types.Repo{Slug: "repo3"}

	svc := NewRepoService(store)
	ctx := context.Background()

	repos, err := svc.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos failed: %v", err)
	}

	if len(repos) != 3 {
		t.Errorf("ListRepos returned %d repos, want 3", len(repos))
	}
}

func TestRepoServiceDeleteRepo(t *testing.T) {
	store := newStubGraphStore()
	store.repos["my-repo"] = &types.Repo{
		Slug: "my-repo",
		Path: "/path",
	}

	svc := NewRepoService(store)
	ctx := context.Background()

	repo, err := svc.DeleteRepo(ctx, "my-repo")
	if err != nil {
		t.Fatalf("DeleteRepo failed: %v", err)
	}

	if repo.Slug != "my-repo" {
		t.Errorf("DeleteRepo returned repo with Slug = %s, want my-repo", repo.Slug)
	}
}

func TestRepoServiceDeleteRepoNotFound(t *testing.T) {
	store := newStubGraphStore()
	svc := NewRepoService(store)
	ctx := context.Background()

	_, err := svc.DeleteRepo(ctx, "nonexistent")
	if err == nil {
		t.Errorf("DeleteRepo should fail for non-existent repo")
	}
}
