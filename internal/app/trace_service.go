package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// TraceRequest represents a code trace request.
type TraceRequest struct {
	Symbol    string
	RepoSlug  string
	Direction string
	Depth     int
}

// TraceService traces code flow paths.
type TraceService struct {
	store     domain.GraphStore
	embedder  domain.Embedder
	vectorIdx domain.VectorIndex
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewTraceService creates a new trace service.
func NewTraceService(
	store domain.GraphStore,
	embedder domain.Embedder,
	vectorIdx domain.VectorIndex,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *TraceService {
	return &TraceService{
		store:     store,
		embedder:  embedder,
		vectorIdx: vectorIdx,
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

	// Trace
	var hops []types.TraceHop
	if req.Direction == "forward" {
		hops, err = ts.store.TraceForward(ctx, node.ID, req.Depth)
	} else {
		hops, err = ts.store.TraceReverse(ctx, node.ID, req.Depth)
	}
	if err != nil {
		return nil, fmt.Errorf("trace %s: %w", req.Direction, err)
	}

	// Build explanation (non-fatal). Structured output first, fallback to streaming.
	graphStart := time.Now()
	explanation := ""
	var structuredExplan *types.TraceExplanation
	if ts.explainer != nil {
		var excerpts []domain.CodeExcerpt
		ts.collectHopExcerpts(hops, &excerpts)

		explainReq := domain.ExplainRequest{
			QueryType:   "trace",
			UserQuery:   fmt.Sprintf("trace %s from %s", req.Direction, req.Symbol),
			CodeContext: excerpts,
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

// resolveSymbol resolves a symbol to a code node.
func (ts *TraceService) resolveSymbol(ctx context.Context, repo, symbol string) (*types.CodeNode, error) {
	// Try direct lookup
	node, err := ts.store.GetNodeByQualified(ctx, repo, symbol)
	if err == nil && node != nil {
		return node, nil
	}

	// Fallback to vector search
	if ts.embedder != nil && ts.vectorIdx != nil {
		query, err := ts.embedder.EmbedQuery(ctx, symbol)
		if err != nil {
			return nil, fmt.Errorf("embed symbol: %w", err)
		}

		results, err := ts.vectorIdx.Search(ctx, query, domain.VectorSearchOpts{
			RepoSlug: repo,
			TopK:     1,
			MinScore: 0.8,
		})
		if err != nil {
			return nil, fmt.Errorf("vector search: %w", err)
		}

		if len(results) > 0 {
			return &results[0].Node, nil
		}
	}

	return nil, domain.NotFound(fmt.Sprintf("symbol %s not found", symbol))
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
