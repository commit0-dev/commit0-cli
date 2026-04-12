package surreal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// ── nodeTable ─────────────────────────────────────────────────────────────────

func TestNodeTable(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"function", "function"},
		{"class", "class"},
		{"file", "file"},
		{"module", "module"},
		// Unknown kinds should fall back to "function".
		{"unknown_kind", "function"},
		{"", "function"},
		{"FUNCTION", "function"}, // case-sensitive; uppercase falls through to default
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := nodeTable(tt.kind)
			if got != tt.want {
				t.Errorf("nodeTable(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

// ── splitRecordID ─────────────────────────────────────────────────────────────

func TestSplitRecordID(t *testing.T) {
	tests := []struct {
		input     string
		wantTable string
		wantID    string
	}{
		{"function:pkg.Handler", "function", "pkg.Handler"},
		{"class:myPkg.MyClass", "class", "myPkg.MyClass"},
		{"nocolon", "", "nocolon"},
		{"a:b:c", "a", "b:c"}, // splits on first colon only
		{"", "", ""},
		{":", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotTable, gotID := splitRecordID(tt.input)
			if gotTable != tt.wantTable {
				t.Errorf("splitRecordID(%q) table = %q, want %q", tt.input, gotTable, tt.wantTable)
			}
			if gotID != tt.wantID {
				t.Errorf("splitRecordID(%q) id = %q, want %q", tt.input, gotID, tt.wantID)
			}
		})
	}
}

// ── recordID ─────────────────────────────────────────────────────────────────

func TestRecordID(t *testing.T) {
	rid := recordID("function", "myPkg.Handler")

	if rid.Table != "function" {
		t.Errorf("expected table 'function', got %q", rid.Table)
	}
	// The ID field is stored as the opaque string passed to NewRecordID.
	// We verify it equals the provided id string when converted back.
	if rid.ID != "myPkg.Handler" {
		t.Errorf("expected ID 'myPkg.Handler', got %v", rid.ID)
	}
}

func TestRecordID_EmptyFields(t *testing.T) {
	rid := recordID("", "")
	// Should still produce a RecordID without panicking.
	_ = rid
}

// ── rowToCodeNode ─────────────────────────────────────────────────────────────

func TestRowToCodeNode_NilID(t *testing.T) {
	row := nodeRow{
		ID:          nil,
		Name:        "Handler",
		Qualified:   "pkg.Handler",
		FilePath:    "pkg/handler.go",
		RepoSlug:    "myrepo",
		Language:    "go",
		StartLine:   10,
		EndLine:     50,
		Signature:   "func(w http.ResponseWriter, r *http.Request)",
		Docstring:   "Handler serves HTTP.",
		Body:        "func Handler() {}",
		ContentHash: "abc123",
		Embedding:   []float32{0.1, 0.2, 0.3},
		Visibility:  "public",
	}

	node := rowToCodeNode(row, types.NodeFunction)

	if node.ID != "" {
		t.Errorf("expected empty ID for nil RecordID, got %q", node.ID)
	}
	if node.Kind != types.NodeFunction {
		t.Errorf("expected kind NodeFunction, got %v", node.Kind)
	}
	if node.Name != "Handler" {
		t.Errorf("expected Name 'Handler', got %q", node.Name)
	}
	if node.Qualified != "pkg.Handler" {
		t.Errorf("expected Qualified 'pkg.Handler', got %q", node.Qualified)
	}
	if node.FilePath != "pkg/handler.go" {
		t.Errorf("expected FilePath 'pkg/handler.go', got %q", node.FilePath)
	}
	if node.RepoSlug != "myrepo" {
		t.Errorf("expected RepoSlug 'myrepo', got %q", node.RepoSlug)
	}
	if node.Language != "go" {
		t.Errorf("expected Language 'go', got %q", node.Language)
	}
	if node.StartLine != 10 {
		t.Errorf("expected StartLine 10, got %d", node.StartLine)
	}
	if node.EndLine != 50 {
		t.Errorf("expected EndLine 50, got %d", node.EndLine)
	}
	if node.Signature != "func(w http.ResponseWriter, r *http.Request)" {
		t.Errorf("unexpected Signature: %q", node.Signature)
	}
	if node.Docstring != "Handler serves HTTP." {
		t.Errorf("unexpected Docstring: %q", node.Docstring)
	}
	if node.Body != "func Handler() {}" {
		t.Errorf("unexpected Body: %q", node.Body)
	}
	if node.ContentHash != "abc123" {
		t.Errorf("unexpected ContentHash: %q", node.ContentHash)
	}
	if len(node.Embedding) != 3 {
		t.Errorf("expected 3 embedding dims, got %d", len(node.Embedding))
	}
	if node.Visibility != "public" {
		t.Errorf("expected Visibility 'public', got %q", node.Visibility)
	}
}

func TestRowToCodeNode_NonNilID(t *testing.T) {
	rid := models.NewRecordID("function", "pkg.Handler")
	row := nodeRow{
		ID:       &rid,
		Name:     "Handler",
		Language: "go",
	}

	node := rowToCodeNode(row, types.NodeFunction)

	// ID should be formatted as "table:id".
	if node.ID == "" {
		t.Fatal("expected non-empty ID when RecordID is non-nil")
	}
	// The format is "<Table>:<ID>" — table should be present.
	wantPrefix := "function:"
	if len(node.ID) < len(wantPrefix) || node.ID[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected ID to start with %q, got %q", wantPrefix, node.ID)
	}
}

func TestRowToCodeNode_NodeKindPassThrough(t *testing.T) {
	kinds := []types.NodeKind{
		types.NodeFile,
		types.NodeFunction,
		types.NodeClass,
		types.NodeModule,
	}

	for _, kind := range kinds {
		row := nodeRow{Name: "test"}
		node := rowToCodeNode(row, kind)
		if node.Kind != kind {
			t.Errorf("expected kind %v, got %v", kind, node.Kind)
		}
	}
}

func TestRowToCodeNode_NilEmbedding(t *testing.T) {
	row := nodeRow{
		Name:      "Foo",
		Embedding: nil,
	}
	node := rowToCodeNode(row, types.NodeFunction)
	if node.Embedding != nil {
		t.Errorf("expected nil embedding to be preserved, got %v", node.Embedding)
	}
}

// ── NewSurrealAdapter — empty URL validation (no network call) ─────────────────

func TestNewSurrealAdapter_EmptyURL(t *testing.T) {
	ctx := context.Background()
	_, err := NewSurrealAdapter(ctx, &config.SurrealConfig{URL: ""}, 3072)
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Fatalf("expected code %q, got %q", domain.ErrValidation, de.Code)
	}
}

