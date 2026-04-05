package gemini

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"log/slog"
)

// ---------------------------------------------------------------------------
// NewGeminiExplainer — nil client
// ---------------------------------------------------------------------------

func TestNewGeminiExplainerNilClient_Explainer(t *testing.T) {
	cfg := &config.GeminiConfig{}
	_, err := NewGeminiExplainer(nil, cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Fatalf("expected code %q, got %q", domain.ErrValidation, de.Code)
	}
}

// ---------------------------------------------------------------------------
// NewGeminiExplainer — default model
// ---------------------------------------------------------------------------

func TestNewGeminiExplainerDefaultModel(t *testing.T) {
	cfg := &config.GeminiConfig{ExplainModel: ""}
	ex, err := NewGeminiExplainer(&genai.Client{}, cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex.model != "gemini-2.0-flash" {
		t.Fatalf("expected default model %q, got %q", "gemini-2.0-flash", ex.model)
	}
}

func TestNewGeminiExplainerCustomModel(t *testing.T) {
	cfg := &config.GeminiConfig{ExplainModel: "gemini-custom-model"}
	ex, err := NewGeminiExplainer(&genai.Client{}, cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex.model != "gemini-custom-model" {
		t.Fatalf("expected model %q, got %q", "gemini-custom-model", ex.model)
	}
}

// ---------------------------------------------------------------------------
// buildExplainPrompt — query type routing
// ---------------------------------------------------------------------------

func TestBuildExplainPrompt_SearchType(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "where is JWT validation?",
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "Answer the developer's question")
	mustContain(t, out, req.UserQuery)
	mustContain(t, out, "## Answer")
}

func TestBuildExplainPrompt_TraceType(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "trace",
		UserQuery: "trace ServeHTTP",
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "call chain")
	mustContain(t, out, req.UserQuery)
}

func TestBuildExplainPrompt_BlastType(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "blast",
		UserQuery: "blast UserService.Create",
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "blast radius")
	mustContain(t, out, req.UserQuery)
}

func TestBuildExplainPrompt_UnknownType(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "",
		UserQuery: "some question",
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "Analyse the code excerpts")
	mustContain(t, out, req.UserQuery)
}

// ---------------------------------------------------------------------------
// buildExplainPrompt — code context section
// ---------------------------------------------------------------------------

func TestBuildExplainPrompt_NoCodeContext(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "no context question",
	}
	out := buildExplainPrompt(req)

	if strings.Contains(out, "## Relevant Code") {
		t.Error("expected no 'Relevant Code' section when CodeContext is empty")
	}
}

func TestBuildExplainPrompt_CodeContextWithAllFields(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "find auth",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.Handler.Authenticate",
				FilePath:  "pkg/handler.go",
				Lines:     "42-55",
				Snippet:   "func (h *Handler) Authenticate() error {\n\treturn nil\n}\n",
				Score:     0.987,
			},
		},
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "## Relevant Code")
	mustContain(t, out, "pkg.Handler.Authenticate")
	mustContain(t, out, "pkg/handler.go")
	mustContain(t, out, "42-55")
	mustContain(t, out, "0.987")
	mustContain(t, out, "```")
	mustContain(t, out, "func (h *Handler) Authenticate()")
}

func TestBuildExplainPrompt_CodeContextSnippetNoTrailingNewline(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "find auth",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.Foo",
				Snippet:   "func Foo() {}", // no trailing newline
			},
		},
	}
	out := buildExplainPrompt(req)

	// The closing ``` must appear on its own line, meaning a newline was
	// inserted after the snippet.
	mustContain(t, out, "func Foo() {}\n```")
}

func TestBuildExplainPrompt_CodeContextSnippetTrailingNewlineNotDoubled(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "find auth",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.Bar",
				Snippet:   "func Bar() {}\n", // already has trailing newline
			},
		},
	}
	out := buildExplainPrompt(req)

	// Should NOT have a double newline before ```.
	if strings.Contains(out, "func Bar() {}\n\n```") {
		t.Error("trailing newline was doubled — expected only one newline before closing ```")
	}
	mustContain(t, out, "func Bar() {}\n```")
}

func TestBuildExplainPrompt_CodeExcerptNoFilePath(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "find something",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.NoPath",
				FilePath:  "", // no file path
				Lines:     "1-10",
				Snippet:   "x := 1",
			},
		},
	}
	out := buildExplainPrompt(req)

	if strings.Contains(out, "*File:*") {
		t.Error("expected no '*File:*' line when FilePath is empty")
	}
}

func TestBuildExplainPrompt_CodeExcerptFilePathNoLines(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "find something",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.WithFileNoLines",
				FilePath:  "some/file.go",
				Lines:     "", // no lines
			},
		},
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "*File:* `some/file.go`")
	if strings.Contains(out, "*Lines:*") {
		t.Error("expected no '*Lines:*' when Lines is empty")
	}
}

func TestBuildExplainPrompt_CodeContextScoreZero(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "find something",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.Zero",
				Score:     0, // zero score — should be omitted
			},
		},
	}
	out := buildExplainPrompt(req)

	if strings.Contains(out, "Relevance score") {
		t.Error("expected no relevance score line when Score is 0")
	}
}

// ---------------------------------------------------------------------------
// buildExplainPrompt — graph context section
// ---------------------------------------------------------------------------

func TestBuildExplainPrompt_GraphContextPresent(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType:    "trace",
		UserQuery:    "trace auth",
		GraphContext: "A -> B -> C",
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "## Graph Context")
	mustContain(t, out, "A -> B -> C")
}

func TestBuildExplainPrompt_GraphContextAbsent(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType:    "search",
		UserQuery:    "something",
		GraphContext: "",
	}
	out := buildExplainPrompt(req)

	if strings.Contains(out, "## Graph Context") {
		t.Error("expected no '## Graph Context' section when GraphContext is empty")
	}
}

// ---------------------------------------------------------------------------
// buildExplainPrompt — multiple code context entries
// ---------------------------------------------------------------------------

func TestBuildExplainPrompt_MultipleCodeContextEntries(t *testing.T) {
	req := domain.ExplainRequest{
		QueryType: "search",
		UserQuery: "multi",
		CodeContext: []domain.CodeExcerpt{
			{Qualified: "pkg.Alpha", FilePath: "alpha.go"},
			{Qualified: "pkg.Beta", FilePath: "beta.go"},
			{Qualified: "pkg.Gamma", FilePath: "gamma.go"},
		},
	}
	out := buildExplainPrompt(req)

	mustContain(t, out, "pkg.Alpha")
	mustContain(t, out, "pkg.Beta")
	mustContain(t, out, "pkg.Gamma")
	mustContain(t, out, "alpha.go")
	mustContain(t, out, "beta.go")
	mustContain(t, out, "gamma.go")
}

// ---------------------------------------------------------------------------
// buildExplainPrompt — always ends with ## Answer
// ---------------------------------------------------------------------------

func TestBuildExplainPrompt_AlwaysEndsWithAnswerSection(t *testing.T) {
	cases := []string{"search", "trace", "blast", ""}
	for _, qt := range cases {
		req := domain.ExplainRequest{QueryType: qt, UserQuery: "q"}
		out := buildExplainPrompt(req)
		if !strings.HasSuffix(strings.TrimRight(out, "\n"), "## Answer") {
			t.Errorf("prompt for QueryType=%q does not end with '## Answer'", qt)
		}
	}
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\n--- output ---\n%s", needle, haystack)
	}
}
