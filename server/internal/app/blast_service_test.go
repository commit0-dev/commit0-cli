package app

import (
	"context"
	"errors"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
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

func TestBlastServiceBlastEmptyRepoSlug(t *testing.T) {
	cfg := &config.Config{}
	svc := NewBlastService(nil, nil, cfg)

	_, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "",
	})

	if err == nil {
		t.Errorf("Blast should fail with empty repo slug")
	}

	var domErr *domain.DomainError
	if !errors.As(err, &domErr) || domErr.Code != domain.ErrValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestBlastServiceBlastDefaultMaxDepth(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{
		ID:        "f1",
		Qualified: "pkg.Func",
	}
	store.affected = []types.AffectedNode{}

	cfg := &config.Config{}
	svc := NewBlastService(store, nil, cfg)

	// MaxDepth = 0 should default to 10 (no error)
	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
		MaxDepth: 0,
	})

	if err != nil {
		t.Fatalf("Blast failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
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

func TestBlastServiceBlastRadiusError(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.blastRadiusErr = domain.Timeout("blast timed out", nil)

	cfg := &config.Config{}
	svc := NewBlastService(store, nil, cfg)

	_, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
	})

	if err == nil {
		t.Errorf("Blast should fail when BlastRadius fails")
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

func TestBlastServiceBlastSortSameHopCount(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	// Two affected nodes with same hop count — sort by Qualified name
	store.affected = []types.AffectedNode{
		{Node: types.CodeNode{ID: "f3", Qualified: "pkg.Z"}, HopCount: 1},
		{Node: types.CodeNode{ID: "f2", Qualified: "pkg.A"}, HopCount: 1},
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, nil, cfg)

	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Blast failed: %v", err)
	}
	if len(result.Affected) != 2 {
		t.Fatalf("want 2 results, got %d", len(result.Affected))
	}
	// Should be sorted by Qualified ascending when HopCount is equal
	if result.Affected[0].Node.Qualified >= result.Affected[1].Node.Qualified {
		t.Errorf("results not sorted by Qualified: %s >= %s",
			result.Affected[0].Node.Qualified, result.Affected[1].Node.Qualified)
	}
}

func TestBlastServiceBlastWithExplainerSuccess(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{
		ID: "f1", Qualified: "pkg.Func", FilePath: "main.go", Body: "func Func() {}",
	}
	store.affected = []types.AffectedNode{
		{Node: types.CodeNode{ID: "f2", Qualified: "pkg.Caller"}, HopCount: 1},
	}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Text: "impact is ", Done: false},
			{Text: "significant", Done: true},
		},
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, explainer, cfg)

	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Blast failed: %v", err)
	}
	if result.Summary != "impact is significant" {
		t.Errorf("Summary = %q, want 'impact is significant'", result.Summary)
	}
}

func TestBlastServiceBlastWithExplainerFails(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.affected = []types.AffectedNode{}

	explainer := &stubExplainer{
		err: errors.New("explainer unavailable"),
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, explainer, cfg)

	// Explainer failure is non-fatal — result should still be returned
	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Blast should succeed even when explainer fails, got: %v", err)
	}
	if result.Summary != "" {
		t.Errorf("Summary should be empty on explainer error, got %q", result.Summary)
	}
}

func TestBlastServiceBlastWithExplainerChunkError(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.affected = []types.AffectedNode{}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Error: errors.New("stream error")},
		},
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, explainer, cfg)

	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Blast should succeed even when chunk has error, got: %v", err)
	}
	// Chunk error causes break with empty explanation
	if result.Summary != "" {
		t.Errorf("Summary should be empty on chunk error, got %q", result.Summary)
	}
}

func TestBlastServiceBlastWithManyAffected(t *testing.T) {
	// Ensures minInt(5, len(affected)) path is covered when len > 5
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	affected := make([]types.AffectedNode, 8)
	for i := range affected {
		affected[i] = types.AffectedNode{
			Node:     types.CodeNode{ID: string(rune('a' + i)), Qualified: "pkg.Node" + string(rune('A'+i))},
			HopCount: i + 1,
		}
	}
	store.affected = affected

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{{Text: "many affected", Done: true}},
	}

	cfg := &config.Config{}
	svc := NewBlastService(store, explainer, cfg)

	result, err := svc.Blast(context.Background(), BlastRequest{
		Symbol:   "pkg.Func",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Blast failed: %v", err)
	}
	if result.Summary != "many affected" {
		t.Errorf("Summary = %q, want 'many affected'", result.Summary)
	}
}

func TestMinInt(t *testing.T) {
	if got := minInt(3, 5); got != 3 {
		t.Errorf("minInt(3,5) = %d, want 3", got)
	}
	if got := minInt(7, 2); got != 2 {
		t.Errorf("minInt(7,2) = %d, want 2", got)
	}
	if got := minInt(4, 4); got != 4 {
		t.Errorf("minInt(4,4) = %d, want 4", got)
	}
}