// ── defaultVisibility ─────────────────────────────────────────────────────────

func TestDefaultVisibility_Empty(t *testing.T) {
	if got := defaultVisibility(""); got != "public" {
		t.Errorf("defaultVisibility(\"\") = %q; want %q", got, "public")
	}
}

func TestDefaultVisibility_NonEmpty(t *testing.T) {
	if got := defaultVisibility("private"); got != "private" {
		t.Errorf("defaultVisibility(\"private\") = %q; want %q", got, "private")
	}
}

// ── kindFromTable ─────────────────────────────────────────────────────────────

func TestKindFromTable(t *testing.T) {
	cases := []struct {
		table string
		want  types.NodeKind
	}{
		{"function", types.NodeFunction},
		{"class", types.NodeClass},
		{"file", types.NodeFile},
		{"module", types.NodeModule},
		{"unknown", types.NodeFunction}, // default fallback
		{"", types.NodeFunction},
	}
	for _, tc := range cases {
		got := kindFromTable(tc.table)
		if got != tc.want {
			t.Errorf("kindFromTable(%q) = %v; want %v", tc.table, got, tc.want)
		}
	}
}

// ── nodeParams ────────────────────────────────────────────────────────────────

func TestNodeParams_FullNode(t *testing.T) {
	node := &types.CodeNode{
		ID:          "function:pkg⋅Handler",
		Kind:        types.NodeFunction,
		Name:        "Handler",
		Qualified:   "pkg.Handler",
		FilePath:    "pkg/handler.go",
		RepoSlug:    "myrepo",
		Language:    "go",
		StartLine:   10,
		EndLine:     20,
		Signature:   "func Handler()",
		Docstring:   "Handler serves HTTP.",
		Body:        "func Handler() {}",
		ContentHash: "abc123",
		Embedding:   []float32{0.1, 0.2},
		Visibility:  "public",
	}
	params := nodeParams(node)

	if params["name"] != "Handler" {
		t.Errorf("params[name] = %v; want %q", params["name"], "Handler")
	}
	if params["qualified"] != "pkg.Handler" {
		t.Errorf("params[qualified] = %v; want %q", params["qualified"], "pkg.Handler")
	}
	if params["language"] != "go" {
		t.Errorf("params[language] = %v; want %q", params["language"], "go")
	}
	if params["visibility"] != "public" {
		t.Errorf("params[visibility] = %v; want %q", params["visibility"], "public")
	}
	if params["start_line"] != 10 {
		t.Errorf("params[start_line] = %v; want 10", params["start_line"])
	}
	repoRef, _ := params["repo"].(models.RecordID)
	if repoRef.Table != "repo" || repoRef.ID != "myrepo" {
		t.Errorf("params[repo] = %v; want repo:myrepo", repoRef)
	}
	fileRef, _ := params["file"].(models.RecordID)
	if fileRef.Table != "file" || fileRef.ID != "pkg/handler.go" {
		t.Errorf("params[file] = %v; want file:pkg/handler.go", fileRef)
	}
}

