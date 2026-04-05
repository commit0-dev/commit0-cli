package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestBlastServiceBlastSuccess(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.UserService.Create"] = &types.CodeNode{
		ID:        "f1",
		Qualified: "pkg.UserService.Create",
		Kind:      types.NodeFunction,
	}
	store.affected = []types.AffectedNode{
		{Node: types.CodeNode{ID: "f2", Qualified: "pkg.Controller.Handle"}, HopCount: 1},
		{Node: types.CodeNode{ID: "f3", Qualified: "pkg.Validator.Check"}, HopCount: 2},
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, nil, cfg)

	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.UserService.Create",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Blast failed: %v", err)
	}

	if result.Target.Qualified != "pkg.UserService.Create" {
		t.Errorf("Target.Qualified = %s, want pkg.UserService.Create", result.Target.Qualified)
	}

	if len(result.Affected) != 2 {
		t.Errorf("Affected = %d nodes, want 2", len(result.Affected))
	}

	// Verify sorted by hop count
	if result.Affected[0].HopCount > result.Affected[1].HopCount {
		t.Errorf("Affected not sorted by hop count")
	}
}

func TestBlastServiceBlastEmptySymbol(t *testing.T) {
	cfg := &config.Config{}
	svc := NewBlastService(nil, nil, cfg)

	_, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "",
		RepoSlug: "my-repo",
	})

	if err == nil {
		t.Errorf("Blast should fail with empty symbol")
	}
}

func TestBlastServiceBlastNotFound(t *testing.T) {
	store := newStubGraphStore()
	cfg := &config.Config{}
	svc := NewBlastService(store, nil, cfg)

	_, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "nonexistent",
		RepoSlug: "my-repo",
	})

	if err == nil {
		t.Errorf("Blast should fail for non-existent symbol")
	}
}

func TestBlastServiceBlastDedup(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.Func"] = &types.CodeNode{
		ID:        "f1",
		Qualified: "pkg.Func",
	}
	// Add duplicate affected nodes with different hop counts
	store.affected = []types.AffectedNode{
		{Node: types.CodeNode{ID: "f2"}, HopCount: 2},
		{Node: types.CodeNode{ID: "f2"}, HopCount: 1}, // duplicate with lower hop count
		{Node: types.CodeNode{ID: "f3"}, HopCount: 3},
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, nil, cfg)

	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Blast failed: %v", err)
	}

	// Should have 2 unique nodes (f2 and f3)
	if len(result.Affected) != 2 {
		t.Errorf("Affected = %d nodes, want 2 (after dedup)", len(result.Affected))
	}

	// f2 should have hop count 1 (kept the lowest)
	for _, aff := range result.Affected {
		if aff.Node.ID == "f2" && aff.HopCount != 1 {
			t.Errorf("f2 HopCount = %d, want 1 (lowest)", aff.HopCount)
		}
	}
}
