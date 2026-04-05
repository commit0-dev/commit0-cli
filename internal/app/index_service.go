package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// IndexRequest represents a request to index a repository.
type IndexRequest struct {
	RepoPath  string
	RepoSlug  string
	Languages []string
	Force     bool
}

// IndexResult represents the result of an indexing operation.
type IndexResult struct {
	FilesIndexed int
	NodesCreated int
	EdgesCreated int
	Timing       types.TimingInfo
}

// IndexService orchestrates the indexing pipeline.
type IndexService struct {
	walker      domain.FileWalker
	parser      domain.Parser
	embedder    domain.Embedder
	store       domain.GraphStore
	builder     *ContextBuilder
	cfg         *config.Config
	log         *slog.Logger
	parsedChBuf int // channel buffer override: <0 = unbuffered, 0 = default (64), >0 = exact size
	embedChBuf  int // channel buffer override: <0 = unbuffered, 0 = default (32), >0 = exact size
}

// NewIndexService creates a new index service.
func NewIndexService(
	walker domain.FileWalker,
	parser domain.Parser,
	embedder domain.Embedder,
	store domain.GraphStore,
	cfg *config.Config,
) *IndexService {
	return &IndexService{
		walker:   walker,
		parser:   parser,
		embedder: embedder,
		store:    store,
		builder:  NewContextBuilder(32768),
		cfg:      cfg,
		log:      slog.Default().With("service", "index"),
	}
}

