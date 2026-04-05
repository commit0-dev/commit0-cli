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

	if !strings.Contains(result, "title: [FUNCTION] pkg.Handler | text:") {
		t.Errorf("result missing title prefix, got: %q", result)
	}
	if !strings.Contains(result, "go function defined in main.go") {
		t.Errorf("result missing natural language description")
	}
	if !strings.Contains(result, "lines 10") {
		t.Errorf("result missing line numbers")
	}
	if !strings.Contains(result, "Signature:") {
		t.Errorf("result missing signature")
	}
	if !strings.Contains(result, "Handle HTTP requests") {
		t.Errorf("result missing docstring")
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
	// No Signature and no Docstring → those phrases should be absent
	if strings.Contains(result, "Signature:") {
		t.Errorf("result should not have Signature: when signature is empty")
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

	if !strings.Contains(result, "title: [CLASS] pkg.User | text:") {
		t.Errorf("result missing title prefix")
	}
	if !strings.Contains(result, "python type defined in models.py") {
		t.Errorf("result missing natural language description")
	}
	if !strings.Contains(result, "User class") {
		t.Errorf("result missing docstring")
	}
}

func TestContextBuilderForClassSmallBodyLimit(t *testing.T) {
	// When maxBodyRunes < 2048, the class body limit is capped to maxBodyRunes
	cb := NewContextBuilder(50)
	longBody := strings.Repeat("x", 200)
	node := &types.CodeNode{
		Kind:      types.NodeClass,
		Qualified: "pkg.BigClass",
		Language:  "go",
		Body:      longBody,
	}

	result := cb.ForNode(node)
	if !strings.Contains(result, "[CLASS]") {
		t.Errorf("result missing [CLASS] prefix")
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

	if !strings.Contains(result, "title: [FILE] main.go | text:") {
		t.Errorf("result missing title prefix")
	}
	if !strings.Contains(result, "go source file") {
		t.Errorf("result missing natural language description")
	}
}

func TestContextBuilderForFileSmallBodyLimit(t *testing.T) {
	// When maxBodyRunes < 4096, the file body limit is capped to maxBodyRunes
	cb := NewContextBuilder(100)
	longBody := strings.Repeat("y", 5000)
	node := &types.CodeNode{
		Kind:     types.NodeFile,
		FilePath: "big.go",
		Language: "go",
		Body:     longBody,
	}

	result := cb.ForNode(node)
	if !strings.Contains(result, "[FILE]") {
		t.Errorf("result missing [FILE] prefix")
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

	if !strings.Contains(result, "title: [MODULE] utils | text:") {
		t.Errorf("result missing title prefix")
	}
	if !strings.Contains(result, "External go package") {
		t.Errorf("result missing natural language description")
	}
}

func TestContextBuilderForModuleNoBody(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeModule,
		Name:      "empty-module",
		Qualified: "empty",
		Language:  "go",
		FilePath:  "empty/",
		Body:      "", // no body
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "[MODULE]") {
		t.Errorf("result missing [MODULE] prefix")
	}
	// Module format doesn't use "---" separator
	if strings.Contains(result, "---") {
		t.Errorf("result should not have separator for module")
	}
}

func TestContextBuilderForDefaultKind(t *testing.T) {
	// Unknown NodeKind falls through to default branch
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind: types.NodeKind("unknown"),
		Body: "some content",
	}

	result := cb.ForNode(node)

	if !strings.Contains(result, "title: code | text:") {
		t.Errorf("default kind should have title: code prefix, got: %q", result)
	}
	if !strings.Contains(result, "some content") {
		t.Errorf("default kind result should contain body")
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

	longText := "This is a very long text that should be truncated"
	result := cb.truncate(longText, 10)

	runeCount := len([]rune(result))
	if runeCount > 10 {
		t.Errorf("truncate(%d) returned %d runes", 10, runeCount)
	}
}

func TestContextBuilderTruncateZeroMax(t *testing.T) {
	cb := NewContextBuilder(1000)
	result := cb.truncate("hello world", 0)
	if result != "" {
		t.Errorf("truncate with maxRunes=0 should return empty string, got %q", result)
	}
}

func TestContextBuilderTruncateUnicode(t *testing.T) {
	cb := NewContextBuilder(1000)

	unicodeText := "Hello 世界 🌍 こんにちは"
	result := cb.truncate(unicodeText, 5)

	runeCount := len([]rune(result))
	if runeCount > 5 {
		t.Errorf("truncate should count runes, not bytes")
	}
}

func TestContextBuilderEmptyDocstring(t *testing.T) {
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.Func",
		Language:  "go",
		FilePath:  "main.go",
		Docstring: "",
		Body:      "func Func() {}",
	}

	result := cb.ForNode(node)

	// The docstring text should not appear in the output
	// (no trailing ". ." or empty sentence from empty docstring)
	if strings.Count(result, "..") > 0 {
		t.Errorf("result should not have double dots for empty docstring")
	}
}

func TestContextBuilderNilNode(t *testing.T) {
	cb := NewContextBuilder(1000)
	result := cb.ForNode(nil)

	if result != "" {
		t.Errorf("ForNode(nil) should return empty string, got %q", result)
	}
}

func TestContextBuilderDefaultMaxBodyRunes(t *testing.T) {
	// maxBodyRunes <= 0 should default to 32768
	cb := NewContextBuilder(0)
	if cb.maxBodyRunes != 32768 {
		t.Errorf("maxBodyRunes = %d, want 32768 for 0 input", cb.maxBodyRunes)
	}

	cbNeg := NewContextBuilder(-1)
	if cbNeg.maxBodyRunes != 32768 {
		t.Errorf("maxBodyRunes = %d, want 32768 for negative input", cbNeg.maxBodyRunes)
	}
}

func TestContextBuilderWithStoreNilNode(t *testing.T) {
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)
	if cb.ForNodeCtx(context.Background(), nil) != "" {
		t.Error("ForNodeCtx(nil) should return empty string")
	}
}

