package surreal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
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
	_, err := NewSurrealAdapter(ctx, &config.SurrealConfig{URL: ""})
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
	repoRef, _ := params["repo_ref"].(models.RecordID)
	if repoRef.Table != "repo" || repoRef.ID != "myrepo" {
		t.Errorf("params[repo_ref] = %v; want repo:myrepo", repoRef)
	}
	fileRef, _ := params["file_ref"].(models.RecordID)
	if fileRef.Table != "file" || fileRef.ID != "pkg/handler.go" {
		t.Errorf("params[file_ref] = %v; want file:pkg/handler.go", fileRef)
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

func TestUpsertNodeQuery_AllTables(t *testing.T) {
	tables := []string{"function", "class", "file", "module"}
	for _, tbl := range tables {
		q := upsertNodeQuery(tbl)
		if q == "" {
			t.Errorf("upsertNodeQuery(%q) returned empty string", tbl)
		}
		if !strings.Contains(q, "UPSERT") {
			t.Errorf("upsertNodeQuery(%q) missing UPSERT: %s", tbl, q)
		}
	}
}

func TestUpsertNodeQuery_UnknownTableEmpty(t *testing.T) {
	q := upsertNodeQuery("nonexistent")
	if q != "" {
		t.Errorf("upsertNodeQuery(unknown) = %q; want empty", q)
	}
}

// ── relateEdgeQuery ───────────────────────────────────────────────────────────

func TestRelateEdgeQuery_AllKinds(t *testing.T) {
	kinds := []types.EdgeKind{
		types.EdgeCalls,
		types.EdgeImports,
		types.EdgeDefines,
		types.EdgeInherits,
		types.EdgeUses,
	}
	for _, k := range kinds {
		q := relateEdgeQuery(k)
		if q == "" {
			t.Errorf("relateEdgeQuery(%v) returned empty string", k)
		}
		if !strings.Contains(q, "RELATE") {
			t.Errorf("relateEdgeQuery(%v) missing RELATE keyword: %s", k, q)
		}
	}
}

func TestRelateEdgeQuery_UnknownKindEmpty(t *testing.T) {
	q := relateEdgeQuery(types.EdgeKind("unknown_edge"))
	if q != "" {
		t.Errorf("relateEdgeQuery(unknown) = %q; want empty", q)
	}
}

// ── edgeParams ────────────────────────────────────────────────────────────────

func TestEdgeParams_Basic(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:     types.EdgeCalls,
		FromID:   "function:pkg⋅Caller",
		ToID:     "function:pkg⋅Callee",
		CallSite: "pkg/main.go:42",
		CallType: "direct",
	}
	params := edgeParams(edge, "myrepo")

	if params["call_site"] != "pkg/main.go:42" {
		t.Errorf("params[call_site] = %v; want %q", params["call_site"], "pkg/main.go:42")
	}
	if params["call_type"] != "direct" {
		t.Errorf("params[call_type] = %v; want 'direct'", params["call_type"])
	}
	repoRef, _ := params["repo_ref"].(models.RecordID)
	if repoRef.Table != "repo" || repoRef.ID != "myrepo" {
		t.Errorf("params[repo_ref] = %v; want repo:myrepo", repoRef)
	}
}

func TestEdgeParams_EmptyCallTypeDefaultsDirect(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:   types.EdgeCalls,
		FromID: "function:pkg⋅A",
		ToID:   "function:pkg⋅B",
	}
	params := edgeParams(edge, "r")
	if params["call_type"] != "direct" {
		t.Errorf("params[call_type] = %v; want 'direct' for empty CallType", params["call_type"])
	}
}

func TestEdgeParams_MetadataInheritKind(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:     types.EdgeInherits,
		FromID:   "class:Dog",
		ToID:     "class:Animal",
		Metadata: map[string]string{"kind": "implements"},
	}
	params := edgeParams(edge, "r")
	if params["inherit_kind"] != "implements" {
		t.Errorf("params[inherit_kind] = %v; want 'implements'", params["inherit_kind"])
	}
}

func TestEdgeParams_MetadataUsageType(t *testing.T) {
	edge := &types.CodeEdge{
		Kind:     types.EdgeUses,
		FromID:   "function:pkg⋅F",
		ToID:     "class:pkg⋅Svc",
		Metadata: map[string]string{"usage_type": "injection"},
	}
	params := edgeParams(edge, "r")
	if params["usage_type"] != "injection" {
		t.Errorf("params[usage_type] = %v; want 'injection'", params["usage_type"])
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
