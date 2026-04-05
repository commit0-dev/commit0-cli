package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// BlastRequest represents a blast radius analysis request
type BlastRequest struct {
	Symbol   string
	RepoSlug string
	MaxDepth int
}

// BlastService analyzes code change impact
type BlastService struct {
	store     domain.GraphStore
	explainer domain.LLMExplainer
	cfg       *config.Config
	log       *slog.Logger
}

// NewBlastService creates a new blast service
func NewBlastService(
	store domain.GraphStore,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *BlastService {
	return &BlastService{
		store:     store,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "blast"),
	}
}

// Blast analyzes the impact of changing a code element
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
		req.MaxDepth = 10
	}

	// Resolve symbol
	target, err := bs.store.GetNodeByQualified(ctx, req.RepoSlug, req.Symbol)
	if err != nil {
		return nil, domain.NotFound(fmt.Sprintf("symbol %s not found", req.Symbol))
	}

	// Get blast radius
	graphStart := time.Now()
	affected, err := bs.store.BlastRadius(ctx, target.ID, req.MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("blast radius: %w", err)
	}

	// Deduplicate and sort
	affected = deduplicateAffected(affected)
	sortAffectedByHopCount(affected)

	// Build explanation (non-fatal)
	explanation := ""
	if bs.explainer != nil {
		// Build code excerpts
		excerpts := []domain.CodeExcerpt{
			{
				Qualified: target.Qualified,
				FilePath:  target.FilePath,
				Snippet:   target.Body,
			},
		}
		for _, aff := range affected[:minInt(5, len(affected))] { // Take top 5 affected
			excerpts = append(excerpts, domain.CodeExcerpt{
				Qualified: aff.Node.Qualified,
				FilePath:  aff.Node.FilePath,
				Snippet:   aff.Node.Body,
			})
		}

		chunks, err := bs.explainer.Explain(ctx, domain.ExplainRequest{
			QueryType:   "blast",
			UserQuery:   fmt.Sprintf("impact of changing %s", req.Symbol),
			CodeContext: excerpts,
		})
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

	return &types.BlastResult{
		Target:    *target,
		Affected:  affected,
		Summary:   explanation,
		Timing: types.TimingInfo{
			GraphMS: time.Since(graphStart).Milliseconds(),
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// deduplicateAffected removes duplicate nodes, keeping lowest hop count
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

// sortAffectedByHopCount sorts affected nodes by hop count ascending
func sortAffectedByHopCount(affected []types.AffectedNode) {
	sort.Slice(affected, func(i, j int) bool {
		if affected[i].HopCount != affected[j].HopCount {
			return affected[i].HopCount < affected[j].HopCount
		}
		return affected[i].Node.Qualified < affected[j].Node.Qualified
	})
}

// minInt returns the smaller of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
