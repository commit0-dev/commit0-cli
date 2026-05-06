package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── DefaultTaintRules ─────────────────────────────────────────────────────

func TestDefaultTaintRules_ReturnsFourRules(t *testing.T) {
	rules := DefaultTaintRules()
	if len(rules) != 4 {
		t.Errorf("expected 4 default taint rules, got %d", len(rules))
	}
}

func TestDefaultTaintRules_HasSQLInjection(t *testing.T) {
	rules := DefaultTaintRules()
	found := false
	for _, r := range rules {
		if r.Category == "sql-injection" {
			found = true
			if r.Severity != "critical" {
				t.Errorf("sql-injection severity = %q, want critical", r.Severity)
			}
		}
	}
	if !found {
		t.Error("expected sql-injection rule")
	}
}

func TestDefaultTaintRules_AllHaveNonEmptyFields(t *testing.T) {
	for _, r := range DefaultTaintRules() {
		if r.Name == "" {
			t.Errorf("rule has empty Name: %+v", r)
		}
		if r.Category == "" {
			t.Errorf("rule has empty Category: %+v", r)
		}
		if len(r.Sources) == 0 {
			t.Errorf("rule %q has no Sources", r.Name)
		}
		if len(r.Sinks) == 0 {
			t.Errorf("rule %q has no Sinks", r.Name)
		}
	}
}

// ── NewAnalysisService ────────────────────────────────────────────────────

func TestNewAnalysisService_SetsDefaultRules(t *testing.T) {
	store := newStubGraphStore()
	svc := NewAnalysisService(store, nil, nil)
	if len(svc.rules) != 4 {
		t.Errorf("expected 4 default rules, got %d", len(svc.rules))
	}
}

// ── checkTaintRule (stub — documents the empty-slice return) ──────────────

// TestCheckTaintRule_ReturnsEmptySlice documents the TODO stub in analysis_service.go.
// FindMutations is not yet implemented: the method always returns (nil, 0).
// This test MUST NOT be removed or changed to expect non-empty results until
// the implementation is provided (see TODO at analysis_service.go:147-154).
func TestCheckTaintRule_ReturnsEmptySlice(t *testing.T) {
	store := newStubGraphStore()
	svc := NewAnalysisService(store, nil, nil)
	rule := TaintRule{
		Name:     "Test Rule",
		Severity: "high",
		Category: "test",
		Sources:  []string{"req.Body"},
		Sinks:    []string{"db.Query"},
	}
	issues, scanned := svc.checkTaintRule(context.Background(), "test/repo", rule)
	if len(issues) != 0 {
		t.Errorf("checkTaintRule: expected 0 issues (stub), got %d", len(issues))
	}
	if scanned != 0 {
		t.Errorf("checkTaintRule: expected 0 scanned (stub), got %d", scanned)
	}
}

// ── Scan ──────────────────────────────────────────────────────────────────

func TestScan_EmptyGraph_NoIssues(t *testing.T) {
	store := newStubGraphStore()
	svc := NewAnalysisService(store, nil, nil)
	result, err := svc.Scan(context.Background(), "test/repo")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result == nil {
		t.Fatal("Scan returned nil result")
	}
	// With no nodes in graph, both taint rules and auth gaps find nothing.
	if result.ScannedNodes != 0 {
		t.Errorf("ScannedNodes = %d, want 0 (stub)", result.ScannedNodes)
	}
}

func TestScan_TimingIsSet(t *testing.T) {
	store := newStubGraphStore()
	svc := NewAnalysisService(store, nil, nil)
	result, err := svc.Scan(context.Background(), "test/repo")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.Timing.TotalMS < 0 {
		t.Errorf("TotalMS = %d, should be >= 0", result.Timing.TotalMS)
	}
}

// ── checkAuthGaps ──────────────────────────────────────────────────────────

func TestCheckAuthGaps_NoHandlerNodes_NoIssues(t *testing.T) {
	store := newStubGraphStore()
	svc := NewAnalysisService(store, nil, nil)
	issues := svc.checkAuthGaps(context.Background(), "test/repo")
	if len(issues) != 0 {
		t.Errorf("expected 0 auth gap issues, got %d", len(issues))
	}
}

func TestCheckAuthGaps_HandlersWithCallers_ReportsIssue(t *testing.T) {
	store := newStubGraphStore()
	// Simulate a handler node returned by ListNodes (via Concepts filter).
	handlerNode := types.CodeNode{
		ID:        "function:pkg.MyHandler",
		Qualified: "pkg.MyHandler",
		FilePath:  "handler.go",
		StartLine: 10,
		Kind:      types.NodeFunction,
	}
	store.nodesByQ["test/repo::pkg.MyHandler"] = &handlerNode
	// Make ListNodes return this node (via stubGraphStore override).
	// stubGraphStore.ListNodes returns nodeIDs when IDsOnly is set.
	// For Concepts filter (non-IDsOnly), it returns the err field.
	// We need to make it return the handler node — use a custom ListNodes behavior.

	// Instead use the analysisHandlerStore wrapper.
	customStore := &analysisHandlerStore{
		stubGraphStore: store,
		handlers:       []types.CodeNode{handlerNode},
		neighborhood: &domain.Neighborhood{
			Callers: []domain.NeighborNode{
				{Qualified: "pkg.RegisterRoutes"}, // no auth in name → issue
			},
		},
	}
	svc := NewAnalysisService(customStore, nil, nil)
	issues := svc.checkAuthGaps(context.Background(), "test/repo")
	if len(issues) == 0 {
		t.Error("expected auth gap issue for handler without auth caller")
	}
	if len(issues) > 0 && issues[0].Category != "missing-auth" {
		t.Errorf("issue category = %q, want missing-auth", issues[0].Category)
	}
}

