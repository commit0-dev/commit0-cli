package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// BlastRequest represents a blast radius analysis request.
type BlastRequest struct {
	Symbol     string
	RepoSlug   string
	MaxDepth   int
	NoExplain  bool
	EdgeLabels []string // which edge types to follow. Empty = ["calls"].
}

// BlastService analyzes code change impact.
type BlastService struct {
	graph     domain.OpenCodeGraph
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewBlastService creates a new blast service.
func NewBlastService(
	graph domain.OpenCodeGraph,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *BlastService {
	return &BlastService{
		graph:     graph,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "blast"),
	}
}

// Blast analyzes the impact of changing a code element.
func (bs *BlastService) Blast(ctx context.Context, req BlastRequest) (*types.BlastResult, error) {
	startTime := time.Now()

	// Validate
	if req.Symbol == "" {
		return nil, domain.Validation("symbol cannot be empty")
	}
	if req.RepoSlug == "" {
		return nil, domain.Validation("repo slug cannot be empty")
	}

	if req.MaxDepth <= 0 {
		req.MaxDepth = 3 // default 3 (was 10 — depth 10 causes 8min+ graph traversal)
	}
	if req.MaxDepth > 5 {
		req.MaxDepth = 5 // cap at 5 — deeper traversals are exponentially slower
	}

	// Resolve symbol using shared resolver (returns AmbiguousSymbolError on multiple matches).
	target, err := ResolveSymbol(ctx, bs.graph, nil, req.RepoSlug, req.Symbol)
	if err != nil {
		return nil, err
	}

	// Get blast radius — label-parameterized reverse traversal (OpenCodeGraph §3)
	graphStart := time.Now()
	labels := req.EdgeLabels
	if len(labels) == 0 {
		labels = []string{"calls"} // backward compat default
	}
	hops, err := bs.graph.TraverseGraph(ctx, target.ID, labels, "reverse", req.MaxDepth)
	// Convert TraceHop to AffectedNode for backward compat
	affected := make([]types.AffectedNode, 0, len(hops))
	for _, h := range hops {
		module := h.Node.Qualified
		if idx := strings.IndexByte(h.Node.Qualified, '.'); idx > 0 {
			module = h.Node.Qualified[:idx]
		}
		hopCount := h.Depth
		if hopCount <= 0 {
			hopCount = 1
		}
		affected = append(affected, types.AffectedNode{
			Node:     h.Node,
			HopCount: hopCount,
			Module:   module,
			Path:     h.Node.FilePath,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("blast radius: %w", err)
	}

	// Deduplicate and sort
	affected = deduplicateAffected(affected)
	sortAffectedByHopCount(affected)

	// Build explanation (non-fatal). Structured first, fallback to streaming.
	explanation := ""
	var structuredSummary *types.BlastExplanation
	if bs.explainer != nil && !req.NoExplain {
		excerpts := []domain.CodeExcerpt{
			{
				Qualified: target.Qualified,
				FilePath:  target.FilePath,
				Snippet:   target.Body,
			},
		}
		for _, aff := range affected[:minInt(5, len(affected))] {
			excerpts = append(excerpts, domain.CodeExcerpt{
				Qualified: aff.Node.Qualified,
				FilePath:  aff.Node.FilePath,
				Snippet:   aff.Node.Body,
			})
		}

		explainReq := domain.ExplainRequest{
			QueryType:      "blast",
			UserQuery:      fmt.Sprintf("impact of changing %s", req.Symbol),
			CodeContext:    excerpts,
			ResponseSchema: domain.SchemaForQueryType("blast"),
		}

		raw, err := bs.explainer.ExplainStructured(ctx, explainReq)
		if err == nil {
			var be types.BlastExplanation
			if json.Unmarshal(raw, &be) == nil {
				structuredSummary = &be
				explanation = be.Overview
			}
		}

		if structuredSummary == nil {
			chunks, err := bs.explainer.Explain(ctx, explainReq)
			if err != nil {
				bs.log.Warn("explain failed", "err", err)
			} else if chunks != nil {
				var buf []byte
				for chunk := range chunks {
					if chunk.Error != nil {
						bs.log.Warn("explain chunk error", "err", chunk.Error)
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

	return &types.BlastResult{
		Target:            *target,
		Affected:          affected,
		Summary:           explanation,
		StructuredSummary: structuredSummary,
		Timing: types.TimingInfo{
			GraphMS: time.Since(graphStart).Milliseconds(),
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// deduplicateAffected removes duplicate nodes, keeping lowest hop count.
func deduplicateAffected(affected []types.AffectedNode) []types.AffectedNode {
	seen := make(map[string]types.AffectedNode)
	for _, a := range affected {
		if existing, ok := seen[a.Node.ID]; ok {
			if a.HopCount < existing.HopCount {
				seen[a.Node.ID] = a
			}
		} else {
			seen[a.Node.ID] = a
		}
	}

	result := make([]types.AffectedNode, 0, len(seen))
	for _, a := range seen {
		result = append(result, a)
	}
	return result
}

// sortAffectedByHopCount sorts affected nodes by hop count ascending.
func sortAffectedByHopCount(affected []types.AffectedNode) {
	sort.Slice(affected, func(i, j int) bool {
		if affected[i].HopCount != affected[j].HopCount {
			return affected[i].HopCount < affected[j].HopCount
		}
		return affected[i].Node.Qualified < affected[j].Node.Qualified
	})
}

// minInt returns the smaller of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
