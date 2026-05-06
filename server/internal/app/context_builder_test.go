package app

import (
	"context"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

func TestContextBuilderForFunction(t *testing.T) {
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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

	if !strings.Contains(result, defaultDocPrefix+"[FUNCTION] pkg.Handler") {
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
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
	result := cb.ForQuery("where is the error handler?")

	if !strings.Contains(result, "task: code retrieval | query:") {
		t.Errorf("result missing code retrieval task prefix")
	}
	if !strings.Contains(result, "where is the error handler?") {
		t.Errorf("result missing query text")
	}
}

func TestTruncate(t *testing.T) {
	result := truncate("This is a very long text", 10)
	if len([]rune(result)) > 10 {
		t.Errorf("truncate(%d) returned too many runes", 10)
	}
}

func TestTruncateZeroMax(t *testing.T) {
	if truncate("hello", 0) != "" {
		t.Error("truncate with maxRunes=0 should return empty")
	}
}

func TestTruncateUnicode(t *testing.T) {
	result := truncate("Hello 世界 🌍 こんにちは", 5)
	if len([]rune(result)) > 5 {
		t.Error("truncate should count runes, not bytes")
	}
}

func TestContextBuilderNilNode(t *testing.T) {
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
	if cb.ForNode(nil) != "" {
		t.Error("ForNode(nil) should return empty string")
	}
}

func TestDefaultEmbedBudget(t *testing.T) {
	b := domain.DefaultEmbedBudget(2048)
	if b.Total <= 0 {
		t.Error("DefaultEmbedBudget should have positive Total")
	}
	if b.Body <= 0 {
		t.Error("DefaultEmbedBudget should allocate Body budget")
	}
}

func TestContextBuilderWithStoreNilNode(t *testing.T) {
	store := newStubGraphStore()
	cb := NewContextBuilderWithGraph(domain.DefaultEmbedBudget(500), store)
	if cb.ForNodeCtx(context.Background(), nil) != "" {
		t.Error("ForNodeCtx(nil) should return empty string")
	}
}

func TestContextBuilderForNodeCtxNoStore(t *testing.T) {
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))
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
	cb := NewContextBuilderWithGraph(domain.DefaultEmbedBudget(500), store)
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
	cb := NewContextBuilderWithGraph(domain.DefaultEmbedBudget(500), store)
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
	cb := NewContextBuilderWithGraph(domain.DefaultEmbedBudget(500), store)
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
	cb := NewContextBuilderWithGraph(domain.DefaultEmbedBudget(500), store)
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

// ── forNodeWithNeighborhood tests ────────────────────────────────────────────────

func TestContextBuilderForNodeWithNeighborhood_AllFieldsPopulated(t *testing.T) {
	// Exercise all branches in forNodeWithNeighborhood (lines 67-128)
	cb := NewContextBuilder(domain.DefaultEmbedBudget(2000))

	node := &types.CodeNode{
		ID:        "fn:auth⋅validateToken",
		Kind:      types.NodeFunction,
		Qualified: "auth.validateToken",
		Language:  "go",
		FilePath:  "auth.go",
		StartLine: 42,
		EndLine:   88,
		Summary:   "Validates JWT token",
		Docstring: "Validates JWT tokens using RS256 signature",
		Signature: "func(token string) (bool, error)",
		Concepts:  []string{"security", "jwt", "cryptography"},
		Body:      "func validateToken(token string) (bool, error) { ... }",
	}

	nb := &domain.Neighborhood{
		Callers: []domain.NeighborNode{
			{Qualified: "api.Handler"},
			{Qualified: "middleware.Auth"},
		},
		Callees: []domain.NeighborNode{
			{Qualified: "jwt.Verify", Signature: "(token string, key []byte) bool"},
			{Qualified: "log.Error", Signature: "(msg string)"},
		},
		DataSinks: []domain.NeighborNode{
			{Qualified: "db.LogAttempt", ParamName: "userId"},
			{Qualified: "cache.Set", ParamName: "token"},
		},
		DataSources: []domain.NeighborNode{
			{Qualified: "http.Header", ArgExpr: "r.Header.Get(\"Authorization\")"},
		},
		Reads:  []string{"User.LastLogin", "Config.JWTSecret"},
		Writes: []string{"AuditLog.Entry", "Cache.TokenSet"},
	}

	result := cb.forNodeWithNeighborhood(node, nb)

	// Verify all sections are present
	if !strings.Contains(result, defaultDocPrefix) {
		t.Error("missing document prefix")
	}
	if !strings.Contains(result, "[FUNCTION]") {
		t.Error("missing FUNCTION kind")
	}
	if !strings.Contains(result, "auth.validateToken") {
		t.Error("missing qualified name")
	}
	if !strings.Contains(result, "Validates JWT token") {
		t.Error("missing summary")
	}
	if !strings.Contains(result, "Concepts:") {
		t.Error("missing Concepts section")
	}
	if !strings.Contains(result, "security, jwt, cryptography") {
		t.Error("missing concept values")
	}
	if !strings.Contains(result, "Signature:") {
		t.Error("missing Signature section")
	}
	if !strings.Contains(result, "Callers:") {
		t.Error("missing Callers section")
	}
	if !strings.Contains(result, "api.Handler") {
		t.Error("missing caller name")
	}
	if !strings.Contains(result, "Callees:") {
		t.Error("missing Callees section")
	}
	if !strings.Contains(result, "jwt.Verify") {
		t.Error("missing callee name")
	}
	if !strings.Contains(result, "Data flows to:") {
		t.Error("missing data sink section")
	}
	if !strings.Contains(result, "param") && !strings.Contains(result, "userId") {
		t.Error("missing param name in sink")
	}
	if !strings.Contains(result, "Data flows from:") {
		t.Error("missing data source section")
	}
	if !strings.Contains(result, `Authorization`) {
		t.Error("missing arg expr in source")
	}
	if !strings.Contains(result, "Reads:") {
		t.Error("missing Reads section")
	}
	if !strings.Contains(result, "User.LastLogin") {
		t.Error("missing read field")
	}
	if !strings.Contains(result, "Writes:") {
		t.Error("missing Writes section")
	}
	if !strings.Contains(result, "AuditLog.Entry") {
		t.Error("missing written field")
	}
	if !strings.Contains(result, "---") {
		t.Error("missing body separator")
	}
	if !strings.Contains(result, "func validateToken") {
		t.Error("missing body content")
	}
}

func TestContextBuilderForNodeWithNeighborhood_PartialNeighborhood(t *testing.T) {
	// Test with only some neighborhood fields populated
	cb := NewContextBuilder(domain.DefaultEmbedBudget(1000))

	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "util.Parse",
		Language:  "go",
		FilePath:  "util.go",
		Body:      "func Parse() {}",
	}

	nb := &domain.Neighborhood{
		Callees: []domain.NeighborNode{{Qualified: "json.Unmarshal"}},
		Writes:  []string{"buffer"},
	}

	result := cb.forNodeWithNeighborhood(node, nb)

	if !strings.Contains(result, "Callees:") {
		t.Error("should include Callees")
	}
	if !strings.Contains(result, "Writes:") {
		t.Error("should include Writes")
	}
	if strings.Contains(result, "Callers:") {
		t.Error("should not include Callers when empty")
	}
	if strings.Contains(result, "Reads:") {
		t.Error("should not include Reads when empty")
	}
}

