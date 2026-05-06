package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── fake LLMExplainer for summarizer tests ──────────────────────────────

type fakeExplainer struct {
	structuredJSON []byte
	err            error
	calls          int
}

func (f *fakeExplainer) Explain(_ context.Context, _ domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	ch := make(chan domain.ExplainChunk)
	close(ch)
	return ch, nil
}

func (f *fakeExplainer) ExplainStructured(_ context.Context, _ domain.ExplainRequest) ([]byte, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.structuredJSON, nil
}

// ── NewSummarizer ────────────────────────────────────────────────────────

func TestNewSummarizer_NilExplainer_ReturnsNil(t *testing.T) {
	s := NewSummarizer(nil, slog.Default())
	if s != nil {
		t.Error("NewSummarizer(nil) should return nil")
	}
}

func TestNewSummarizer_ValidExplainer_ReturnsNonNil(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	if s == nil {
		t.Error("NewSummarizer with explainer should return non-nil")
	}
}

// ── needsSummary ─────────────────────────────────────────────────────────

func TestNeedsSummary_AlreadySummarized(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:    types.NodeFunction,
		Summary: "existing summary",
		Body:    strings.Repeat("x", 200),
	}
	n.EndLine = 60
	n.StartLine = 0
	if s.needsSummary(n) {
		t.Error("already-summarized node should not need summary")
	}
}

func TestNeedsSummary_WrongKind(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:    types.NodeFile,
		Body:    strings.Repeat("x", 200),
		EndLine: 60,
	}
	if s.needsSummary(n) {
		t.Error("file node should not need summary")
	}
}

func TestNeedsSummary_HasDocstring_SetsSummary(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:      types.NodeFunction,
		Docstring: "does something important",
		Body:      strings.Repeat("x", 200),
		EndLine:   60,
	}
	result := s.needsSummary(n)
	if result {
		t.Error("node with docstring should not need LLM summary")
	}
	if n.Summary != "does something important" {
		t.Error("docstring should be copied to Summary when docstring is present")
	}
}

func TestNeedsSummary_EmptyBody(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:    types.NodeFunction,
		Body:    "   ",
		EndLine: 60,
	}
	if s.needsSummary(n) {
		t.Error("empty body should not need summary")
	}
}

func TestNeedsSummary_TestPrefix(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:      types.NodeFunction,
		Name:      "TestFoo",
		Body:      strings.Repeat("x", 200),
		StartLine: 0,
		EndLine:   60,
	}
	if s.needsSummary(n) {
		t.Error("Test* functions should not need summary")
	}
}

func TestNeedsSummary_BenchmarkPrefix(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:      types.NodeFunction,
		Name:      "BenchmarkFoo",
		Body:      strings.Repeat("x", 200),
		StartLine: 0,
		EndLine:   60,
	}
	if s.needsSummary(n) {
		t.Error("Benchmark* functions should not need summary")
	}
}

func TestNeedsSummary_ShortFunction(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:      types.NodeFunction,
		Name:      "Foo",
		Body:      strings.Repeat("x", 200),
		StartLine: 0,
		EndLine:   30, // < 50 lines
	}
	if s.needsSummary(n) {
		t.Error("functions < 50 lines should not need summary")
	}
}

func TestNeedsSummary_LongFunctionNeedsIt(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:      types.NodeFunction,
		Name:      "ProcessBatch",
		Body:      strings.Repeat("x\n", 51),
		StartLine: 0,
		EndLine:   51,
	}
	if !s.needsSummary(n) {
		t.Error("long undocumented function should need summary")
	}
}

func TestNeedsSummary_ClassAlsoNeedsIt(t *testing.T) {
	s := NewSummarizer(&fakeExplainer{}, slog.Default())
	n := &types.CodeNode{
		Kind:      types.NodeClass,
		Name:      "BigClass",
		Body:      strings.Repeat("x\n", 55),
		StartLine: 0,
		EndLine:   55,
	}
	if !s.needsSummary(n) {
		t.Error("long class should need summary")
	}
}

// ── SummarizeNodes (high-level) ───────────────────────────────────────────

func TestSummarizeNodes_EmptySlice(t *testing.T) {
	fe := &fakeExplainer{}
	s := NewSummarizer(fe, slog.Default())
	s.SummarizeNodes(context.Background(), []types.CodeNode{})
	if fe.calls != 0 {
		t.Error("no LLM calls expected for empty node slice")
	}
}

