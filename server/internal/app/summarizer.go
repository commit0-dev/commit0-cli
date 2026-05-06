package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// SummaryResult is the structured output from the LLM summarizer.
type SummaryResult struct {
	Summary  string   `json:"summary"`
	Concepts []string `json:"concepts"`
}

// Summarizer generates semantic summaries for code nodes using the LLM.
// It sets node.Summary and node.Concepts for each function/class that has
// a non-trivial body (> 3 lines).
type Summarizer struct {
	explainer domain.LLMExplainer
	log       *slog.Logger
}

// NewSummarizer creates a summarizer. Returns nil if explainer is nil
// (summarization is optional — the pipeline continues without it).
func NewSummarizer(explainer domain.LLMExplainer, log *slog.Logger) *Summarizer {
	if explainer == nil {
		return nil
	}
	return &Summarizer{
		explainer: explainer,
		log:       log.With("component", "summarizer"),
	}
}

// SummarizeNodes generates summaries for all non-trivial nodes in the slice.
// Nodes with an existing Summary whose ContentHash hasn't changed are skipped.
// Modifies nodes in place.
func (s *Summarizer) SummarizeNodes(ctx context.Context, nodes []types.CodeNode) {
	// Filter to nodes that need summarization
	var targets []*types.CodeNode
	for i := range nodes {
		n := &nodes[i]
		if s.needsSummary(n) {
			targets = append(targets, n)
		}
	}

	if len(targets) == 0 {
		return
	}

	s.log.Info("summarizing nodes", "count", len(targets))

	// Process in batches with bounded concurrency
	const batchSize = 10
	const maxWorkers = 4

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxWorkers)

	for i := 0; i < len(targets); i += batchSize {
		end := min(i+batchSize, len(targets))
		batch := targets[i:end]

		g.Go(func() error {
			s.summarizeBatch(gCtx, batch)
			return nil // non-fatal — log and continue
		})
	}

	_ = g.Wait() // errors are logged per-batch, not propagated
	s.log.Info("summarization complete", "summarized", len(targets))
}

// needsSummary returns true if a node needs LLM summarization.
// Most nodes don't: docstrings, simple functions, and tests are handled
// without an LLM call. Only complex undocumented code needs summarization.
func (s *Summarizer) needsSummary(n *types.CodeNode) bool {
	// Already summarized (cached from previous run).
	if n.Summary != "" {
		return false
	}
	// Only functions and classes.
	if n.Kind != types.NodeFunction && n.Kind != types.NodeClass {
		return false
	}
	// Has a docstring — use it directly (no LLM needed).
	if n.Docstring != "" {
		n.Summary = n.Docstring
		return false
	}
	// Empty body.
	if strings.TrimSpace(n.Body) == "" {
		return false
	}
	// Test/benchmark functions — not useful for search quality.
	if strings.HasPrefix(n.Name, "Test") || strings.HasPrefix(n.Name, "Benchmark") {
		return false
	}
	// Under 50 LOC — logic is simple enough that signature + name conveys intent.
	// Only complex functions (50+ LOC) benefit from LLM summarization.
	lineCount := n.EndLine - n.StartLine
	if lineCount < 50 {
		return false
	}
	return true
}

