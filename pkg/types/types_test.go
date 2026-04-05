package types

import (
	"testing"
	"time"
)

// ── NodeKind constants ────────────────────────────────────────────────────────

func TestNodeKindConstants(t *testing.T) {
	tests := []struct {
		kind NodeKind
		want string
	}{
		{NodeFile, "file"},
		{NodeFunction, "function"},
		{NodeClass, "class"},
		{NodeModule, "module"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if string(tt.kind) != tt.want {
				t.Errorf("NodeKind %q: expected string value %q, got %q", tt.kind, tt.want, string(tt.kind))
			}
		})
	}
}

// ── EdgeKind constants ────────────────────────────────────────────────────────

func TestEdgeKindConstants(t *testing.T) {
	tests := []struct {
		kind EdgeKind
		want string
	}{
		{EdgeCalls, "calls"},
		{EdgeImports, "imports"},
		{EdgeDefines, "defines"},
		{EdgeInherits, "inherits"},
		{EdgeUses, "uses"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if string(tt.kind) != tt.want {
				t.Errorf("EdgeKind %q: expected string value %q, got %q", tt.kind, tt.want, string(tt.kind))
			}
		})
	}
}

// ── CodeNode round-trip ───────────────────────────────────────────────────────

func TestCodeNode_AllFields(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3}
	n := CodeNode{
		ID:          "function:pkg.Handler",
		Kind:        NodeFunction,
		Name:        "Handler",
		Qualified:   "pkg.Handler",
		FilePath:    "pkg/handler.go",
		RepoSlug:    "my-repo",
		Language:    "go",
		StartLine:   10,
		EndLine:     50,
		Signature:   "func(w http.ResponseWriter, r *http.Request)",
		Docstring:   "Handler serves HTTP requests.",
		Body:        "func Handler() {}",
		ContentHash: "sha256abc",
		Embedding:   embedding,
		Visibility:  "public",
	}

	if n.ID != "function:pkg.Handler" {
		t.Errorf("ID mismatch: got %q", n.ID)
	}
	if n.Kind != NodeFunction {
		t.Errorf("Kind mismatch: got %v", n.Kind)
	}
	if n.Name != "Handler" {
		t.Errorf("Name mismatch: got %q", n.Name)
	}
	if n.Qualified != "pkg.Handler" {
		t.Errorf("Qualified mismatch: got %q", n.Qualified)
	}
	if n.FilePath != "pkg/handler.go" {
		t.Errorf("FilePath mismatch: got %q", n.FilePath)
	}
	if n.RepoSlug != "my-repo" {
		t.Errorf("RepoSlug mismatch: got %q", n.RepoSlug)
	}
	if n.Language != "go" {
		t.Errorf("Language mismatch: got %q", n.Language)
	}
	if n.StartLine != 10 {
		t.Errorf("StartLine mismatch: got %d", n.StartLine)
	}
	if n.EndLine != 50 {
		t.Errorf("EndLine mismatch: got %d", n.EndLine)
	}
	if n.Signature != "func(w http.ResponseWriter, r *http.Request)" {
		t.Errorf("Signature mismatch: got %q", n.Signature)
	}
	if n.Docstring != "Handler serves HTTP requests." {
		t.Errorf("Docstring mismatch: got %q", n.Docstring)
	}
	if n.Body != "func Handler() {}" {
		t.Errorf("Body mismatch: got %q", n.Body)
	}
	if n.ContentHash != "sha256abc" {
		t.Errorf("ContentHash mismatch: got %q", n.ContentHash)
	}
	if len(n.Embedding) != 3 {
		t.Errorf("Embedding length mismatch: got %d", len(n.Embedding))
	}
	if n.Visibility != "public" {
		t.Errorf("Visibility mismatch: got %q", n.Visibility)
	}
}

// ── CodeEdge ──────────────────────────────────────────────────────────────────

func TestCodeEdge_Instantiation(t *testing.T) {
	e := CodeEdge{
		Kind:      EdgeCalls,
		FromID:    "function:caller",
		ToID:      "function:callee",
		CallSite:  "main.go:42",
		IsDynamic: true,
		CallType:  "direct",
		Metadata:  map[string]string{"note": "test"},
	}

	if e.Kind != EdgeCalls {
		t.Errorf("Kind mismatch: got %v", e.Kind)
	}
	if e.FromID != "function:caller" {
		t.Errorf("FromID mismatch: got %q", e.FromID)
	}
	if e.ToID != "function:callee" {
		t.Errorf("ToID mismatch: got %q", e.ToID)
	}
	if e.CallSite != "main.go:42" {
		t.Errorf("CallSite mismatch: got %q", e.CallSite)
	}
	if !e.IsDynamic {
		t.Error("IsDynamic should be true")
	}
	if e.CallType != "direct" {
		t.Errorf("CallType mismatch: got %q", e.CallType)
	}
	if e.Metadata["note"] != "test" {
		t.Errorf("Metadata mismatch: got %v", e.Metadata)
	}
}