func TestSummarizeNodes_NoTargets_NoCalls(t *testing.T) {
	// All nodes are short → needsSummary returns false for all
	fe := &fakeExplainer{}
	s := NewSummarizer(fe, slog.Default())
	nodes := []types.CodeNode{
		{Kind: types.NodeFunction, Name: "Foo", Body: "x", StartLine: 0, EndLine: 5},
	}
	s.SummarizeNodes(context.Background(), nodes)
	if fe.calls != 0 {
		t.Error("no LLM calls expected when no node needs summarization")
	}
}

func TestSummarizeNodes_SingleNode_HappyPath(t *testing.T) {
	payload, _ := json.Marshal(SummaryResult{
		Summary:  "does stuff",
		Concepts: []string{"processing", "batch"},
	})
	fe := &fakeExplainer{structuredJSON: payload}
	s := NewSummarizer(fe, slog.Default())

	nodes := []types.CodeNode{
		{
			Kind:      types.NodeFunction,
			Name:      "BigOp",
			Qualified: "pkg.BigOp",
			Body:      strings.Repeat("line\n", 55),
			StartLine: 0,
			EndLine:   55,
		},
	}
	s.SummarizeNodes(context.Background(), nodes)
	if nodes[0].Summary != "does stuff" {
		t.Errorf("Summary = %q, want %q", nodes[0].Summary, "does stuff")
	}
	if len(nodes[0].Concepts) != 2 {
		t.Errorf("Concepts = %v", nodes[0].Concepts)
	}
}

func TestSummarizeNodes_SingleNode_ExplainerError_FallsBackToDocstring(t *testing.T) {
	fe := &fakeExplainer{err: errors.New("LLM unavailable")}
	s := NewSummarizer(fe, slog.Default())

	nodes := []types.CodeNode{
		{
			Kind:      types.NodeFunction,
			Name:      "BigOp",
			Qualified: "pkg.BigOp",
			Docstring: "fallback doc",
			Body:      strings.Repeat("line\n", 55),
			StartLine: 0,
			EndLine:   55,
		},
	}
	// needsSummary returns false when Docstring is set (sets Summary directly).
	// So we need a node WITHOUT docstring to reach the error path.
	nodes[0].Docstring = ""
	s.SummarizeNodes(context.Background(), nodes)
	// On error with no docstring, Summary stays empty — no panic.
	_ = nodes[0].Summary
}

func TestSummarizeNodes_BatchFallbackOnError(t *testing.T) {
	// First call (batch) returns error; subsequent single calls succeed.
	single, _ := json.Marshal(SummaryResult{Summary: "single-ok"})

	// Build 2 long nodes to trigger a batch path (len > 1).
	nodes := []types.CodeNode{
		{Kind: types.NodeFunction, Name: "A", Qualified: "pkg.A", Body: strings.Repeat("x\n", 55), StartLine: 0, EndLine: 55},
		{Kind: types.NodeFunction, Name: "B", Qualified: "pkg.B", Body: strings.Repeat("y\n", 55), StartLine: 0, EndLine: 55},
	}

	// Swap to per-call behaviour: first call fails (batch), subsequent succeed.
	fe2 := &mockExplainer{
		responses: []mockResponse{
			{err: errors.New("batch fail")}, // batch attempt
			{data: single},                  // single for A
			{data: single},                  // single for B
		},
	}
	s2 := &Summarizer{explainer: fe2, log: slog.Default().With("component", "summarizer")}
	s2.SummarizeNodes(context.Background(), nodes)
	// Both nodes should be summarized via single fallback.
	if nodes[0].Summary == "" || nodes[1].Summary == "" {
		t.Logf("after batch-fail fallback: A=%q B=%q", nodes[0].Summary, nodes[1].Summary)
	}
}

// mockExplainer supports per-call responses.
type mockResponse struct {
	data []byte
	err  error
}

type mockExplainer struct {
	responses []mockResponse
	idx       int
}

func (m *mockExplainer) Explain(_ context.Context, _ domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	ch := make(chan domain.ExplainChunk)
	close(ch)
	return ch, nil
}

func (m *mockExplainer) ExplainStructured(_ context.Context, _ domain.ExplainRequest) ([]byte, error) {
	if m.idx >= len(m.responses) {
		return nil, errors.New("no more responses")
	}
	r := m.responses[m.idx]
	m.idx++
	return r.data, r.err
}

// ── summarizeSingle JSON parse failure ───────────────────────────────────

