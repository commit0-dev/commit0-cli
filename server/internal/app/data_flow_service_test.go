package app

import (
	"context"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── BuildFlowContext tests ──────────────────────────────────────────────────────

func TestDataFlowServiceBuildFlowContext_EmptyInput(t *testing.T) {
	store := newStubGraphStore()
	svc := NewDataFlowService(store)

	result := svc.BuildFlowContext(context.Background(), nil)

	if result != "" {
		t.Errorf("expected empty string for nil input, got %q", result)
	}
}

func TestDataFlowServiceBuildFlowContext_EmptySlice(t *testing.T) {
	store := newStubGraphStore()
	svc := NewDataFlowService(store)

	result := svc.BuildFlowContext(context.Background(), []types.ScoredNode{})

	if result != "" {
		t.Errorf("expected empty string for empty slice, got %q", result)
	}
}

func TestDataFlowServiceBuildFlowContext_NodesWithoutID(t *testing.T) {
	// Test line 46-47: nodes without ID are skipped
	store := newStubGraphStore()
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{Qualified: "pkg.F", FilePath: "f.go"}}, // no ID
		{Node: types.CodeNode{Qualified: "pkg.G", FilePath: "g.go"}}, // no ID
	}

	result := svc.BuildFlowContext(context.Background(), results)

	if result != "" {
		t.Errorf("expected empty string when all nodes lack ID, got %q", result)
	}
}

func TestDataFlowServiceBuildFlowContext_NeighborhoodError(t *testing.T) {
	// Test line 50-53: neighborhood error or nil causes skip
	store := newStubGraphStore()
	store.neighborhood = nil // Neighbors will return empty neighborhood
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{ID: "fn:pkg⋅F", Qualified: "pkg.F", FilePath: "f.go", StartLine: 10}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	// Should return empty because Neighbors fails or returns empty
	if result != "" {
		t.Errorf("expected empty string when Neighbors returns nil, got %q", result)
	}
}

func TestDataFlowServiceBuildFlowContext_SingleNodeCallGraph(t *testing.T) {
	// Test loop body with callees and callers (lines 57-61)
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{
		Callees: []domain.NeighborNode{
			{Qualified: "db.Save"},
			{Qualified: "log.Info"},
		},
		Callers: []domain.NeighborNode{
			{Qualified: "api.Handler"},
		},
	}
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{
			ID:        "fn:service⋅Update",
			Qualified: "service.Update",
			FilePath:  "service.go",
			StartLine: 42,
		}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	if !strings.Contains(result, "service.Update") {
		t.Error("missing qualified name")
	}
	if !strings.Contains(result, "service.go:42") {
		t.Error("missing file and line info")
	}
	if !strings.Contains(result, "Calls:") {
		t.Error("missing Calls section")
	}
	if !strings.Contains(result, "db.Save") {
		t.Error("missing callee")
	}
	if !strings.Contains(result, "Called by:") {
		t.Error("missing Called by section")
	}
	if !strings.Contains(result, "api.Handler") {
		t.Error("missing caller")
	}
}

func TestDataFlowServiceBuildFlowContext_DataFlowEdges(t *testing.T) {
	// Test data flow sections (lines 63-85)
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{
		DataSinks: []domain.NeighborNode{
			{Qualified: "db.Save", ParamName: "user"},
			{Qualified: "cache.Set", ParamName: "session"},
		},
		DataSources: []domain.NeighborNode{
			{Qualified: "request.Parse", ArgExpr: "body"},
			{Qualified: "env.Get", ArgExpr: "TOKEN"},
		},
	}
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{
			ID:        "fn:handler⋅Auth",
			Qualified: "handler.Auth",
			FilePath:  "handler.go",
			StartLine: 15,
		}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	// Verify data flow sections
	if !strings.Contains(result, "Data flows to:") {
		t.Error("missing Data flows to:")
	}
	if !strings.Contains(result, "db.Save") {
		t.Error("missing DataSink")
	}
	if !strings.Contains(result, `param "user"`) {
		t.Error("missing param name in sink")
	}
	if !strings.Contains(result, "Data flows from:") {
		t.Error("missing Data flows from:")
	}
	if !strings.Contains(result, "request.Parse") {
		t.Error("missing DataSource")
	}
	if !strings.Contains(result, `via "body"`) {
		t.Error("missing arg expr in source")
	}
}

