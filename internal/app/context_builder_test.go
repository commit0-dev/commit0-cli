package app

import (
	"context"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestContextBuilderForFunction(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.Handler",
		Language:  "go",
		FilePath:  "main.go",
		StartLine: 10,
		EndLine:   20,
		Signature: "func(w http.ResponseWriter, r *http.Request)",
		Docstring: "Handle HTTP requests",
		Body:      "func Handler(w http.ResponseWriter, r *http.Request) { }",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, docPrefix+"[FUNCTION] pkg.Handler") {
		t.Errorf("result missing document prefix, got: %q", result)
	}
	if !strings.Contains(result, "Handle HTTP requests") {
		t.Errorf("result missing docstring")
	}
	if !strings.Contains(result, "Signature:") {
		t.Errorf("result missing signature")
	}
}

func TestContextBuilderForFunctionWithSummary(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.Handler",
		Language:  "go",
		FilePath:  "main.go",
		Summary:   "HTTP request handler for user authentication",
		Concepts:  []string{"auth", "http"},
		Signature: "func(w http.ResponseWriter, r *http.Request)",
		Body:      "func Handler() {}",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "HTTP request handler for user authentication") {
		t.Errorf("result should lead with Summary")
	}
	if !strings.Contains(result, "Concepts: auth, http") {
		t.Errorf("result should include concepts")
	}
}

func TestContextBuilderForFunctionNoSignatureNoDoc(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.Func",
		Language:  "go",
		FilePath:  "util.go",
		Body:      "func Func() {}",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[FUNCTION]") {
		t.Errorf("result missing [FUNCTION] prefix")
	}
	if strings.Contains(result, "Signature:") {
		t.Errorf("result should not have Signature: when signature is empty")
	}
	// Should fall back to location metadata
	if !strings.Contains(result, "go function defined in util.go") {
		t.Errorf("result should include fallback location metadata")
	}
}

func TestContextBuilderForClass(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeClass,
		Qualified: "pkg.User",
		Language:  "python",
		FilePath:  "models.py",
		Docstring: "User class",
		Body:      "class User: pass",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[CLASS] pkg.User") {
		t.Errorf("result missing class prefix")
	}
	if !strings.Contains(result, "User class") {
		t.Errorf("result missing docstring")
	}
}

func TestContextBuilderForFile(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:     types.NodeFile,
		FilePath: "main.go",
		Language: "go",
		Body:     "package main",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[FILE] main.go") {
		t.Errorf("result missing file prefix")
	}
}

func TestContextBuilderForModule(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeModule,
		Name:      "utils",
		Qualified: "github.com/org/utils",
		Language:  "go",
		FilePath:  "utils/",
		Body:      "module content",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[MODULE] utils") {
		t.Errorf("result missing module prefix")
	}
}

func TestContextBuilderForQuery(t *testing.T) {
	cb := NewContextBuilder(1000)
	result := cb.ForQuery("where is the error handler?")

	if !strings.Contains(result, "task: code retrieval | query:") {
		t.Errorf("result missing code retrieval task prefix")
	}
	if !strings.Contains(result, "where is the error handler?") {
		t.Errorf("result missing query text")
	}
}

func TestContextBuilderTruncate(t *testing.T) {
	cb := NewContextBuilder(10)
	result := cb.truncate("This is a very long text", 10)
	if len([]rune(result)) > 10 {
		t.Errorf("truncate(%d) returned too many runes", 10)
	}
}

func TestContextBuilderTruncateZeroMax(t *testing.T) {
	cb := NewContextBuilder(1000)
	if cb.truncate("hello", 0) != "" {
		t.Error("truncate with maxRunes=0 should return empty")
	}
}

func TestContextBuilderTruncateUnicode(t *testing.T) {
	cb := NewContextBuilder(1000)
	result := cb.truncate("Hello 世界 🌍 こんにちは", 5)
	if len([]rune(result)) > 5 {
		t.Error("truncate should count runes, not bytes")
	}
}

func TestContextBuilderNilNode(t *testing.T) {
	cb := NewContextBuilder(1000)
	if cb.ForNode(nil) != "" {
		t.Error("ForNode(nil) should return empty string")
	}
}

func TestContextBuilderDefaultMaxBodyRunes(t *testing.T) {
	cb := NewContextBuilder(0)
	if cb.maxBodyRunes != 32768 {
		t.Errorf("maxBodyRunes = %d, want 32768", cb.maxBodyRunes)
	}
}

func TestContextBuilderWithStoreNilNode(t *testing.T) {
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)
	if cb.ForNodeCtx(context.Background(), nil) != "" {
		t.Error("ForNodeCtx(nil) should return empty string")
	}
}

func TestContextBuilderForNodeCtxNoStore(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind: types.NodeFunction, Qualified: "pkg.F", Language: "go",
		FilePath: "f.go", ID: "function:pkg⋅F", Body: "func F() {}",
	}
	withCtx := cb.ForNodeCtx(context.Background(), node)
	withoutCtx := cb.ForNode(node)
	if withCtx != withoutCtx {
		t.Error("ForNodeCtx without store should equal ForNode")
	}
}