func TestContextBuilderWithStoreNonFunctionFallsBack(t *testing.T) {
	// Class node with empty neighborhood falls back to ForNode.
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind:      types.NodeClass,
		Qualified: "pkg.MyClass",
		Language:  "go",
		FilePath:  "pkg.go",
		ID:        "class:pkg⋅MyClass",
	}
	result := cb.ForNodeCtx(context.Background(), node)
	if !strings.Contains(result, "[CLASS]") {
		t.Error("class ForNodeCtx with empty neighborhood should fall back to ForNode class output")
	}
}

func TestContextBuilderForNodeCtxNoStore(t *testing.T) {
	// ForNodeCtx without store == ForNode
	cb := NewContextBuilder(1000)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.F",
		Language:  "go",
		FilePath:  "f.go",
		ID:        "function:pkg⋅F",
		Body:      "func F() {}",
	}
	withCtx := cb.ForNodeCtx(context.Background(), node)
	withoutCtx := cb.ForNode(node)
	if withCtx != withoutCtx {
		t.Error("ForNodeCtx without store should produce same output as ForNode")
	}
}

func TestContextBuilderForNodeCtxNoID(t *testing.T) {
	// Node with empty ID skips graph enrichment
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.F",
		Language:  "go",
		FilePath:  "f.go",
		ID:        "", // empty ID
		Body:      "func F() {}",
	}
	result := cb.ForNodeCtx(context.Background(), node)
	if !strings.Contains(result, "[FUNCTION]") {
		t.Error("ForNodeCtx with empty ID should fall back to ForNode")
	}
	if strings.Contains(result, "Calls:") || strings.Contains(result, "Called by:") {
		t.Error("ForNodeCtx with empty ID should not have Calls/Called-by lines")
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
		Kind:      types.NodeFunction,
		Qualified: "pkg.F",
		Language:  "go",
		FilePath:  "f.go",
		ID:        "function:pkg⋅F",
		Body:      "func F() {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "Calls:") {
		t.Error("ForNodeCtx with callees should include Calls: line")
	}
	if !strings.Contains(result, "Called by:") {
		t.Error("ForNodeCtx with callers should include Called by: line")
	}
	if !strings.Contains(result, "pkg.G") {
		t.Error("ForNodeCtx should include callee qualified name")
	}
	if !strings.Contains(result, "pkg.H") {
		t.Error("ForNodeCtx should include caller qualified name")
	}
}