func TestContextBuilderForNodeWithNeighborhood_BudgetEnforced(t *testing.T) {
	// Test that total budget is enforced (line 139-141)
	smallBudget := domain.TokenBudget{
		Prefix:    100,
		Summary:   100,
		Signature: 100,
		Neighbors: 100,
		Body:      100,
		Total:     300, // Small total
	}
	cb := NewContextBuilder(smallBudget)

	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "pkg.VeryLongFunctionNameWithLotsOfDetails",
		Summary:   strings.Repeat("summary ", 100),
		Signature: strings.Repeat("sig ", 100),
		Body:      strings.Repeat("body ", 100),
	}

	nb := &domain.Neighborhood{
		Callers: []domain.NeighborNode{
			{Qualified: strings.Repeat("caller", 50)},
		},
	}

	result := cb.forNodeWithNeighborhood(node, nb)

	// Total length should not exceed budget (or be close to it)
	// Allow some flexibility since truncate() counts runes and formatting adds overhead
	if len(result) > smallBudget.Total*2 {
		t.Errorf("result length (%d) far exceeds total budget (%d)", len(result), smallBudget.Total)
	}
}

func TestContextBuilderForNodeWithNeighborhood_EmptyNeighborhoodFields(t *testing.T) {
	// Test when neighborhood exists but all fields are empty
	cb := NewContextBuilder(domain.DefaultEmbedBudget(500))

	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "empty.Func",
		Language:  "go",
		FilePath:  "empty.go",
		Body:      "func Func() {}",
	}

	nb := &domain.Neighborhood{}

	result := cb.forNodeWithNeighborhood(node, nb)

	// Should not error; should just have basic node info
	if !strings.Contains(result, "empty.Func") {
		t.Error("should include qualified name even with empty neighborhood")
	}
}