func TestContextBuilderForNodeCtxNoID(t *testing.T) {
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind: types.NodeFunction, Qualified: "pkg.F", Language: "go",
		FilePath: "f.go", ID: "", Body: "func F() {}",
	}
	result := cb.ForNodeCtx(context.Background(), node)
	if !strings.Contains(result, "[FUNCTION]") {
		t.Error("ForNodeCtx with empty ID should fall back to ForNode")
	}
}

func TestContextBuilderForNodeCtxWithNeighborhood(t *testing.T) {
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		Callees: []domain.NeighborNode{{Qualified: "pkg.G", Signature: "(x int) error"}},
		Callers: []domain.NeighborNode{{Qualified: "pkg.H"}},
	}
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind: types.NodeFunction, Qualified: "pkg.F", Language: "go",
		FilePath: "f.go", ID: "function:pkg⋅F", Body: "func F() {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "Callees:") {
		t.Error("ForNodeCtx should include Callees: line")
	}
	if !strings.Contains(result, "Callers:") {
		t.Error("ForNodeCtx should include Callers: line")
	}
	if !strings.Contains(result, "pkg.G") {
		t.Error("ForNodeCtx should include callee name")
	}
	if !strings.Contains(result, "pkg.H") {
		t.Error("ForNodeCtx should include caller name")
	}
}

func TestContextBuilderForNodeCtxEmptyNeighborhood(t *testing.T) {
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind: types.NodeFunction, Qualified: "pkg.H", Language: "go",
		FilePath: "h.go", ID: "function:pkg⋅H", Body: "func H() {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)
	expected := cb.ForNode(node)
	if result != expected {
		t.Errorf("ForNodeCtx with empty neighborhood should equal ForNode\ngot:  %q\nwant: %q", result, expected)
	}
}

func TestContextBuilderForNodeCtxDataFlow(t *testing.T) {
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		DataSinks:   []domain.NeighborNode{{Qualified: "db.Save", ParamName: "user"}},
		DataSources: []domain.NeighborNode{{Qualified: "api.Handler", ArgExpr: "req.User"}},
		Reads:       []string{"User.Email"},
		Writes:      []string{"User.UpdatedAt"},
	}
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind: types.NodeFunction, Qualified: "svc.Update", Language: "go",
		FilePath: "svc.go", ID: "function:svc⋅Update", Body: "func Update() {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "Data flows to:") {
		t.Error("expected Data flows to:")
	}
	if !strings.Contains(result, `param "user"`) {
		t.Error("expected param name in sink")
	}
	if !strings.Contains(result, "Data flows from:") {
		t.Error("expected Data flows from:")
	}
	if !strings.Contains(result, `via "req.User"`) {
		t.Error("expected arg expr in source")
	}
	if !strings.Contains(result, "Reads:") {
		t.Error("expected Reads:")
	}
	if !strings.Contains(result, "Writes:") {
		t.Error("expected Writes:")
	}
}

// ── DataFlowService tests ────────────────────────────────────────────────────

func TestDataFlowServiceBuildFlowContextEmpty(t *testing.T) {
	store := newStubGraphStore()
	svc := NewDataFlowService(store)
	if got := svc.BuildFlowContext(context.Background(), nil); got != "" {
		t.Errorf("BuildFlowContext(nil) = %q, want empty", got)
	}
}

func TestDataFlowServiceBuildFlowContextNoID(t *testing.T) {
	store := newStubGraphStore()
	svc := NewDataFlowService(store)
	results := []types.ScoredNode{{Node: types.CodeNode{Qualified: "pkg.F"}}}
	if got := svc.BuildFlowContext(context.Background(), results); got != "" {
		t.Errorf("BuildFlowContext with no-ID nodes = %q, want empty", got)
	}
}

func TestDataFlowServiceBuildFlowContextWithNeighborhood(t *testing.T) {
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		Callees:   []domain.NeighborNode{{Qualified: "db.Save"}},
		Callers:   []domain.NeighborNode{{Qualified: "api.Handler"}},
		DataSinks: []domain.NeighborNode{{Qualified: "db.Save", ArgExpr: "user"}},
		Reads:     []string{"User.Name"},
	}
	svc := NewDataFlowService(store)
	results := []types.ScoredNode{
		{Node: types.CodeNode{ID: "function:svc⋅Update", Qualified: "svc.Update", FilePath: "svc.go", StartLine: 10}},
	}

	got := svc.BuildFlowContext(context.Background(), results)

	if !strings.Contains(got, "svc.Update") {
		t.Error("expected node qualified name")
	}
	if !strings.Contains(got, "Calls:") {
		t.Error("expected Calls:")
	}
	if !strings.Contains(got, "Called by:") {
		t.Error("expected Called by:")
	}
}