func TestContextBuilderForNodeCtxEmptyNeighborhood(t *testing.T) {
	// Store returns empty neighborhood → falls back to ForNode output
	store := newStubGraphStore() // neighborhood nil → stub returns &Neighborhood{}
	cb := NewContextBuilderWithStore(1000, store)

	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.H",
		Language:  "go",
		FilePath:  "h.go",
		ID:        "function:pkg⋅H",
		Body:      "func H() {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	// When no hops, output equals ForNode (no Calls/Called-by lines)
	expected := cb.ForNode(node)
	if result != expected {
		t.Errorf("ForNodeCtx with empty neighborhood should equal ForNode output\ngot:  %q\nwant: %q", result, expected)
	}
}

func TestNewContextBuilderWithStore(t *testing.T) {
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(500, store)
	if cb.maxBodyRunes != 500 {
		t.Errorf("maxBodyRunes = %d, want 500", cb.maxBodyRunes)
	}
	if cb.store == nil {
		t.Error("store should not be nil")
	}
}

func TestContextBuilderForNodeCtxSignatureAndDocstring(t *testing.T) {
	// Covers the Signature and Docstring branches inside ForNodeCtx enrichment path
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		Callees: []domain.NeighborNode{{Qualified: "pkg.G", Signature: "(x int) error"}},
	}
	cb := NewContextBuilderWithStore(1000, store)

	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.F",
		Language:  "go",
		FilePath:  "f.go",
		ID:        "function:pkg⋅F",
		Signature: "func(x int) error",
		Docstring: "F does something",
		Body:      "func F(x int) error { return nil }",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "Signature:") {
		t.Error("ForNodeCtx should include Signature when set")
	}
	if !strings.Contains(result, "F does something") {
		t.Error("ForNodeCtx should include docstring when set")
	}
	if !strings.Contains(result, "pkg.G") {
		t.Error("ForNodeCtx should include neighbor qualified name")
	}
}

func TestContextBuilderForNodeCtxDataFlow(t *testing.T) {
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		DataSinks: []domain.NeighborNode{
			{Qualified: "db.Save", ParamName: "user", ArgExpr: "u"},
		},
		DataSources: []domain.NeighborNode{
			{Qualified: "api.Handler", ArgExpr: "req.User"},
		},
		Reads:  []string{"User.Email"},
		Writes: []string{"User.UpdatedAt"},
	}
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "svc.Update",
		Language:  "go",
		FilePath:  "svc.go",
		ID:        "function:svc⋅Update",
		Body:      "func Update() {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "Data flows to:") {
		t.Error("expected Data flows to: line")
	}
	if !strings.Contains(result, "db.Save") {
		t.Error("expected sink qualified name")
	}
	if !strings.Contains(result, `param "user"`) {
		t.Error("expected param name in sink")
	}
	if !strings.Contains(result, "Data flows from:") {
		t.Error("expected Data flows from: line")
	}
	if !strings.Contains(result, `via "req.User"`) {
		t.Error("expected arg expr in source")
	}
	if !strings.Contains(result, "Reads fields: User.Email") {
		t.Error("expected Reads fields: line")
	}
	if !strings.Contains(result, "Writes fields: User.UpdatedAt") {
		t.Error("expected Writes fields: line")
	}
}

func TestContextBuilderForClassCtxWithNeighborhood(t *testing.T) {
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		Callers: []domain.NeighborNode{{Qualified: "svc.UserService"}},
	}
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind:      types.NodeClass,
		Qualified: "db.UserRepo",
		Language:  "go",
		FilePath:  "db/repo.go",
		ID:        "class:db⋅UserRepo",
		Docstring: "handles user persistence",
		Body:      "type UserRepo struct {}",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "[CLASS]") {
		t.Error("expected [CLASS] prefix")
	}
	if !strings.Contains(result, "Used by:") {
		t.Error("expected Used by: line for class callers")
	}
	if !strings.Contains(result, "svc.UserService") {
		t.Error("expected caller qualified name")
	}
}