// ── ScoredNode ────────────────────────────────────────────────────────────────

func TestScoredNode_Instantiation(t *testing.T) {
	sn := ScoredNode{
		Node:        CodeNode{ID: "function:foo", Kind: NodeFunction, Name: "foo"},
		VectorScore: 0.95,
		FTSScore:    0.80,
		FusedScore:  0.90,
		Centrality:  42,
	}

	if sn.VectorScore != 0.95 {
		t.Errorf("VectorScore mismatch: got %v", sn.VectorScore)
	}
	if sn.FTSScore != 0.80 {
		t.Errorf("FTSScore mismatch: got %v", sn.FTSScore)
	}
	if sn.FusedScore != 0.90 {
		t.Errorf("FusedScore mismatch: got %v", sn.FusedScore)
	}
	if sn.Centrality != 42 {
		t.Errorf("Centrality mismatch: got %d", sn.Centrality)
	}
	if sn.Node.ID != "function:foo" {
		t.Errorf("Node.ID mismatch: got %q", sn.Node.ID)
	}
}

// ── QueryResult ───────────────────────────────────────────────────────────────

func TestQueryResult_Instantiation(t *testing.T) {
	qr := QueryResult{
		Nodes:       []ScoredNode{{Node: CodeNode{Name: "Handler"}, FusedScore: 0.9}},
		Explanation: "Handler processes requests.",
		Query:       "where is JWT validation?",
		RepoSlug:    "my-repo",
		Timing:      TimingInfo{TotalMS: 150},
	}

	if len(qr.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(qr.Nodes))
	}
	if qr.Explanation == "" {
		t.Error("expected non-empty Explanation")
	}
	if qr.Query != "where is JWT validation?" {
		t.Errorf("Query mismatch: got %q", qr.Query)
	}
	if qr.RepoSlug != "my-repo" {
		t.Errorf("RepoSlug mismatch: got %q", qr.RepoSlug)
	}
	if qr.Timing.TotalMS != 150 {
		t.Errorf("Timing.TotalMS mismatch: got %d", qr.Timing.TotalMS)
	}
}

// ── TraceHop ──────────────────────────────────────────────────────────────────

func TestTraceHop_Instantiation(t *testing.T) {
	hop := TraceHop{
		Depth: 2,
		Node:  CodeNode{ID: "function:foo", Name: "foo"},
		Edge:  CodeEdge{Kind: EdgeCalls, FromID: "function:bar", ToID: "function:foo"},
		Children: []TraceHop{
			{Depth: 3, Node: CodeNode{Name: "child"}},
		},
	}

	if hop.Depth != 2 {
		t.Errorf("Depth mismatch: got %d", hop.Depth)
	}
	if hop.Node.Name != "foo" {
		t.Errorf("Node.Name mismatch: got %q", hop.Node.Name)
	}
	if hop.Edge.Kind != EdgeCalls {
		t.Errorf("Edge.Kind mismatch: got %v", hop.Edge.Kind)
	}
	if len(hop.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(hop.Children))
	}
}

// ── TraceResult ───────────────────────────────────────────────────────────────

func TestTraceResult_Instantiation(t *testing.T) {
	tr := TraceResult{
		Root:        CodeNode{ID: "function:root", Name: "root"},
		Tree:        []TraceHop{{Depth: 1, Node: CodeNode{Name: "child"}}},
		Direction:   "forward",
		Explanation: "root calls child.",
		Timing:      TimingInfo{GraphMS: 20, TotalMS: 30},
	}

	if tr.Root.Name != "root" {
		t.Errorf("Root.Name mismatch: got %q", tr.Root.Name)
	}
	if tr.Direction != "forward" {
		t.Errorf("Direction mismatch: got %q", tr.Direction)
	}
	if len(tr.Tree) != 1 {
		t.Errorf("expected 1 hop, got %d", len(tr.Tree))
	}
	if tr.Timing.GraphMS != 20 {
		t.Errorf("Timing.GraphMS mismatch: got %d", tr.Timing.GraphMS)
	}
}

// ── BlastResult ───────────────────────────────────────────────────────────────