func TestNodeParams_EmptyVisibilityDefaultsToPublic(t *testing.T) {
	node := &types.CodeNode{
		ID:        "function:pkg⋅F",
		Kind:      types.NodeFunction,
		Qualified: "pkg.F",
	}
	params := nodeParams(node)
	if params["visibility"] != "public" {
		t.Errorf("expected default visibility 'public', got %v", params["visibility"])
	}
}

func TestNodeParams_EmptyIDFallsBackToQualified(t *testing.T) {
	node := &types.CodeNode{
		ID:        "", // no ID
		Kind:      types.NodeFunction,
		Qualified: "pkg.Handler",
	}
	params := nodeParams(node)
	rid, ok := params["record_id"].(models.RecordID)
	if !ok {
		t.Fatalf("expected record_id to be models.RecordID, got %T", params["record_id"])
	}
	if rid.Table != "function" {
		t.Errorf("rid.Table = %q; want 'function'", rid.Table)
	}
}

// ── upsertNodeQuery ───────────────────────────────────────────────────────────

// ── genericUpsertNodeQuery ───────────────────────────────────────────────────

func TestGenericUpsertNodeQuery(t *testing.T) {
	q := genericUpsertNodeQuery()
	if !strings.Contains(q, "UPSERT") {
		t.Errorf("genericUpsertNodeQuery missing UPSERT: %s", q)
	}
	if !strings.Contains(q, "$props") {
		t.Errorf("genericUpsertNodeQuery missing $props: %s", q)
	}
}

// ── genericRelateQuery ──────────────────────────────────────────────────────

func TestGenericRelateQuery(t *testing.T) {
	for _, label := range []string{"calls", "data_flow", "my_custom_edge"} {
		q := genericRelateQuery(label)
		if !strings.Contains(q, "RELATE") {
			t.Errorf("genericRelateQuery(%q) missing RELATE: %s", label, q)
		}
		if !strings.Contains(q, label) {
			t.Errorf("genericRelateQuery(%q) missing label: %s", label, q)
		}
	}
}

// ── genericEdgeParams ───────────────────────────────────────────────────────

func TestGenericEdgeParams_Basic(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:     types.EdgeCalls,
		FromID:   "function:pkg⋅Caller",
		ToID:     "function:pkg⋅Callee",
		CallSite: "pkg/main.go:42",
		CallType: "direct",
	}
	params := genericEdgeParams(edge, "myrepo")

	props, _ := params["props"].(map[string]any)
	if props["call_site"] != "pkg/main.go:42" {
		t.Errorf("props[call_site] = %v; want %q", props["call_site"], "pkg/main.go:42")
	}
	if props["call_type"] != "direct" {
		t.Errorf("props[call_type] = %v; want 'direct'", props["call_type"])
	}
}

func TestGenericEdgeParams_MetadataCopied(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:     types.EdgeInherits,
		FromID:   "class:Dog",
		ToID:     "class:Animal",
		Metadata: map[string]string{"kind": "implements"},
	}
	params := genericEdgeParams(edge, "r")
	props, _ := params["props"].(map[string]any)
	if props["kind"] != "implements" {
		t.Errorf("props[kind] = %v; want 'implements'", props["kind"])
	}
}