func TestContextBuilderForFileCtxWithNeighborhood(t *testing.T) {
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		Callees: []domain.NeighborNode{{Qualified: "fmt"}},
		Callers: []domain.NeighborNode{{Qualified: "pkg.Handler"}},
	}
	cb := NewContextBuilderWithStore(1000, store)
	node := &types.CodeNode{
		Kind:     types.NodeFile,
		FilePath: "handler.go",
		Language: "go",
		ID:       "file:handler.go",
		Body:     "package handler",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "[FILE]") {
		t.Error("expected [FILE] prefix")
	}
	if !strings.Contains(result, "Imports:") {
		t.Error("expected Imports: line")
	}
	if !strings.Contains(result, "Defines:") {
		t.Error("expected Defines: line")
	}
}

func TestContextBuilderForNodeCtxModuleKind(t *testing.T) {
	// ForNodeCtx with NodeModule dispatches to forModuleCtx (which was 0% coverage).
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		Callers: []domain.NeighborNode{{Qualified: "main.go"}},
	}
	cb := NewContextBuilderWithStore(1000, store)

	node := &types.CodeNode{
		Kind:      types.NodeModule,
		Name:      "encoding/json",
		Qualified: "encoding/json",
		Language:  "go",
		Docstring: "v1.2.3",
		ID:        "module:encoding/json",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "[MODULE]") {
		t.Error("expected [MODULE] prefix")
	}
	if !strings.Contains(result, "encoding/json") {
		t.Error("expected module name")
	}
	if !strings.Contains(result, "Version:") {
		t.Error("expected Version: from Docstring")
	}
	if !strings.Contains(result, "Imported by:") {
		t.Error("expected Imported by: line")
	}
	if !strings.Contains(result, "main.go") {
		t.Error("expected importer in Imported by: list")
	}
}

func TestContextBuilderForNodeCtxModuleNoDocstring(t *testing.T) {
	// forModuleCtx with no docstring and empty neighborhood.
	store := newStubGraphStore() // returns empty Neighborhood
	cb := NewContextBuilderWithStore(1000, store)

	node := &types.CodeNode{
		Kind:      types.NodeModule,
		Name:      "fmt",
		Qualified: "fmt",
		Language:  "go",
		ID:        "module:fmt",
	}

	result := cb.ForNodeCtx(context.Background(), node)

	if !strings.Contains(result, "[MODULE]") {
		t.Error("expected [MODULE] prefix")
	}
	// No docstring → no Version: phrase
	if strings.Contains(result, "Version:") {
		t.Error("should not have Version: when docstring is empty")
	}
	// Empty callers → no Imported by: line
	if strings.Contains(result, "Imported by:") {
		t.Error("should not have Imported by: line when no callers")
	}
}

func TestContextBuilderForNodeCtxModuleStoreError(t *testing.T) {
	// forModuleCtx ignores neighborhood error and continues.
	store := newStubGraphStore()
	store.err = domain.NotFound("forced error")
	cb := NewContextBuilderWithStore(1000, store)

	node := &types.CodeNode{
		Kind:      types.NodeModule,
		Name:      "os",
		Qualified: "os",
		Language:  "go",
		ID:        "module:os",
	}

	// Must not panic; returns module context without callers.
	result := cb.ForNodeCtx(context.Background(), node)
	if !strings.Contains(result, "[MODULE]") {
		t.Error("expected [MODULE] prefix even when store errors")
	}
}

func TestContextBuilderForNodeCtxUnknownKind(t *testing.T) {
	// ForNodeCtx default branch for unrecognized NodeKind falls back to ForNode.
	store := newStubGraphStore()
	cb := NewContextBuilderWithStore(1000, store)

	node := &types.CodeNode{
		Kind: types.NodeKind("symbol"),
		ID:   "symbol:x",
		Body: "some body",
	}

	result := cb.ForNodeCtx(context.Background(), node)
	expected := cb.ForNode(node)
	if result != expected {
		t.Errorf("ForNodeCtx unknown kind should equal ForNode output")
	}
}