func TestBlastResult_Instantiation(t *testing.T) {
	br := BlastResult{
		Target: CodeNode{ID: "function:target", Name: "target"},
		Affected: []AffectedNode{
			{
				Node:     CodeNode{Name: "caller"},
				HopCount: 1,
				Module:   "pkg",
				Path:     "pkg/caller.go",
			},
		},
		Summary: "1 node affected.",
		Timing:  TimingInfo{TotalMS: 50},
	}

	if br.Target.Name != "target" {
		t.Errorf("Target.Name mismatch: got %q", br.Target.Name)
	}
	if len(br.Affected) != 1 {
		t.Errorf("expected 1 affected node, got %d", len(br.Affected))
	}
	if br.Affected[0].HopCount != 1 {
		t.Errorf("HopCount mismatch: got %d", br.Affected[0].HopCount)
	}
	if br.Affected[0].Module != "pkg" {
		t.Errorf("Module mismatch: got %q", br.Affected[0].Module)
	}
	if br.Affected[0].Path != "pkg/caller.go" {
		t.Errorf("Path mismatch: got %q", br.Affected[0].Path)
	}
}

// ── AffectedNode ─────────────────────────────────────────────────────────────

func TestAffectedNode_Instantiation(t *testing.T) {
	an := AffectedNode{
		Node:     CodeNode{Name: "Foo"},
		HopCount: 3,
		Module:   "auth",
		Path:     "auth/foo.go",
	}

	if an.Node.Name != "Foo" {
		t.Errorf("Node.Name mismatch: got %q", an.Node.Name)
	}
	if an.HopCount != 3 {
		t.Errorf("HopCount mismatch: got %d", an.HopCount)
	}
	if an.Module != "auth" {
		t.Errorf("Module mismatch: got %q", an.Module)
	}
}

// ── TimingInfo ────────────────────────────────────────────────────────────────

func TestTimingInfo_AllFields(t *testing.T) {
	ti := TimingInfo{
		EmbedMS:   10,
		SearchMS:  20,
		GraphMS:   30,
		ExplainMS: 40,
		TotalMS:   100,
	}

	if ti.EmbedMS != 10 {
		t.Errorf("EmbedMS mismatch: got %d", ti.EmbedMS)
	}
	if ti.SearchMS != 20 {
		t.Errorf("SearchMS mismatch: got %d", ti.SearchMS)
	}
	if ti.GraphMS != 30 {
		t.Errorf("GraphMS mismatch: got %d", ti.GraphMS)
	}
	if ti.ExplainMS != 40 {
		t.Errorf("ExplainMS mismatch: got %d", ti.ExplainMS)
	}
	if ti.TotalMS != 100 {
		t.Errorf("TotalMS mismatch: got %d", ti.TotalMS)
	}
}

// ── Repo ──────────────────────────────────────────────────────────────────────

func TestRepo_Instantiation(t *testing.T) {
	now := time.Now()
	r := Repo{
		Slug:          "my-repo",
		Path:          "/tmp/my-repo",
		RemoteURL:     "https://github.com/example/my-repo",
		DefaultBranch: "main",
		Languages:     []string{"go", "python"},
		LastCommit:    "abc123def456",
		LastIndexedAt: &now,
		CreatedAt:     now,
	}

	if r.Slug != "my-repo" {
		t.Errorf("Slug mismatch: got %q", r.Slug)
	}
	if r.Path != "/tmp/my-repo" {
		t.Errorf("Path mismatch: got %q", r.Path)
	}
	if r.RemoteURL != "https://github.com/example/my-repo" {
		t.Errorf("RemoteURL mismatch: got %q", r.RemoteURL)
	}
	if r.DefaultBranch != "main" {
		t.Errorf("DefaultBranch mismatch: got %q", r.DefaultBranch)
	}
	if len(r.Languages) != 2 {
		t.Errorf("expected 2 languages, got %d", len(r.Languages))
	}
	if r.LastCommit != "abc123def456" {
		t.Errorf("LastCommit mismatch: got %q", r.LastCommit)
	}
	if r.LastIndexedAt == nil {
		t.Error("expected non-nil LastIndexedAt")
	}
}

func TestRepo_NilLastIndexedAt(t *testing.T) {
	r := Repo{
		Slug: "new-repo",
	}
	if r.LastIndexedAt != nil {
		t.Errorf("expected nil LastIndexedAt, got %v", r.LastIndexedAt)
	}
}

// ── Zero values compile and are accessible ────────────────────────────────────

func TestZeroValues_Compile(t *testing.T) {
	// Ensure all exported types can be zero-value instantiated without panicking.
	var _ CodeNode
	var _ CodeEdge
	var _ ScoredNode
	var _ QueryResult
	var _ TraceHop
	var _ TraceResult
	var _ BlastResult
	var _ AffectedNode
	var _ TimingInfo
	var _ Repo
}
