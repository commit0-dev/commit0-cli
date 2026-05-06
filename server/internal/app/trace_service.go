package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// TraceRequest represents a code trace request.
type TraceRequest struct {
	Symbol     string
	RepoSlug   string
	Direction  string
	Depth      int
	NoExplain  bool
	EdgeLabels []string // which edge types to follow. Empty = ["calls"].
}

// TraceService traces code flow paths.
type TraceService struct {
	graph     domain.OpenCodeGraph
	embedder  domain.Embedder
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewTraceService creates a new trace service.
func NewTraceService(
	graph domain.OpenCodeGraph,
	embedder domain.Embedder,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *TraceService {
	return &TraceService{
		graph:     graph,
		embedder:  embedder,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "trace"),
	}
}

// Trace traces code flow starting from a symbol.
func (ts *TraceService) Trace(ctx context.Context, req TraceRequest) (*types.TraceResult, error) {
	startTime := time.Now()

	// Validate
	if req.Symbol == "" {
		return nil, domain.Validation("symbol cannot be empty")
	}
	if req.RepoSlug == "" {
		return nil, domain.Validation("repo slug cannot be empty")
	}

	if req.Direction != "forward" && req.Direction != "reverse" {
		return nil, domain.Validation(fmt.Sprintf("invalid direction: %s (must be 'forward' or 'reverse')", req.Direction))
	}

	if req.Depth <= 0 {
		req.Depth = 5
	}

	// Resolve symbol
	node, err := ts.resolveSymbol(ctx, req.RepoSlug, req.Symbol)
	if err != nil {
		return nil, err
	}

	// Trace — label-parameterized traversal (OpenCodeGraph §3)
	labels := req.EdgeLabels
	if len(labels) == 0 {
		labels = []string{"calls"} // backward compat default
	}
	hops, err := ts.graph.TraverseGraph(ctx, node.ID, labels, req.Direction, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("trace %s: %w", req.Direction, err)
	}

	// Dedup hops by qualified name — graph traversal may return multiple
	// edge paths to the same node, causing duplicates.
	hops = dedupHops(hops)

	// Build explanation (non-fatal). Structured output first, fallback to streaming.
	graphStart := time.Now()
	explanation := ""
	var structuredExplan *types.TraceExplanation
	if ts.explainer != nil && !req.NoExplain {
		var excerpts []domain.CodeExcerpt
		ts.collectHopExcerpts(hops, &excerpts)

		explainReq := domain.ExplainRequest{
			QueryType:      "trace",
			UserQuery:      fmt.Sprintf("trace %s from %s", req.Direction, req.Symbol),
			CodeContext:    excerpts,
			ResponseSchema: domain.SchemaForQueryType("trace"),
		}

		raw, err := ts.explainer.ExplainStructured(ctx, explainReq)
		if err == nil {
			var te types.TraceExplanation
			if json.Unmarshal(raw, &te) == nil {
				structuredExplan = &te
				explanation = te.Overview
			}
		}

		if structuredExplan == nil {
			chunks, err := ts.explainer.Explain(ctx, explainReq)
			if err != nil {
				ts.log.Warn("explain failed", "err", err)
			} else if chunks != nil {
				var buf []byte
				for chunk := range chunks {
					if chunk.Error != nil {
						ts.log.Warn("explain chunk error", "err", chunk.Error)
						break
					}
					buf = append(buf, []byte(chunk.Text)...)
					if chunk.Done {
						break
					}
				}
				explanation = string(buf)
			}
		}
	}

	return &types.TraceResult{
		Root:                  *node,
		Tree:                  hops,
		Direction:             req.Direction,
		Explanation:           explanation,
		StructuredExplanation: structuredExplan,
		Timing: types.TimingInfo{
			GraphMS: time.Since(graphStart).Milliseconds(),
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// resolveSymbol resolves a symbol to a code node using the shared resolver.
func (ts *TraceService) resolveSymbol(ctx context.Context, repo, symbol string) (*types.CodeNode, error) {
	return ResolveSymbol(ctx, ts.graph, ts.embedder, repo, symbol)
}

// dedupHops removes duplicate nodes from trace results, keeping the first occurrence.
func dedupHops(hops []types.TraceHop) []types.TraceHop {
	seen := make(map[string]bool)
	result := make([]types.TraceHop, 0, len(hops))
	for _, h := range hops {
		key := h.Node.Qualified
		if key == "" {
			key = h.Node.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(h.Children) > 0 {
			h.Children = dedupHops(h.Children)
		}
		result = append(result, h)
	}
	return result
}

// collectHopExcerpts recursively collects code excerpts from trace hops.
func (ts *TraceService) collectHopExcerpts(hops []types.TraceHop, excerpts *[]domain.CodeExcerpt) {
	for _, hop := range hops {
		*excerpts = append(*excerpts, domain.CodeExcerpt{
			Qualified: hop.Node.Qualified,
			FilePath:  hop.Node.FilePath,
			Snippet:   hop.Node.Body,
		})
		if len(hop.Children) > 0 {
			ts.collectHopExcerpts(hop.Children, excerpts)
		}
	}
}