// ── DataFlowService tests ────────────────────────────────────────────────────

func TestDataFlowServiceBuildFlowContextEmpty(t *testing.T) {
	store := newStubGraphStore()
	svc := NewDataFlowService(store)
	if got := svc.BuildFlowContext(context.Background(), nil); got != "" {
		t.Errorf("BuildFlowContext(nil) = %q, want empty", got)
	}
	if got := svc.BuildFlowContext(context.Background(), []types.ScoredNode{}); got != "" {
		t.Errorf("BuildFlowContext([]) = %q, want empty", got)
	}
}

func TestDataFlowServiceBuildFlowContextNoID(t *testing.T) {
	// Nodes without IDs are skipped; store has no neighborhood anyway.
	store := newStubGraphStore()
	svc := NewDataFlowService(store)
	results := []types.ScoredNode{
		{Node: types.CodeNode{Qualified: "pkg.F"}}, // ID is empty
	}
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
		t.Error("expected node qualified name in output")
	}
	if !strings.Contains(got, "Calls:") {
		t.Error("expected Calls: line")
	}
	if !strings.Contains(got, "Called by:") {
		t.Error("expected Called by: line")
	}
	if !strings.Contains(got, "Data flows to:") {
		t.Error("expected Data flows to: line")
	}
	if !strings.Contains(got, "Reads:") {
		t.Error("expected Reads: line")
	}
}

func TestDataFlowServiceBuildFlowContextParamName(t *testing.T) {
	// DataSink with ParamName set (hits s.ParamName != "" branch).
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		DataSinks: []domain.NeighborNode{
			{Qualified: "db.Insert", ParamName: "record"},
		},
	}
	svc := NewDataFlowService(store)
	results := []types.ScoredNode{
		{Node: types.CodeNode{ID: "function:svc⋅Create", Qualified: "svc.Create", FilePath: "svc.go"}},
	}

	got := svc.BuildFlowContext(context.Background(), results)

	if !strings.Contains(got, "Data flows to:") {
		t.Error("expected Data flows to: line")
	}
	if !strings.Contains(got, `param "record"`) {
		t.Error("expected param name in sink annotation")
	}
}

func TestDataFlowServiceBuildFlowContextDataSourcesAndWrites(t *testing.T) {
	// DataSources (with and without ArgExpr) and Writes field.
	store := newStubGraphStore()
	store.neighborhood = &domain.Neighborhood{
		DataSources: []domain.NeighborNode{
			{Qualified: "http.Request", ArgExpr: "r.Body"},
			{Qualified: "cfg.Config"}, // no ArgExpr
		},
		Writes: []string{"User.LastSeen"},
	}
	svc := NewDataFlowService(store)
	results := []types.ScoredNode{
		{Node: types.CodeNode{ID: "function:handler⋅Handle", Qualified: "handler.Handle", FilePath: "h.go"}},
	}

	got := svc.BuildFlowContext(context.Background(), results)

	if !strings.Contains(got, "Data flows from:") {
		t.Error("expected Data flows from: line")
	}
	if !strings.Contains(got, `via "r.Body"`) {
		t.Error("expected arg expr annotation")
	}
	if !strings.Contains(got, "Writes:") {
		t.Error("expected Writes: line")
	}
	if !strings.Contains(got, "User.LastSeen") {
		t.Error("expected write field in output")
	}
}

func TestDataFlowServiceBuildFlowContextAllEmptyNeighborhoods(t *testing.T) {
	// All nodes have IDs but store returns empty neighborhood → written==0 → returns "".
	store := newStubGraphStore() // returns &Neighborhood{} which IsEmpty()==true
	svc := NewDataFlowService(store)
	results := []types.ScoredNode{
		{Node: types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F"}},
		{Node: types.CodeNode{ID: "function:pkg⋅G", Qualified: "pkg.G"}},
	}

	if got := svc.BuildFlowContext(context.Background(), results); got != "" {
		t.Errorf("BuildFlowContext with all empty neighborhoods = %q, want empty", got)
	}
}
