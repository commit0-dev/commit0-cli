package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// QueryRequest represents a natural language query.
type QueryRequest struct {
	Question  string
	RepoSlug  string
	NodeKinds []types.NodeKind
	TopK      int
	MinScore  float64
}

// QueryService handles semantic code search.
type QueryService struct {
	embedder  domain.Embedder
	vectorIdx domain.VectorIndex
	textIdx   domain.TextIndex
	store     domain.GraphStore
	explainer domain.LLMExplainer
	flowSvc   *DataFlowService // optional: enriches LLM context with data-flow paths
	cfg       *config.Config
	log       *slog.Logger
}

// NewQueryService creates a new query service.
func NewQueryService(
	embedder domain.Embedder,
	vectorIdx domain.VectorIndex,
	textIdx domain.TextIndex,
	store domain.GraphStore,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *QueryService {
	qs := &QueryService{
		embedder:  embedder,
		vectorIdx: vectorIdx,
		textIdx:   textIdx,
		store:     store,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "query"),
	}
	if store != nil {
		qs.flowSvc = NewDataFlowService(store)
	}
	return qs
}

// Query executes a semantic code search.
func (qs *QueryService) Query(ctx context.Context, req QueryRequest) (*types.QueryResult, error) {
	startTime := time.Now()

	// Validate input
	if req.Question == "" {
		return nil, domain.Validation("question cannot be empty")
	}

	// Set defaults
	if req.TopK <= 0 {
		req.TopK = qs.cfg.Query.DefaultTopK
	}
	if req.MinScore == 0 {
		req.MinScore = qs.cfg.Query.MinScore
	}

	// Exclude MODULE nodes by default — go.mod dependencies are noise for code questions.
	// Users can explicitly request them with NodeKinds filter.
	if len(req.NodeKinds) == 0 {
		req.NodeKinds = []types.NodeKind{types.NodeFunction, types.NodeClass, types.NodeFile}
	}

	// Embed the query
	embedStart := time.Now()
	queryVec, err := qs.embedder.EmbedQuery(ctx, req.Question)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	embedMS := time.Since(embedStart).Milliseconds()

	// Parallel search: vector + FTS
	searchStart := time.Now()
	var vectorHits, ftsHits []types.ScoredNode

	g, gCtx := errgroup.WithContext(ctx)

	// Vector search
	g.Go(func() error {
		hits, err := qs.vectorIdx.Search(gCtx, queryVec, domain.VectorSearchOpts{
			RepoSlug:  req.RepoSlug,
			TopK:      req.TopK * 2, // Get more results for merging
			MinScore:  req.MinScore,
			NodeKinds: req.NodeKinds,
		})
		if err != nil {
			return fmt.Errorf("vector search: %w", err)
		}
		vectorHits = hits
		return nil
	})

	// FTS search
	g.Go(func() error {
		hits, err := qs.textIdx.Search(gCtx, req.Question, domain.TextSearchOpts{
			RepoSlug:  req.RepoSlug,
			TopK:      req.TopK * 2,
			NodeKinds: req.NodeKinds,
		})
		if err != nil {
			return fmt.Errorf("fts search: %w", err)
		}
		ftsHits = hits
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}
	searchMS := time.Since(searchStart).Milliseconds()

	// Fuse results (vector + FTS)
	fused := ReciprocalRankFusion(vectorHits, ftsHits, DefaultRRFWeights())

	// Graph-augmented expansion: for top results, pull in 1-hop neighbors
	fused = qs.expandWithGraph(ctx, fused, req.RepoSlug, req.TopK)

	// Concept-boosted reranking
	fused = qs.conceptRerank(fused, req.Question)

	if len(fused) > req.TopK {
		fused = fused[:req.TopK]
	}

	// Build code excerpts
	excerpts := make([]domain.CodeExcerpt, 0, len(fused))
	for _, scored := range fused {
		excerpts = append(excerpts, domain.CodeExcerpt{
			Qualified: scored.Node.Qualified,
			FilePath:  scored.Node.FilePath,
			Snippet:   scored.Node.Body,
			Score:     scored.FusedScore,
		})
	}

	// Build data-flow graph context for the top results (non-fatal if unavailable).
	graphContext := ""
	if qs.flowSvc != nil {
		graphContext = qs.flowSvc.BuildFlowContext(ctx, fused)
	}

	// Generate explanation (non-fatal if fails).
	// Try structured output first, fall back to streaming text.
	explainStart := time.Now()
	explanation := ""
	var structuredExplan *types.SearchExplanation
	if qs.explainer != nil {
		explainReq := domain.ExplainRequest{
			QueryType:    "search",
			UserQuery:    req.Question,
			GraphContext: graphContext,
			CodeContext:  excerpts,
		}

		raw, err := qs.explainer.ExplainStructured(ctx, explainReq)
		if err == nil {
			var se types.SearchExplanation
			if json.Unmarshal(raw, &se) == nil {
				structuredExplan = &se
				explanation = se.Overview
			}
		}

		// Fallback to streaming text if structured failed
		if structuredExplan == nil {
			chunks, err := qs.explainer.Explain(ctx, explainReq)
			if err != nil {
				qs.log.Warn("explain failed", "err", err)
			} else if chunks != nil {
				var buf []byte
				for chunk := range chunks {
					if chunk.Error != nil {
						qs.log.Warn("explain chunk error", "err", chunk.Error)
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
	explainMS := time.Since(explainStart).Milliseconds()

	return &types.QueryResult{
		Nodes:                 fused,
		Explanation:           explanation,
		StructuredExplanation: structuredExplan,
		Query:                 req.Question,
		RepoSlug:              req.RepoSlug,
		Timing: types.TimingInfo{
			EmbedMS:   embedMS,
			SearchMS:  searchMS,
			ExplainMS: explainMS,
			TotalMS:   time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// expandWithGraph pulls 1-hop callers + callees of top results into the
// candidate pool, deduplicating by node ID. This ensures that when we find
// "MemoryStore.Save", we also surface "MemoryStore.Load" and "MemoryStore.Clear".
func (qs *QueryService) expandWithGraph(ctx context.Context, fused []types.ScoredNode, repoSlug string, topK int) []types.ScoredNode {
	if qs.store == nil {
		return fused
	}

	// Take top-5 for expansion to avoid over-fetching
	expandCount := min(5, len(fused))
	seen := make(map[string]bool, len(fused))
	for _, n := range fused {
		seen[n.Node.ID] = true
	}

	var expanded []types.ScoredNode
	for i := 0; i < expandCount; i++ {
		node := &fused[i]
		if node.Node.ID == "" {
			continue
		}
		nb, err := qs.store.GetNeighborhood(ctx, node.Node.ID)
		if err != nil || nb == nil {
			continue
		}

		// Add callers/callees as candidates with a reduced score
		neighborScore := node.FusedScore * 0.6
		for _, caller := range nb.Callers {
			callerNode, err := qs.store.GetNodeByQualified(ctx, repoSlug, caller.Qualified)
			if err != nil || callerNode == nil || seen[callerNode.ID] {
				continue
			}
			seen[callerNode.ID] = true
			expanded = append(expanded, types.ScoredNode{
				Node:       *callerNode,
				FusedScore: neighborScore,
			})
		}
		for _, callee := range nb.Callees {
			calleeNode, err := qs.store.GetNodeByQualified(ctx, repoSlug, callee.Qualified)
			if err != nil || calleeNode == nil || seen[calleeNode.ID] {
				continue
			}
			seen[calleeNode.ID] = true
			expanded = append(expanded, types.ScoredNode{
				Node:       *calleeNode,
				FusedScore: neighborScore,
			})
		}
	}

	result := append(fused, expanded...)
	// Re-sort by fused score
	sort.Slice(result, func(i, j int) bool {
		return result[i].FusedScore > result[j].FusedScore
	})
	return result
}

// conceptRerank boosts nodes whose concepts match query keywords.
// Also applies a centrality boost for well-connected nodes.
func (qs *QueryService) conceptRerank(fused []types.ScoredNode, question string) []types.ScoredNode {
	if len(fused) == 0 {
		return fused
	}

	// Extract keywords from question (simple: lowercase, split, dedup)
	words := strings.Fields(strings.ToLower(question))
	queryTerms := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 2 { // skip short words like "is", "in", "a"
			queryTerms[w] = true
		}
	}

	for i := range fused {
		boost := 1.0

		// Concept match boost: 2x if any concept matches a query term
		for _, concept := range fused[i].Node.Concepts {
			parts := strings.Split(concept, "-")
			for _, part := range parts {
				if queryTerms[part] {
					boost = 2.0
					break
				}
			}
			if boost > 1 {
				break
			}
		}

		// Centrality boost: well-connected nodes are more important
		if fused[i].Centrality > 0 {
			boost *= 1 + math.Log(float64(fused[i].Centrality)+1)*0.1
		}

		fused[i].FusedScore *= boost
	}

	// Re-sort after boosting
	sort.Slice(fused, func(i, j int) bool {
		return fused[i].FusedScore > fused[j].FusedScore
	})

	return fused
}
