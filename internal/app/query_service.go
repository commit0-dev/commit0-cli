package app

import (
	"context"
	"fmt"
	"log/slog"
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
	return &QueryService{
		embedder:  embedder,
		vectorIdx: vectorIdx,
		textIdx:   textIdx,
		store:     store,
		explainer: explainer,
		cfg:       cfg,
		log:       slog.Default().With("service", "query"),
	}
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

	// Fuse results
	fused := ReciprocalRankFusion(vectorHits, ftsHits, DefaultRRFWeights())
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

	// Generate explanation (non-fatal if fails)
	explainStart := time.Now()
	explanation := ""
	if qs.explainer != nil {
		chunks, err := qs.explainer.Explain(ctx, domain.ExplainRequest{
			QueryType:   "search",
			UserQuery:   req.Question,
			CodeContext: excerpts,
		})
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
	explainMS := time.Since(explainStart).Milliseconds()

	return &types.QueryResult{
		Nodes:       fused,
		Explanation: explanation,
		Query:       req.Question,
		RepoSlug:    req.RepoSlug,
		Timing: types.TimingInfo{
			EmbedMS:   embedMS,
			SearchMS:  searchMS,
			ExplainMS: explainMS,
			TotalMS:   time.Since(startTime).Milliseconds(),
		},
	}, nil
}