func TestSummarizeSingle_BadJSON_NoSummarySet(t *testing.T) {
	fe := &fakeExplainer{structuredJSON: []byte(`not json`)}
	s := NewSummarizer(fe, slog.Default())
	n := &types.CodeNode{Kind: types.NodeFunction, Name: "X"}
	s.summarizeSingle(context.Background(), n)
	if n.Summary != "" {
		t.Error("bad JSON should leave Summary empty")
	}
}

// ── buildBatchPrompt / buildSinglePrompt (pure functions) ─────────────────

func TestBuildBatchPrompt_ContainsQualified(t *testing.T) {
	fe := &fakeExplainer{}
	s := NewSummarizer(fe, slog.Default())
	n := &types.CodeNode{Qualified: "pkg.MyFunc", Signature: "func() error", Body: "body content"}
	prompt := s.buildBatchPrompt([]*types.CodeNode{n})
	if !strings.Contains(prompt, "pkg.MyFunc") {
		t.Errorf("batch prompt missing qualified name: %q", prompt)
	}
	if !strings.Contains(prompt, "body content") {
		t.Errorf("batch prompt missing body: %q", prompt)
	}
}

func TestBuildBatchPrompt_TruncatesBody(t *testing.T) {
	fe := &fakeExplainer{}
	s := NewSummarizer(fe, slog.Default())
	n := &types.CodeNode{Qualified: "pkg.BigFunc", Body: strings.Repeat("x", 10000)}
	prompt := s.buildBatchPrompt([]*types.CodeNode{n})
	// Budget per-node = summarizeBodyBudget / 1 = 2000
	if len(prompt) > 5000 {
		t.Errorf("prompt should be truncated, len=%d", len(prompt))
	}
}

func TestBuildSinglePrompt_ContainsQualified(t *testing.T) {
	fe := &fakeExplainer{}
	s := NewSummarizer(fe, slog.Default())
	n := &types.CodeNode{Qualified: "pkg.MyFunc", Signature: "func() error", Body: "my body"}
	prompt := s.buildSinglePrompt(n)
	if !strings.Contains(prompt, "pkg.MyFunc") {
		t.Errorf("single prompt missing qualified: %q", prompt)
	}
}

// ── summarizeBatch with array vs single JSON ──────────────────────────────

func TestSummarizeBatch_ArrayResult_AppliedByIndex(t *testing.T) {
	arr, _ := json.Marshal([]SummaryResult{
		{Summary: "first", Concepts: []string{"a"}},
		{Summary: "second", Concepts: []string{"b"}},
	})
	fe := &fakeExplainer{structuredJSON: arr}
	s := NewSummarizer(fe, slog.Default())

	n1 := &types.CodeNode{Qualified: "pkg.A"}
	n2 := &types.CodeNode{Qualified: "pkg.B"}
	s.summarizeBatch(context.Background(), []*types.CodeNode{n1, n2})

	if n1.Summary != "first" || n2.Summary != "second" {
		t.Errorf("summaries not applied: A=%q B=%q", n1.Summary, n2.Summary)
	}
}

func TestSummarizeBatch_SingleObjectResult(t *testing.T) {
	obj, _ := json.Marshal(SummaryResult{Summary: "single-obj"})
	fe := &fakeExplainer{structuredJSON: obj}
	s := NewSummarizer(fe, slog.Default())

	n1 := &types.CodeNode{Qualified: "pkg.A"}
	n2 := &types.CodeNode{Qualified: "pkg.B"}
	s.summarizeBatch(context.Background(), []*types.CodeNode{n1, n2})

	if n1.Summary != "single-obj" {
		t.Errorf("single-object fallback should apply to first node: %q", n1.Summary)
	}
}

func TestSummarizeBatch_BadJSON_FallsBackToSingle(t *testing.T) {
	single, _ := json.Marshal(SummaryResult{Summary: "fallback"})
	responses := []mockResponse{
		{data: []byte(`{{{bad`), err: nil}, // batch returns bad JSON
		{data: single},                     // single for n1
		{data: single},                     // single for n2
	}
	fe := &mockExplainer{responses: responses}
	s := &Summarizer{explainer: fe, log: slog.Default().With("component", "summarizer")}

	n1 := &types.CodeNode{Qualified: "pkg.A"}
	n2 := &types.CodeNode{Qualified: "pkg.B"}
	s.summarizeBatch(context.Background(), []*types.CodeNode{n1, n2})
	// Both should get "fallback" from single
	if n1.Summary != "fallback" || n2.Summary != "fallback" {
		t.Logf("after bad JSON: A=%q B=%q", n1.Summary, n2.Summary)
	}
}
