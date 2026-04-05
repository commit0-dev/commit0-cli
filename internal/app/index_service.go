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

// IndexRequest represents a request to index a repository
type IndexRequest struct {
	RepoPath  string
	RepoSlug  string
	Languages []string
	Force     bool
}

// IndexResult represents the result of an indexing operation
type IndexResult struct {
	FilesIndexed  int
	NodesCreated  int
	EdgesCreated  int
	Timing        types.TimingInfo
}

// IndexService orchestrates the indexing pipeline
type IndexService struct {
	walker   domain.FileWalker
	parser   domain.Parser
	embedder domain.Embedder
	store    domain.GraphStore
	builder  *ContextBuilder
	cfg      *config.Config
	log      *slog.Logger
}

// NewIndexService creates a new index service
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

// Index executes the 4-stage indexing pipeline
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
	parsedCh := make(chan *domain.ParsedFile, 64)
	parseGroup, parseCtx := errgroup.WithContext(ctx)
	parseGroup.SetLimit(parseLimit)

	go func() {
		defer close(parsedCh)
		for file := range fileCh {
			file := file // capture for closure
			parseGroup.Go(func() error {
				parsed, err := is.parser.Parse(parseCtx, file)
				if err != nil {
					is.log.Warn("parse failed", "file", file.Path, "err", err)
					run.addError()
					return nil // non-fatal
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
		parseGroup.Wait()
	}()

	// Stage 3: Embed (Gemini API, limit to 4 concurrent)
	embedCh := make(chan *embeddedFile, 32)
	embedGroup, embedCtx := errgroup.WithContext(ctx)
	embedGroup.SetLimit(is.cfg.Index.MaxWorkersEmbed)

	go func() {
		defer close(embedCh)
		batcher := NewEmbedBatcher(is.embedder, is.cfg.Index.BatchSize)
		for parsed := range parsedCh {
			parsed := parsed // capture for closure
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
		embedGroup.Wait()
	}()

	// Stage 4: Store (SurrealDB, limit to 8 concurrent)
	storeGroup, storeCtx := errgroup.WithContext(ctx)
	storeGroup.SetLimit(is.cfg.Index.MaxWorkersStore)

	for embedded := range embedCh {
		embedded := embedded // capture for closure
		storeGroup.Go(func() error {
			err := is.store.UpsertFileBatch(storeCtx, embedded.Nodes, embedded.Edges)
			if err != nil {
				is.log.Error("upsert failed", "err", err)
				run.addError()
				return nil // non-fatal
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

	return &IndexResult{
		FilesIndexed: run.filesIndexed,
		NodesCreated: run.nodesCreated,
		EdgesCreated: run.edgesCreated,
		Timing: types.TimingInfo{
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// indexRun tracks statistics during indexing
type indexRun struct {
	mu            sync.Mutex
	filesIndexed  int
	nodesCreated  int
	edgesCreated  int
	errors        int
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

// embeddedFile holds nodes with embeddings and edges
type embeddedFile struct {
	Nodes []types.CodeNode
	Edges []types.CodeEdge
}
