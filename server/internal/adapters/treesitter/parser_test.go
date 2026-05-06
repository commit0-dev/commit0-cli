// CGO_ENABLED=1 required — tree-sitter uses C libraries.
package treesitter

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── makeNodeID ────────────────────────────────────────────────────────────────

func TestMakeNodeID_Function(t *testing.T) {
	got := makeNodeID("function", "pkg.Handler")
	want := "function:pkg⋅Handler"
	if got != want {
		t.Errorf("makeNodeID(%q, %q) = %q; want %q", "function", "pkg.Handler", got, want)
	}
}

func TestMakeNodeID_FilePath(t *testing.T) {
	got := makeNodeID("file", "path/to/file.go")
	want := "file:path⋅to⋅file⋅go"
	if got != want {
		t.Errorf("makeNodeID(%q, %q) = %q; want %q", "file", "path/to/file.go", got, want)
	}
}

func TestMakeNodeID_EmptyQualified(t *testing.T) {
	got := makeNodeID("class", "")
	want := "class:"
	if got != want {
		t.Errorf("makeNodeID(%q, %q) = %q; want %q", "class", "", got, want)
	}
}

// ── sha256Hex ─────────────────────────────────────────────────────────────────

func TestSha256Hex_Hello(t *testing.T) {
	// echo -n "hello" | sha256sum
	got := sha256Hex([]byte("hello"))
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("sha256Hex(hello) = %q; want %q", got, want)
	}
}

func TestSha256Hex_Empty(t *testing.T) {
	// SHA-256 of empty input is well-known
	got := sha256Hex([]byte{})
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("sha256Hex([]) = %q; want %q", got, want)
	}
}

// ── countLines ────────────────────────────────────────────────────────────────

func TestCountLines_ThreeLines(t *testing.T) {
	got := countLines([]byte("a\nb\nc"))
	if got != 3 {
		t.Errorf("countLines(a\\nb\\nc) = %d; want 3", got)
	}
}

func TestCountLines_Empty(t *testing.T) {
	got := countLines([]byte(""))
	if got != 1 {
		t.Errorf("countLines([]) = %d; want 1", got)
	}
}

func TestCountLines_Single(t *testing.T) {
	got := countLines([]byte("single"))
	if got != 1 {
		t.Errorf("countLines(single) = %d; want 1", got)
	}
}

func TestCountLines_TrailingNewline(t *testing.T) {
	got := countLines([]byte("a\n"))
	if got != 2 {
		t.Errorf("countLines(a\\n) = %d; want 2", got)
	}
}

// ── lastPathSegment ───────────────────────────────────────────────────────────

func TestLastPathSegment_Nested(t *testing.T) {
	got := lastPathSegment("internal/adapters/walker/fs_walker.go")
	if got != "fs_walker.go" {
		t.Errorf("lastPathSegment = %q; want %q", got, "fs_walker.go")
	}
}

func TestLastPathSegment_NoSlash(t *testing.T) {
	got := lastPathSegment("noSlash.go")
	if got != "noSlash.go" {
		t.Errorf("lastPathSegment = %q; want %q", got, "noSlash.go")
	}
}

func TestLastPathSegment_Empty(t *testing.T) {
	got := lastPathSegment("")
	if got != "" {
		t.Errorf("lastPathSegment = %q; want %q", got, "")
	}
}

// ── NewParser ─────────────────────────────────────────────────────────────────

func TestNewParser_Nil(t *testing.T) {
	p := NewParser(nil)
	if p == nil {
		t.Fatal("NewParser(nil) returned nil")
	}
}

func TestNewParser_WithLogger(t *testing.T) {
	p := NewParser(slog.Default())
	if p == nil {
		t.Fatal("NewParser(slog.Default()) returned nil")
	}
}

func TestSupportedLanguages(t *testing.T) {
	p := NewParser(nil)
	langs := p.SupportedLanguages()
	required := []string{"go", "python", "typescript", "javascript"}
	for _, req := range required {
		found := false
		for _, l := range langs {
			if l == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SupportedLanguages() missing %q; got %v", req, langs)
		}
	}
}

// ── makeFileNode ──────────────────────────────────────────────────────────────

func TestMakeFileNode(t *testing.T) {
	fe := domain.FileEntry{
		Path:     "main.go",
		Language: "go",
		Content:  []byte("package main\n"),
	}
	node := makeFileNode(fe, "abc123")

	if node.Kind != types.NodeFile {
		t.Errorf("Kind = %q; want %q", node.Kind, types.NodeFile)
	}
	if node.FilePath != "main.go" {
		t.Errorf("FilePath = %q; want %q", node.FilePath, "main.go")
	}
	if node.Language != "go" {
		t.Errorf("Language = %q; want %q", node.Language, "go")
	}
	if node.StartLine != 1 {
		t.Errorf("StartLine = %d; want 1", node.StartLine)
	}
	// "package main\n" has 2 lines (1 newline → count+1=2)
	if node.EndLine != 2 {
		t.Errorf("EndLine = %d; want 2", node.EndLine)
	}
}

// ── Parse validation ──────────────────────────────────────────────────────────