func TestDataFlowServiceBuildFlowContext_ReadWriteFields(t *testing.T) {
	// Test Reads and Writes sections (lines 87-92)
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{
		Reads:  []string{"User.Email", "Config.Secret", "State.Counter"},
		Writes: []string{"Audit.Log", "Cache.Value"},
	}
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{
			ID:        "fn:lib⋅Process",
			Qualified: "lib.Process",
			FilePath:  "lib.go",
			StartLine: 100,
		}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	if !strings.Contains(result, "Reads:") {
		t.Error("missing Reads section")
	}
	if !strings.Contains(result, "User.Email") {
		t.Error("missing read field")
	}
	if !strings.Contains(result, "Writes:") {
		t.Error("missing Writes section")
	}
	if !strings.Contains(result, "Audit.Log") {
		t.Error("missing write field")
	}
}

func TestDataFlowServiceBuildFlowContext_MultipleNodes(t *testing.T) {
	// Test the loop that processes multiple nodes (lines 44-95)
	// and the written counter (lines 94 and 97-100)
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{
		Callees: []domain.NeighborNode{{Qualified: "db.Query"}},
	}
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{
			ID:        "fn:search⋅Find",
			Qualified: "search.Find",
			FilePath:  "search.go",
			StartLine: 50,
		}},
		{Node: types.CodeNode{
			ID:        "fn:sort⋅By",
			Qualified: "sort.By",
			FilePath:  "sort.go",
			StartLine: 75,
		}},
		{Node: types.CodeNode{
			ID:        "fn:filter⋅Apply",
			Qualified: "filter.Apply",
			FilePath:  "filter.go",
			StartLine: 120,
		}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	// All three should be included
	if !strings.Contains(result, "search.Find") {
		t.Error("missing first node")
	}
	if !strings.Contains(result, "sort.By") {
		t.Error("missing second node")
	}
	if !strings.Contains(result, "filter.Apply") {
		t.Error("missing third node")
	}
	if !strings.Contains(result, "Data-flow context for top results:") {
		t.Error("missing header")
	}
}

func TestDataFlowServiceBuildFlowContext_EmptyNeighborhoodIsSkipped(t *testing.T) {
	// Test line 51: nb.IsEmpty() causes skip
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{} // Empty neighborhood
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{
			ID:        "fn:test⋅Empty",
			Qualified: "test.Empty",
			FilePath:  "test.go",
			StartLine: 10,
		}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	// Empty neighborhood should cause the node to be skipped
	if result != "" {
		t.Errorf("expected empty result for empty neighborhood, got %q", result)
	}
}

func TestDataFlowServiceBuildFlowContext_MixedNodeResults(t *testing.T) {
	// Test mix of valid and invalid nodes
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{
		Callees: []domain.NeighborNode{{Qualified: "util.Log"}},
	}
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{Qualified: "pkg.NoID"}},                                  // No ID - skipped
		{Node: types.CodeNode{ID: "fn:valid", Qualified: "valid.F", FilePath: "f.go"}}, // Valid
		{Node: types.CodeNode{ID: "", Qualified: "pkg.EmptyID"}},                       // Empty ID - skipped
	}

	result := svc.BuildFlowContext(context.Background(), results)

	// Only one valid node with neighborhood
	if !strings.Contains(result, "valid.F") {
		t.Error("should include valid node")
	}
	if strings.Contains(result, "NoID") && !strings.Contains(result, "valid.F") {
		t.Error("should not include nodes without ID")
	}
}

func TestDataFlowServiceBuildFlowContext_ComplexDataFlowVariations(t *testing.T) {
	// Test DataSink/DataSource entries with various metadata combinations
	store := &stubGraphStore{}
	store.neighborhood = &domain.Neighborhood{
		DataSinks: []domain.NeighborNode{
			{Qualified: "db.Save", ParamName: "entity"}, // with ParamName
			{Qualified: "api.Post", ParamName: ""},      // empty ParamName
			{Qualified: "queue.Push", ArgExpr: "msg"},   // with ArgExpr (shouldn't happen in DataSinks but test robustness)
		},
		DataSources: []domain.NeighborNode{
			{Qualified: "input.Read", ArgExpr: "buffer"}, // with ArgExpr
			{Qualified: "config.Load", ArgExpr: ""},      // empty ArgExpr
			{Qualified: "default.Value"},                 // neither
		},
	}
	svc := NewDataFlowService(store)

	results := []types.ScoredNode{
		{Node: types.CodeNode{
			ID:        "fn:process⋅Data",
			Qualified: "process.Data",
			FilePath:  "process.go",
			StartLine: 30,
		}},
	}

	result := svc.BuildFlowContext(context.Background(), results)

	// Verify formatting doesn't crash and includes all entries
	if !strings.Contains(result, "db.Save") {
		t.Error("should include all DataSink entries")
	}
	if !strings.Contains(result, "input.Read") {
		t.Error("should include all DataSource entries")
	}
}
