package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
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

// needsSummary returns true if a node should be summarized.
func (s *Summarizer) needsSummary(n *types.CodeNode) bool {
	// Skip if already has a summary
	if n.Summary != "" {
		return false
	}
	// Only summarize functions and classes
	if n.Kind != types.NodeFunction && n.Kind != types.NodeClass {
		return false
	}
	// Skip trivial functions (< 3 lines)
	lineCount := n.EndLine - n.StartLine
	if lineCount < 3 {
		return false
	}
	// Skip empty bodies
	if strings.TrimSpace(n.Body) == "" {
		return false
	}
	return true
}

// summarizeBatch summarizes a batch of nodes with a single LLM call.
func (s *Summarizer) summarizeBatch(ctx context.Context, nodes []*types.CodeNode) {
	prompt := s.buildBatchPrompt(nodes)

	req := domain.ExplainRequest{
		QueryType: "summarize",
		UserQuery: prompt,
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

	// Parse batch result
	var results []SummaryResult
	if err := json.Unmarshal(raw, &results); err != nil {
		s.log.Warn("batch JSON parse failed, trying individual", "err", err)
		for _, n := range nodes {
			s.summarizeSingle(ctx, n)
		}
		return
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
		QueryType: "summarize-single",
		UserQuery: prompt,
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

// buildBatchPrompt creates a prompt for summarizing multiple nodes at once.
func (s *Summarizer) buildBatchPrompt(nodes []*types.CodeNode) string {
	var sb strings.Builder
	sb.WriteString("For each function/class below, write a one-paragraph summary ")
	sb.WriteString("of what it does, what problem it solves, and how it fits into the codebase. ")
	sb.WriteString("Also list 3-5 semantic concept tags (lowercase, hyphenated).\n\n")
	sb.WriteString("Return a JSON array with one object per function, in order.\n\n")

	for i, n := range nodes {
		fmt.Fprintf(&sb, "--- Function %d ---\n", i+1)
		fmt.Fprintf(&sb, "Name: %s\n", n.Qualified)
		fmt.Fprintf(&sb, "File: %s:%d-%d\n", n.FilePath, n.StartLine, n.EndLine)
		if n.Signature != "" {
			fmt.Fprintf(&sb, "Signature: %s\n", n.Signature)
		}
		if n.Docstring != "" {
			fmt.Fprintf(&sb, "Docstring: %s\n", n.Docstring)
		}
		// Include body but truncate to 2000 chars to stay within token limits
		body := n.Body
		if len(body) > 2000 {
			body = body[:2000] + "\n... (truncated)"
		}
		fmt.Fprintf(&sb, "Code:\n%s\n\n", body)
	}

	return sb.String()
}

// buildSinglePrompt creates a prompt for summarizing a single node.
func (s *Summarizer) buildSinglePrompt(n *types.CodeNode) string {
	var sb strings.Builder
	sb.WriteString("Write a one-paragraph summary of what this code does, ")
	sb.WriteString("what problem it solves, and what architectural concepts it implements. ")
	sb.WriteString("Also list 3-5 semantic concept tags (lowercase, hyphenated).\n\n")

	fmt.Fprintf(&sb, "Name: %s\n", n.Qualified)
	fmt.Fprintf(&sb, "File: %s:%d-%d\n", n.FilePath, n.StartLine, n.EndLine)
	if n.Signature != "" {
		fmt.Fprintf(&sb, "Signature: %s\n", n.Signature)
	}
	if n.Docstring != "" {
		fmt.Fprintf(&sb, "Docstring: %s\n", n.Docstring)
	}
	body := n.Body
	if len(body) > 3000 {
		body = body[:3000] + "\n... (truncated)"
	}
	fmt.Fprintf(&sb, "Code:\n%s\n", body)

	return sb.String()
}