func TestParse_EmptyPath(t *testing.T) {
	p := NewParser(nil)
	_, err := p.Parse(context.Background(), domain.FileEntry{Path: ""})
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Errorf("expected DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Errorf("Code = %q; want %q", de.Code, domain.ErrValidation)
	}
}

func TestParse_EmptyContent(t *testing.T) {
	p := NewParser(nil)
	_, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "f.go",
		Language: "go",
		Content:  nil,
	})
	if err == nil {
		t.Fatal("expected error for nil content, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Errorf("expected DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Errorf("Code = %q; want %q", de.Code, domain.ErrValidation)
	}
}

func TestParse_UnsupportedLanguage(t *testing.T) {
	p := NewParser(nil)
	_, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "f.rs",
		Language: "rust",
		Content:  []byte("x"),
	})
	if err == nil {
		t.Fatal("expected error for unsupported language, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Errorf("expected DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Errorf("Code = %q; want %q", de.Code, domain.ErrValidation)
	}
}

func TestParse_ContextCancelled(t *testing.T) {
	p := NewParser(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := p.Parse(ctx, domain.FileEntry{
		Path:     "f.go",
		Language: "go",
		Content:  []byte("package main\n"),
	})
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !strings.Contains(err.Error(), "cancel") && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation error, got: %v", err)
	}
}

// ── Parse Go ──────────────────────────────────────────────────────────────────

func TestParse_GoFile(t *testing.T) {
	src := `package main

func Add(a, b int) int { return a + b }
`
	p := NewParser(nil)
	result, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "add.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if result.Path != "add.go" {
		t.Errorf("Path = %q; want %q", result.Path, "add.go")
	}
	if result.Language != "go" {
		t.Errorf("Language = %q; want %q", result.Language, "go")
	}
	if result.ContentHash == "" {
		t.Error("ContentHash is empty")
	}
	// ContentHash should be a 64-char hex string
	if len(result.ContentHash) != 64 {
		t.Errorf("ContentHash length = %d; want 64", len(result.ContentHash))
	}

	// Must have a NodeFile node
	hasFile := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeFile {
			hasFile = true
			break
		}
	}
	if !hasFile {
		t.Error("no NodeFile node found in result")
	}

	// Must have NodeFunction with Name="Add"
	hasAdd := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeFunction && n.Name == "Add" {
			hasAdd = true
			break
		}
	}
	if !hasAdd {
		t.Errorf("no NodeFunction named 'Add' found; nodes: %v", result.Nodes)
	}
}

// ── Parse Python ─────────────────────────────────────────────────────────────

func TestParse_PythonFile(t *testing.T) {
	src := `def greet(name):
    return "hello " + name
`
	p := NewParser(nil)
	result, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "greet.py",
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hasGreet := false
	for _, n := range result.Nodes {
		if n.Kind == types.NodeFunction && n.Name == "greet" {
			hasGreet = true
			break
		}
	}
	if !hasGreet {
		t.Errorf("no NodeFunction named 'greet' found; nodes: %v", result.Nodes)
	}
}

// ── Parse with return-value flow edges ──────────────────────────────────────

func TestParse_GoReturnValueFlowEdges(t *testing.T) {
	src := `package main

func fetch(url string) string { return "" }
func parse(data string) {}

func Run() {
	data := fetch("http://example.com")
	parse(data)
}
`
	p := NewParser(nil)
	result, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "main.go",
		Language: "go",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Verify return-value flow edges survive the full pipeline (including resolver pass)
	var found bool
	for _, edge := range result.Edges {
		if edge.Kind == types.EdgeDataFlow &&
			edge.Metadata["flow_type"] == "return_value" &&
			edge.Metadata["via_var"] == "data" {
			found = true
			if edge.Metadata["from_call"] != "fetch" {
				t.Errorf("from_call = %q; want %q", edge.Metadata["from_call"], "fetch")
			}
			break
		}
	}
	if !found {
		t.Error("no return_value data_flow edge via 'data' after full parse pipeline")
	}
}

func TestParse_PythonReturnValueFlowEdges(t *testing.T) {
	src := `def fetch(url):
    return ""

def handler():
    data = fetch("http://example.com")
    process(data)
`
	p := NewParser(nil)
	result, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "app.py",
		Language: "python",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var found bool
	for _, edge := range result.Edges {
		if edge.Kind == types.EdgeDataFlow &&
			edge.Metadata["flow_type"] == "return_value" &&
			edge.Metadata["via_var"] == "data" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no return_value data_flow edge via 'data' after full parse pipeline")
	}
}

func TestParse_TypeScriptReturnValueFlowEdges(t *testing.T) {
	src := `function fetch(url: string): string { return ""; }

function handler() {
  const data = fetch("http://example.com");
  process(data);
}
`
	p := NewParser(nil)
	result, err := p.Parse(context.Background(), domain.FileEntry{
		Path:     "handler.ts",
		Language: "typescript",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var found bool
	for _, edge := range result.Edges {
		if edge.Kind == types.EdgeDataFlow &&
			edge.Metadata["flow_type"] == "return_value" &&
			edge.Metadata["via_var"] == "data" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no return_value data_flow edge via 'data' after full parse pipeline")
	}
}