func TestContextBuilderForNodeWithNeighborhood_DataFlowVariations(t *testing.T) {
	// Test different combinations of DataSinks/DataSources with/without metadata
	cb := NewContextBuilder(domain.DefaultEmbedBudget(1000))

	node := &types.CodeNode{
		Kind:      types.NodeFunction,
		Qualified: "handler.POST",
		Language:  "go",
		FilePath:  "handler.go",
		Body:      "func POST() {}",
	}

	nb := &domain.Neighborhood{
		DataSinks: []domain.NeighborNode{
			{Qualified: "db.Save", ParamName: "user"}, // with param
			{Qualified: "api.Call", ParamName: ""},    // empty param
			{Qualified: "log.Print"},                  // no param at all
		},
		DataSources: []domain.NeighborNode{
			{Qualified: "request.Parse", ArgExpr: "body"}, // with arg
			{Qualified: "cache.Get", ArgExpr: ""},         // empty arg
			{Qualified: "env.Var"},                        // no arg
		},
	}

	result := cb.forNodeWithNeighborhood(node, nb)

	if !strings.Contains(result, "Data flows to:") {
		t.Error("should include Data flows to:")
	}
	if !strings.Contains(result, "db.Save") {
		t.Error("should include all DataSinks")
	}
	if !strings.Contains(result, "Data flows from:") {
		t.Error("should include Data flows from:")
	}
	if !strings.Contains(result, "request.Parse") {
		t.Error("should include all DataSources")
	}
}

func TestNodeLabel_AllKinds(t *testing.T) {
	cb := NewContextBuilder(domain.DefaultEmbedBudget(2048))

	cases := []struct {
		name string
		node *types.CodeNode
		want string
	}{
		{"file", &types.CodeNode{Kind: types.NodeFile, FilePath: "/a/b.go"}, "/a/b.go"},
		{"module_with_name", &types.CodeNode{Kind: types.NodeModule, Name: "pkgname", Qualified: "ignored"}, "pkgname"},
		{"module_no_name", &types.CodeNode{Kind: types.NodeModule, Qualified: "fallback"}, "fallback"},
		{"function", &types.CodeNode{Kind: types.NodeFunction, Qualified: "p.F"}, "p.F"},
		{"class", &types.CodeNode{Kind: types.NodeClass, Qualified: "p.T"}, "p.T"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cb.nodeLabel(tc.node); got != tc.want {
				t.Errorf("nodeLabel(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