func TestCheckAuthGaps_HandlersWithAuthCallers_NoIssue(t *testing.T) {
	store := newStubGraphStore()
	handlerNode := types.CodeNode{
		ID:        "function:pkg.MyHandler",
		Qualified: "pkg.MyHandler",
		FilePath:  "handler.go",
		Kind:      types.NodeFunction,
	}
	customStore := &analysisHandlerStore{
		stubGraphStore: store,
		handlers:       []types.CodeNode{handlerNode},
		neighborhood: &domain.Neighborhood{
			Callers: []domain.NeighborNode{
				{Qualified: "pkg.AuthMiddleware"}, // "auth" in name → no issue
			},
		},
	}
	svc := NewAnalysisService(customStore, nil, nil)
	issues := svc.checkAuthGaps(context.Background(), "test/repo")
	if len(issues) != 0 {
		t.Errorf("expected no issue when auth caller found, got %d", len(issues))
	}
}

func TestCheckAuthGaps_JWTCallerExcludesIssue(t *testing.T) {
	store := newStubGraphStore()
	handlerNode := types.CodeNode{
		ID: "function:pkg.H", Qualified: "pkg.H", FilePath: "h.go", Kind: types.NodeFunction,
	}
	customStore := &analysisHandlerStore{
		stubGraphStore: store,
		handlers:       []types.CodeNode{handlerNode},
		neighborhood: &domain.Neighborhood{
			Callers: []domain.NeighborNode{{Qualified: "pkg.JWTValidator"}},
		},
	}
	svc := NewAnalysisService(customStore, nil, nil)
	if issues := svc.checkAuthGaps(context.Background(), "r"); len(issues) != 0 {
		t.Errorf("JWT caller should suppress issue, got %d", len(issues))
	}
}

func TestCheckAuthGaps_NoCallers_NoIssue(t *testing.T) {
	// Handlers with NO callers are also not reported (they might be top-level main handlers).
	store := newStubGraphStore()
	handlerNode := types.CodeNode{
		ID: "function:pkg.H", Qualified: "pkg.H", Kind: types.NodeFunction,
	}
	customStore := &analysisHandlerStore{
		stubGraphStore: store,
		handlers:       []types.CodeNode{handlerNode},
		neighborhood:   &domain.Neighborhood{Callers: nil},
	}
	svc := NewAnalysisService(customStore, nil, nil)
	if issues := svc.checkAuthGaps(context.Background(), "r"); len(issues) != 0 {
		t.Errorf("no-callers handler should not be reported, got %d", len(issues))
	}
}

// analysisHandlerStore overrides ListNodes and Neighbors to inject test fixtures.
type analysisHandlerStore struct {
	*stubGraphStore
	handlers     []types.CodeNode
	neighborhood *domain.Neighborhood
	neighborsErr error
}

func (s *analysisHandlerStore) ListNodes(_ context.Context, _ string, opts domain.ListOpts) ([]types.CodeNode, error) {
	if len(opts.Concepts) > 0 {
		return s.handlers, nil
	}
	return nil, nil
}

func (s *analysisHandlerStore) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	if s.neighborsErr != nil {
		return nil, s.neighborsErr
	}
	return s.neighborhood, nil
}

// ── llmVerifyIssues ────────────────────────────────────────────────────────

func TestLLMVerifyIssues_EmptyIssues(t *testing.T) {
	svc := NewAnalysisService(newStubGraphStore(), nil, nil)
	result := svc.llmVerifyIssues(context.Background(), nil)
	if result != nil {
		t.Error("empty input should return nil/empty")
	}
}

func TestLLMVerifyIssues_ExplainerError_ReturnsUnfiltered(t *testing.T) {
	explainer := &stubExplainer{err: nil}
	// ExplainStructured returns nil structuredJSON → returns error from stub default
	svc := NewAnalysisService(newStubGraphStore(), nil, explainer)
	issues := []AnalysisIssue{
		{Title: "issue-1", Severity: "high"},
	}
	result := svc.llmVerifyIssues(context.Background(), issues)
	if len(result) != 1 {
		t.Errorf("on explainer error, should return original issues unchanged, got %d", len(result))
	}
}

func TestLLMVerifyIssues_ValidLLMResponse_AppliesInsights(t *testing.T) {
	resp, _ := json.Marshal(map[string]interface{}{
		"overview": "security review complete",
		"insights": []string{"check input validation"},
	})
	explainer := &stubExplainer{structuredJSON: resp}
	svc := NewAnalysisService(newStubGraphStore(), nil, explainer)
	issues := []AnalysisIssue{
		{Title: "issue-1"},
	}
	result := svc.llmVerifyIssues(context.Background(), issues)
	if len(result) == 0 {
		t.Fatal("expected issues")
	}
	if result[0].Fix == "" {
		t.Error("LLM insight should be applied as Fix suggestion")
	}
}

func TestLLMVerifyIssues_BadJSONFromLLM_ReturnsOriginal(t *testing.T) {
	explainer := &stubExplainer{structuredJSON: []byte(`not json`)}
	svc := NewAnalysisService(newStubGraphStore(), nil, explainer)
	issues := []AnalysisIssue{{Title: "X"}}
	result := svc.llmVerifyIssues(context.Background(), issues)
	if len(result) != 1 {
		t.Errorf("bad JSON should return original issues, got %d", len(result))
	}
}

// ── Scan with explainer nil (LLM verify skipped) ─────────────────────────

func TestScan_NoExplainer_SkipsLLMVerify(t *testing.T) {
	// explainer = nil → llmVerifyIssues is skipped
	store := newStubGraphStore()
	svc := NewAnalysisService(store, nil, nil)
	result, err := svc.Scan(context.Background(), "r")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	_ = result // just ensure no panic
}