func TestGenericEdgeParams_DefaultCallType(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:   types.EdgeCalls,
		FromID: "function:pkg⋅A",
		ToID:   "function:pkg⋅B",
	}
	params := genericEdgeParams(edge, "r")
	props, _ := params["props"].(map[string]any)
	if props["call_type"] != "direct" {
		t.Errorf("props[call_type] = %v; want 'direct'", props["call_type"])
	}
}

// ── vecTablesToSearch ─────────────────────────────────────────────────────────

func TestVecTablesToSearch_Empty(t *testing.T) {
	got := vecTablesToSearch(nil)
	want := []string{"function", "class", "file", "module"}
	if len(got) != len(want) {
		t.Fatalf("vecTablesToSearch(nil) = %v; want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q; want %q", i, got[i], w)
		}
	}
}

func TestVecTablesToSearch_Specific(t *testing.T) {
	got := vecTablesToSearch([]types.NodeKind{types.NodeFunction, types.NodeClass})
	if len(got) != 2 {
		t.Fatalf("expected 2 tables, got %d: %v", len(got), got)
	}
}

func TestVecTablesToSearch_DedupSameTable(t *testing.T) {
	got := vecTablesToSearch([]types.NodeKind{types.NodeFunction, types.NodeFunction})
	if len(got) != 1 {
		t.Fatalf("expected 1 table after dedup, got %d: %v", len(got), got)
	}
	if got[0] != "function" {
		t.Errorf("got[0] = %q; want 'function'", got[0])
	}
}

// ── sortScoredNodes ───────────────────────────────────────────────────────────

func TestSortScoredNodes(t *testing.T) {
	nodes := []types.ScoredNode{
		{Node: types.CodeNode{ID: "a"}, VectorScore: 0.3},
		{Node: types.CodeNode{ID: "b"}, VectorScore: 0.9},
		{Node: types.CodeNode{ID: "c"}, VectorScore: 0.1},
	}
	sortScoredNodes(nodes)
	if nodes[0].VectorScore != 0.9 {
		t.Errorf("nodes[0].VectorScore = %.2f; want 0.9 (sorted desc)", nodes[0].VectorScore)
	}
	if nodes[2].VectorScore != 0.1 {
		t.Errorf("nodes[2].VectorScore = %.2f; want 0.1", nodes[2].VectorScore)
	}
}

func TestSortScoredNodes_Empty(t *testing.T) {
	sortScoredNodes(nil) // no panic
}

func TestSortScoredNodes_SingleElement(t *testing.T) {
	nodes := []types.ScoredNode{{Node: types.CodeNode{ID: "a"}, VectorScore: 0.5}}
	sortScoredNodes(nodes) // no panic, unchanged
	if nodes[0].VectorScore != 0.5 {
		t.Errorf("expected 0.5, got %.2f", nodes[0].VectorScore)
	}
}

// ── deduplicateVecResults ─────────────────────────────────────────────────────

func TestDeduplicateVecResults_NoDups(t *testing.T) {
	nodes := []types.ScoredNode{
		{Node: types.CodeNode{ID: "a"}, VectorScore: 0.9},
		{Node: types.CodeNode{ID: "b"}, VectorScore: 0.5},
	}
	got := deduplicateVecResults(nodes)
	if len(got) != 2 {
		t.Errorf("expected 2 results, got %d", len(got))
	}
}

func TestDeduplicateVecResults_KeepsHighestScore(t *testing.T) {
	nodes := []types.ScoredNode{
		{Node: types.CodeNode{ID: "a"}, VectorScore: 0.5},
		{Node: types.CodeNode{ID: "a"}, VectorScore: 0.9}, // same ID, higher score
	}
	got := deduplicateVecResults(nodes)
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(got))
	}
	if got[0].VectorScore != 0.9 {
		t.Errorf("expected score 0.9 (highest), got %.2f", got[0].VectorScore)
	}
}

func TestDeduplicateVecResults_Empty(t *testing.T) {
	got := deduplicateVecResults(nil)
	if got != nil && len(got) != 0 {
		t.Errorf("expected nil/empty result, got %v", got)
	}
}