// Index executes the 4-stage indexing pipeline.
func (is *IndexService) Index(ctx context.Context, req IndexRequest) (*IndexResult, error) {
	startTime := time.Now()
	run := &indexRun{}

	// Stage 1: Walk filesystem
	is.log.Info("starting walk", "repo", req.RepoSlug, "path", req.RepoPath)
	fileCh, walkErrCh := is.walker.Walk(ctx, req.RepoPath, domain.WalkOpts{
		Languages: req.Languages,
	})

	// Stage 2: Parse (CPU-bound, use GOMAXPROCS workers)
	parseLimit := is.cfg.Index.MaxWorkersParse
	if parseLimit <= 0 {
		parseLimit = runtime.GOMAXPROCS(0)
	}
	parsedCap := 64
	switch {
	case is.parsedChBuf > 0:
		parsedCap = is.parsedChBuf
	case is.parsedChBuf < 0:
		parsedCap = 0 // unbuffered — used in tests to force context-cancel path
	}
	parsedCh := make(chan *domain.ParsedFile, parsedCap)
	parseGroup, parseCtx := errgroup.WithContext(ctx)
	parseGroup.SetLimit(parseLimit)

	go func() {
		defer close(parsedCh)
		for file := range fileCh {
			parseGroup.Go(func() error {
				parsed, err := is.parser.Parse(parseCtx, file)
				if err != nil {
					is.log.Warn("parse failed", "file", file.Path, "err", err)
					run.addError()
					return nil // non-fatal
				}

				// Stamp repo slug onto every node so the store can build record IDs.
				for i := range parsed.Nodes {
					parsed.Nodes[i].RepoSlug = req.RepoSlug
				}

				run.addFilesIndexed(1)
				run.addNodesCreated(len(parsed.Nodes))
				run.addEdgesCreated(len(parsed.Edges))

				select {
				case parsedCh <- parsed:
				case <-parseCtx.Done():
					return parseCtx.Err()
				}
				return nil
			})
		}
		_ = parseGroup.Wait() // individual worker errors are non-fatal: logged and skipped
	}()

	// Stage 3: Embed (Gemini API, limit to 4 concurrent)
	embedCap := 32
	switch {
	case is.embedChBuf > 0:
		embedCap = is.embedChBuf
	case is.embedChBuf < 0:
		embedCap = 0 // unbuffered — used in tests to force context-cancel path
	}
	embedCh := make(chan *embeddedFile, embedCap)
	embedGroup, embedCtx := errgroup.WithContext(ctx)
	embedGroup.SetLimit(is.cfg.Index.MaxWorkersEmbed)

	go func() {
		defer close(embedCh)
		batcher := NewEmbedBatcher(is.embedder, is.cfg.Index.BatchSize)
		for parsed := range parsedCh {
			embedGroup.Go(func() error {
				nodes, err := batcher.Process(embedCtx, parsed.Nodes, is.builder)
				if err != nil {
					is.log.Warn("embed failed", "file", parsed.Path, "err", err)
					run.addError()
					return nil // non-fatal
				}

				select {
				case embedCh <- &embeddedFile{Nodes: nodes, Edges: parsed.Edges}:
				case <-embedCtx.Done():
					return embedCtx.Err()
				}
				return nil
			})
		}
		_ = embedGroup.Wait() // individual worker errors are non-fatal: logged and skipped
	}()

	// Stage 4: Store (SurrealDB, limit to 8 concurrent)
	storeGroup, storeCtx := errgroup.WithContext(ctx)
	storeGroup.SetLimit(is.cfg.Index.MaxWorkersStore)

	for embedded := range embedCh {
		storeGroup.Go(func() error {
			// Context cancellation is fatal — propagate for graceful shutdown
			if err := storeCtx.Err(); err != nil {
				return err
			}
			if err := is.store.UpsertFileBatch(storeCtx, embedded.Nodes, embedded.Edges); err != nil {
				is.log.Error("upsert failed", "err", err)
				run.addError()
				return nil // non-fatal store error
			}
			return nil
		})
	}

	if err := storeGroup.Wait(); err != nil {
		return nil, fmt.Errorf("store stage: %w", err)
	}

	// Drain walker errors (fatal if any)
	if err := <-walkErrCh; err != nil {
		return nil, fmt.Errorf("walk failed: %w", err)
	}

	result := &IndexResult{
		FilesIndexed: run.filesIndexed,
		NodesCreated: run.nodesCreated,
		EdgesCreated: run.edgesCreated,
		Timing: types.TimingInfo{
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}

	// Neighborhood re-embedding pass: now that all nodes and edges are stored,
	// re-embed every node with full graph context (callers, callees, data-flow).
	if _, err := is.ReembedNeighborhood(ctx, req.RepoSlug); err != nil {
		is.log.Warn("neighborhood reembed failed", "repo", req.RepoSlug, "err", err)
		// Non-fatal: base embeddings are still valid.
	}

	return result, nil
}

// ReembedResult holds statistics from a neighborhood re-embedding pass.
type ReembedResult struct {
	NodesUpdated int
	Skipped      int
	Timing       types.TimingInfo
}

// ReembedNeighborhood re-embeds all nodes for repoSlug using graph-context
// (callers, callees, data-flow neighbors). Safe to call after the main Index
// pipeline completes — that is when edges are present in the store.
func (is *IndexService) ReembedNeighborhood(ctx context.Context, repoSlug string) (*ReembedResult, error) {
	start := time.Now()

	// Builder with store so ForNodeCtx enriches context with graph neighbors.
	builder := NewContextBuilderWithStore(32768, is.store)

	ids, err := is.store.ListNodeIDs(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("list node ids: %w", err)
	}

	batchSize := is.cfg.Index.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	var updated int

	// Process IDs in chunks matching the Gemini batch size.
	for i := 0; i < len(ids); i += batchSize {
		end := min(i+batchSize, len(ids))
		chunk := ids[i:end]

		nodes := make([]types.CodeNode, 0, len(chunk))
		for _, id := range chunk {
			node, err := is.store.GetNode(ctx, id)
			if err != nil {
				is.log.Warn("reembed: get node", "id", id, "err", err)
				continue
			}
			nodes = append(nodes, *node)
		}
		if len(nodes) == 0 {
			continue
		}

		batcher := NewEmbedBatcher(is.embedder, batchSize)
		enriched, err := batcher.Process(ctx, nodes, builder)
		if err != nil {
			is.log.Warn("reembed: embed batch", "err", err)
			continue
		}

		for i := range enriched {
			n := enriched[i]
			if err := is.store.UpsertNode(ctx, &n); err != nil {
				is.log.Warn("reembed: upsert", "id", n.ID, "err", err)
				continue
			}
			updated++
		}
	}

	return &ReembedResult{
		NodesUpdated: updated,
		Timing:       types.TimingInfo{TotalMS: time.Since(start).Milliseconds()},
	}, nil
}

// indexRun tracks statistics during indexing.
type indexRun struct {
	mu           sync.Mutex
	filesIndexed int
	nodesCreated int
	edgesCreated int
	errors       int
}

func (r *indexRun) addFilesIndexed(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.filesIndexed += n
}

func (r *indexRun) addNodesCreated(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodesCreated += n
}

func (r *indexRun) addEdgesCreated(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edgesCreated += n
}

func (r *indexRun) addError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors++
}

// embeddedFile holds nodes with embeddings and edges.
type embeddedFile struct {
	Nodes []types.CodeNode
	Edges []types.CodeEdge
}
