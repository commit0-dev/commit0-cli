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

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// QueryRequest represents a natural language query.
type QueryRequest struct {
	Question  string
	RepoSlug  string
	NodeKinds []types.NodeKind
	TopK      int
	MinScore  float64
	NoExplain bool
	FilePath  string // filter results to this file/directory prefix
}

// QueryService handles semantic code search.
type QueryService struct {
	embedder  domain.Embedder
	graph     domain.OpenCodeGraph
	explainer domain.LLMExplainer
	flowSvc   *DataFlowService // optional: enriches LLM context with data-flow paths
	cfg       *config.Config
	log       *slog.Logger
}

// NewQueryService creates a new query service.
func NewQueryService(
	embedder domain.Embedder,
	graph domain.OpenCodeGraph,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *QueryService {
	qs := &QueryService{
		embedder:  embedder,
		graph:     graph,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "query"),
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
		hits, err := qs.graph.VectorSearch(gCtx, queryVec, domain.VectorSearchOpts{
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
		hits, err := qs.graph.TextSearch(gCtx, req.Question, domain.TextSearchOpts{
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

	// File-path filter: keep only nodes matching the prefix.
	if req.FilePath != "" {
		filtered := fused[:0]
		for _, n := range fused {
			if strings.HasPrefix(n.Node.FilePath, req.FilePath) {
				filtered = append(filtered, n)
			}
		}
		fused = filtered
	}

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
	// Skip when NoExplain is set — saves 5-15 seconds per query.
	explainStart := time.Now()
	explanation := ""
	var structuredExplan *types.SearchExplanation
	if qs.explainer != nil && !req.NoExplain {
		explainReq := domain.ExplainRequest{
			QueryType:      "search",
			UserQuery:      req.Question,
			GraphContext:   graphContext,
			CodeContext:    excerpts,
			ResponseSchema: domain.SchemaForQueryType("search"),
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

// QueryStream executes a semantic code search and emits staged events via the provided channel.
// The channel is expected to be buffered (256+ recommended) and consumed by the caller.
// QueryStream closes the channel when done or on fatal error.
func (qs *QueryService) QueryStream(ctx context.Context, req QueryRequest, events chan<- types.QueryEvent) error {
	startTime := time.Now()
	defer close(events)

	if req.Question == "" {
		emitError(events, "validation", "question cannot be empty")
		return domain.Validation("question cannot be empty")
	}

	if req.TopK <= 0 {
		req.TopK = qs.cfg.Query.DefaultTopK
	}
	if req.MinScore == 0 {
		req.MinScore = qs.cfg.Query.MinScore
	}

	if len(req.NodeKinds) == 0 {
		req.NodeKinds = []types.NodeKind{types.NodeFunction, types.NodeClass, types.NodeFile}
	}

	embedStart := time.Now()
	queryVec, err := qs.embedder.EmbedQuery(ctx, req.Question)
	if err != nil {
		emitError(events, "embedding", fmt.Sprintf("embed query: %v", err))
		return err
	}
	embedMS := time.Since(embedStart).Milliseconds()
	embedDims := len(queryVec)

	emitEvent(events, types.QueryEvent{
		Type:      types.QueryEventEmbeddingDone,
		Dims:      embedDims,
		MS:        embedMS,
		EmittedAt: time.Now(),
	})

	searchStart := time.Now()
	var vectorHits, ftsHits []types.ScoredNode

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		hits, err := qs.graph.VectorSearch(gCtx, queryVec, domain.VectorSearchOpts{
			RepoSlug:  req.RepoSlug,
			TopK:      req.TopK * 2,
			MinScore:  req.MinScore,
			NodeKinds: req.NodeKinds,
		})
		if err != nil {
			return fmt.Errorf("vector search: %w", err)
		}
		vectorHits = hits
		for _, hit := range hits {
			emitEvent(events, types.QueryEvent{
				Type:      types.QueryEventVectorHit,
				Hit:       &hit,
				EmittedAt: time.Now(),
			})
		}
		return nil
	})

	g.Go(func() error {
		hits, err := qs.graph.TextSearch(gCtx, req.Question, domain.TextSearchOpts{
			RepoSlug:  req.RepoSlug,
			TopK:      req.TopK * 2,
			NodeKinds: req.NodeKinds,
		})
		if err != nil {
			return fmt.Errorf("fts search: %w", err)
		}
		ftsHits = hits
		for _, hit := range hits {
			emitEvent(events, types.QueryEvent{
				Type:      types.QueryEventFTSHit,
				Hit:       &hit,
				EmittedAt: time.Now(),
			})
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		emitError(events, "search", err.Error())
		return err
	}
	searchMS := time.Since(searchStart).Milliseconds()

	fused := ReciprocalRankFusion(vectorHits, ftsHits, DefaultRRFWeights())

	emitEvent(events, types.QueryEvent{
		Type:         types.QueryEventFused,
		TopKAfterRRF: fused,
		EmittedAt:    time.Now(),
	})

	expanded := qs.expandWithGraph(ctx, fused, req.RepoSlug, req.TopK)

	newNeighbors := expanded
	if len(expanded) > len(fused) {
		newNeighbors = expanded[len(fused):]
	}
	emitEvent(events, types.QueryEvent{
		Type:      types.QueryEventExpanded,
		Neighbors: newNeighbors,
		EmittedAt: time.Now(),
	})

	reranked := qs.conceptRerank(expanded, req.Question)

	emitEvent(events, types.QueryEvent{
		Type:       types.QueryEventReranked,
		FinalOrder: reranked,
		EmittedAt:  time.Now(),
	})

	if req.FilePath != "" {
		filtered := reranked[:0]
		for _, n := range reranked {
			if strings.HasPrefix(n.Node.FilePath, req.FilePath) {
				filtered = append(filtered, n)
			}
		}
		reranked = filtered
	}

	if len(reranked) > req.TopK {
		reranked = reranked[:req.TopK]
	}

	excerpts := make([]domain.CodeExcerpt, 0, len(reranked))
	for _, scored := range reranked {
		excerpts = append(excerpts, domain.CodeExcerpt{
			Qualified: scored.Node.Qualified,
			FilePath:  scored.Node.FilePath,
			Snippet:   scored.Node.Body,
			Score:     scored.FusedScore,
		})
	}

	graphContext := ""
	if qs.flowSvc != nil {
		graphContext = qs.flowSvc.BuildFlowContext(ctx, reranked)
	}

	explanation := ""
	var structuredExplan *types.SearchExplanation
	explainStart := time.Now()

	if qs.explainer != nil && !req.NoExplain {
		explainReq := domain.ExplainRequest{
			QueryType:      "search",
			UserQuery:      req.Question,
			GraphContext:   graphContext,
			CodeContext:    excerpts,
			ResponseSchema: domain.SchemaForQueryType("search"),
		}

		raw, err := qs.explainer.ExplainStructured(ctx, explainReq)
		if err == nil {
			var se types.SearchExplanation
			if json.Unmarshal(raw, &se) == nil {
				structuredExplan = &se
				explanation = se.Overview
			}
		}

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
					if chunk.Text != "" {
						emitEvent(events, types.QueryEvent{
							Type:      types.QueryEventExplanationToken,
							Delta:     chunk.Text,
							EmittedAt: time.Now(),
						})
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

	result := &types.QueryResult{
		Nodes:                 reranked,
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
	}

	emitEvent(events, types.QueryEvent{
		Type:      types.QueryEventDone,
		Done:      result,
		EmittedAt: time.Now(),
	})

	return nil
}

// emitEvent sends an event to the channel non-blocking.
func emitEvent(ch chan<- types.QueryEvent, evt types.QueryEvent) {
	select {
	case ch <- evt:
	default:
	}
}

// emitError sends an error event.
func emitError(ch chan<- types.QueryEvent, stage, message string) {
	select {
	case ch <- types.QueryEvent{
		Type:      types.QueryEventError,
		Stage:     stage,
		Message:   message,
		EmittedAt: time.Now(),
	}:
	default:
	}
}

// expandWithGraph pulls 1-hop callers + callees of top results into the
// candidate pool, deduplicating by node ID. This ensures that when we find
// "MemoryStore.Save", we also surface "MemoryStore.Load" and "MemoryStore.Clear".
func (qs *QueryService) expandWithGraph(ctx context.Context, fused []types.ScoredNode, repoSlug string, topK int) []types.ScoredNode {
	if qs.graph == nil {
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
		nb, err := qs.graph.Neighbors(ctx, node.Node.ID)
		if err != nil || nb == nil {
			continue
		}

		// Add callers/callees as candidates with a reduced score
		neighborScore := node.FusedScore * 0.6
		for _, caller := range nb.Callers {
			callerNode, err := qs.graph.FindNode(ctx, repoSlug, caller.Qualified)
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
			calleeNode, err := qs.graph.FindNode(ctx, repoSlug, callee.Qualified)
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

		// Centrality boost: well-connected nodes get a mild relevance bump.
		// Capped to prevent hub nodes from dominating semantic results.
		if fused[i].Centrality > 0 {
			centralityBoost := math.Min(1+math.Log(float64(fused[i].Centrality)+1)*0.1, 1.3)
			boost *= centralityBoost
		}

		fused[i].FusedScore *= boost
	}

	// Re-sort after boosting
	sort.Slice(fused, func(i, j int) bool {
		return fused[i].FusedScore > fused[j].FusedScore
	})

	return fused
}