// summarizeBatch summarizes a batch of nodes with a single LLM call.
func (s *Summarizer) summarizeBatch(ctx context.Context, nodes []*types.CodeNode) {
	// For single-node batches, use the single prompt directly — avoids
	// array/object ambiguity from smaller models.
	if len(nodes) == 1 {
		s.summarizeSingle(ctx, nodes[0])
		return
	}

	prompt := s.buildBatchPrompt(nodes)

	req := domain.ExplainRequest{
		QueryType:      "summarize",
		UserQuery:      prompt,
		ResponseSchema: domain.SchemaForQueryType("summarize"),
	}

	raw, err := s.explainer.ExplainStructured(ctx, req)
	if err != nil {
		s.log.Warn("batch summarization failed, trying individual", "err", err, "batch_size", len(nodes))
		// Fallback: try each node individually
		for _, n := range nodes {
			s.summarizeSingle(ctx, n)
		}
		return
	}

	// Parse batch result — try array first, then single object (small models
	// often return a bare object instead of a 1-element array).
	var results []SummaryResult
	if err := json.Unmarshal(raw, &results); err != nil {
		var single SummaryResult
		if err2 := json.Unmarshal(raw, &single); err2 == nil && single.Summary != "" {
			results = []SummaryResult{single}
		} else {
			s.log.Warn("batch JSON parse failed, trying individual", "err", err)
			for _, n := range nodes {
				s.summarizeSingle(ctx, n)
			}
			return
		}
	}

	// Apply results to nodes (match by index)
	for i, n := range nodes {
		if i < len(results) {
			n.Summary = results[i].Summary
			n.Concepts = results[i].Concepts
		}
	}
}

// summarizeSingle summarizes a single node as a fallback.
func (s *Summarizer) summarizeSingle(ctx context.Context, n *types.CodeNode) {
	prompt := s.buildSinglePrompt(n)

	req := domain.ExplainRequest{
		QueryType:      "summarize-single",
		UserQuery:      prompt,
		ResponseSchema: domain.SchemaForQueryType("summarize-single"),
	}

	raw, err := s.explainer.ExplainStructured(ctx, req)
	if err != nil {
		s.log.Debug("single summarization failed", "node", n.Qualified, "err", err)
		// Use docstring as fallback summary
		if n.Docstring != "" {
			n.Summary = n.Docstring
		}
		return
	}

	var result SummaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		s.log.Debug("single JSON parse failed", "node", n.Qualified, "err", err)
		return
	}

	n.Summary = result.Summary
	n.Concepts = result.Concepts
}

// summarizeBodyBudget is the max chars of code body sent to the LLM for summarization.
// Derived from typical local model context (~8K tokens). The prompt overhead
// (instructions + metadata) uses ~500 tokens, leaving ~7500 for code body.
// At ~3 chars/token: 7500 * 3 ≈ 22K chars. Per-node in batch: 22K / batchSize.
const summarizeBodyBudget = 2000

// buildBatchPrompt creates a prompt for summarizing multiple nodes.
// Designed for concise output: 2-sentence summary + 3-5 tags.
func (s *Summarizer) buildBatchPrompt(nodes []*types.CodeNode) string {
	var sb strings.Builder
	sb.WriteString("Summarize each function below. For each, write:\n")
	sb.WriteString("- summary: 2 sentences max. What it does and why.\n")
	sb.WriteString("- concepts: 3-5 tags (lowercase, hyphenated).\n\n")

	budget := summarizeBodyBudget / max(len(nodes), 1)
	for i, n := range nodes {
		fmt.Fprintf(&sb, "--- %d: %s ---\n", i+1, n.Qualified)
		if n.Signature != "" {
			fmt.Fprintf(&sb, "%s\n", n.Signature)
		}
		body := n.Body
		if len(body) > budget {
			body = body[:budget]
		}
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// buildSinglePrompt creates a prompt for summarizing one node.
func (s *Summarizer) buildSinglePrompt(n *types.CodeNode) string {
	var sb strings.Builder
	sb.WriteString("Summarize this code. Write:\n")
	sb.WriteString("- summary: 2 sentences max. What it does and why.\n")
	sb.WriteString("- concepts: 3-5 tags (lowercase, hyphenated).\n\n")

	fmt.Fprintf(&sb, "%s\n", n.Qualified)
	if n.Signature != "" {
		fmt.Fprintf(&sb, "%s\n", n.Signature)
	}
	body := n.Body
	if len(body) > summarizeBodyBudget {
		body = body[:summarizeBodyBudget]
	}
	sb.WriteString(body)
	return sb.String()
}
