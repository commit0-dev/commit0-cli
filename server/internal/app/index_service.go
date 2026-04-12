package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// IndexRequest represents a request to index a repository.
type IndexRequest struct {
	RepoPath  string
	RepoSlug  string
	Languages []string
	Force     bool // delete all nodes then re-index (heavy, may crash on large repos)
	Reparse   bool // skip ContentHash check, re-parse all files with current resolver (no delete)
	Fast      bool // skip LLM summarization + neighborhood re-embedding (10x faster)
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
	graph       domain.OpenCodeGraph
	builder     *ContextBuilder
	summarizer  *Summarizer
	cfg         *config.Config
	log         *slog.Logger
	parsedChBuf int            // channel buffer override: <0 = unbuffered, 0 = default (64), >0 = exact size
	progressFn  ProgressFunc   // set during IndexWithProgress, nil otherwise
	tracker     *IndexTracker  // set during IndexWithProgress, nil otherwise
	embedChBuf  int            // channel buffer override: <0 = unbuffered, 0 = default (32), >0 = exact size
	temporalSvc *TemporalService // optional: set via SetTemporalService for commit-aware indexing
	linkers     []domain.EdgeLinker // global cross-file edge resolution chain
}

// NewIndexService creates a new index service.
func NewIndexService(
	walker domain.FileWalker,
	parser domain.Parser,
	embedder domain.Embedder,
	graph domain.OpenCodeGraph,
	explainer domain.LLMExplainer,
	cfg *config.Config,
) *IndexService {
	log := slog.Default().With("service", "index")
	return &IndexService{
		walker:     walker,
		parser:     parser,
		embedder:   embedder,
		graph:      graph,
		builder:    NewContextBuilder(32768),
		summarizer: NewSummarizer(explainer, log),
		cfg:        cfg,
		log:        log,
	}
}

// SetTemporalService attaches a TemporalService for commit-aware indexing.
// When set, Index() will run temporal indexing after the main pipeline.
func (is *IndexService) SetTemporalService(svc *TemporalService) {
	is.temporalSvc = svc
}

// SetLinkers registers the EdgeLinker chain for global cross-file edge resolution.
// Linkers run after all files are parsed, with the complete SymbolTable.
func (is *IndexService) SetLinkers(linkers []domain.EdgeLinker) {
	is.linkers = linkers
}

// SetDocPrefix overrides the document embedding prefix on the ContextBuilder.
// Called by wiring when the embedding provider uses a different prefix convention
// (e.g. "search_document: " for nomic-embed-text via Ollama).
func (is *IndexService) SetDocPrefix(prefix string) {
	is.builder.SetDocPrefix(prefix)
}

// Index executes the 4-stage indexing pipeline.
func (is *IndexService) Index(ctx context.Context, req IndexRequest) (*IndexResult, error) {
	startTime := time.Now()
	run := &indexRun{onProgress: is.progressFn}

	// Force re-index: cascade-delete all existing nodes via the repo record so
	// stale records (functions/files removed from the codebase) don't persist.
	if req.Force {
		if err := is.graph.DeleteByRepo(ctx, req.RepoSlug); err != nil {
			return nil, fmt.Errorf("delete existing nodes: %w", err)
		}
		is.log.Info("deleted existing nodes", "repo", req.RepoSlug)
	}

	// Extract git metadata for repo identity and deduplication.
	git := ExtractGitMetadata(req.RepoPath)
	canonicalSlug := req.RepoSlug
	if git.Slug != "" {
		canonicalSlug = git.Slug
		is.log.Info("detected git repo", "remote", git.RemoteURL, "branch", git.Branch,
			"commit", git.CommitHash, "canonical_slug", canonicalSlug)
	}

	// Deduplicate: if a repo with the same remote URL exists, reuse its slug.
	if git.RemoteURL != "" {
		existing, _ := is.graph.FindRepoByRemoteURL(ctx, git.RemoteURL)
		if existing != nil && existing.Slug != canonicalSlug {
			is.log.Info("reusing existing repo", "existing_slug", existing.Slug, "requested_slug", req.RepoSlug)
			canonicalSlug = existing.Slug
		}
	}

	// Ensure the repo record exists — preserve existing fields via fetch+merge.
	existingRepo, _ := is.graph.GetRepo(ctx, canonicalSlug)
	repo := &types.Repo{
		Slug:          canonicalSlug,
		Path:          req.RepoPath,
		RemoteURL:     git.RemoteURL,
		DefaultBranch: git.Branch,
		LastCommit:    git.CommitHash,
		Languages:     []string{},
	}
	if existingRepo != nil {
		repo.LastIndexedAt = existingRepo.LastIndexedAt
		repo.CreatedAt = existingRepo.CreatedAt
		if len(existingRepo.Languages) > 0 {
			repo.Languages = existingRepo.Languages
		}
	}
	if err := is.graph.PutRepo(ctx, repo); err != nil {
		return nil, fmt.Errorf("upsert repo: %w", err)
	}

	// Use canonical slug for all downstream operations.
	req.RepoSlug = canonicalSlug

	// Stage 1: Walk filesystem
	is.log.Info("starting walk", "repo", req.RepoSlug, "path", req.RepoPath)
	if is.tracker != nil {
		is.tracker.StartStage(types.StageWalk)
	}
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
		if is.tracker != nil {
			is.tracker.CompleteStage(types.StageWalk)
			is.tracker.StartStage(types.StageParse)
		}
		for file := range fileCh {
			if is.tracker != nil {
				is.tracker.AddWalked(1)
			}
			parseGroup.Go(func() error {
				parsed, err := is.parser.Parse(parseCtx, file)
				if err != nil {
					is.log.Warn("parse failed", "file", file.Path, "err", err)
					run.addError()
					if is.tracker != nil {
						is.tracker.AddStageError(types.StageParse, file.Path, "", err.Error())
					}
					return nil // non-fatal
				}

				// Incremental indexing: skip files whose content hasn't changed.
				// Look up the file node by qualified name (= file path) and compare
				// its stored content hash against the freshly parsed one.
				if !req.Force && !req.Reparse && parsed.ContentHash != "" {
					existingFile, err := is.graph.FindNode(parseCtx, req.RepoSlug, file.Path)
					if err == nil && existingFile != nil && existingFile.ContentHash == parsed.ContentHash {
						is.log.Debug("skipping unchanged file", "file", file.Path)
						run.addFilesIndexed(1) // count for progress but don't re-process
						if is.tracker != nil {
							is.tracker.AddSkipped()
						}
						return nil
					}
				}

				// Stamp repo slug onto every node so the store can build record IDs.
				for i := range parsed.Nodes {
					parsed.Nodes[i].RepoSlug = req.RepoSlug
				}

				// Reparse optimization: preserve existing Summary, Concepts, and
				// Embedding from the DB. Reparse only changes edges (resolver), not
				// node content, so re-summarizing and re-embedding is wasteful.
				if req.Reparse {
					for i := range parsed.Nodes {
						n := &parsed.Nodes[i]
						existing, err := is.graph.FindNode(parseCtx, req.RepoSlug, n.Qualified)
						if err == nil && existing != nil {
							if existing.Summary != "" {
								n.Summary = existing.Summary
							}
							if len(existing.Concepts) > 0 {
								n.Concepts = existing.Concepts
							}
							if len(existing.Embedding) > 0 {
								n.Embedding = existing.Embedding
								n.ContentHash = existing.ContentHash
							}
						}
					}
				}

				run.addFilesIndexed(1)
				run.addNodesCreated(len(parsed.Nodes))
				run.addEdgesCreated(len(parsed.Edges))
				if is.tracker != nil {
					is.tracker.AddFiles(1)
					is.tracker.AddNodes(len(parsed.Nodes))
					is.tracker.AddEdges(len(parsed.Edges))
					is.tracker.IncrStage(types.StageParse, 1)
					is.tracker.AddParsed(len(parsed.Nodes), len(parsed.Edges))
					is.tracker.AddCallEdgeResolution(parsed.CallEdgesTotal, parsed.CallEdgesResolved)
				}

				select {
				case parsedCh <- parsed:
				case <-parseCtx.Done():
					return parseCtx.Err()
				}
				return nil
			})
		}
		_ = parseGroup.Wait() // individual worker errors are non-fatal: logged and skipped
		if is.tracker != nil {
			is.tracker.CompleteStage(types.StageParse)
		}
	}()

	// ── Phase 2: LINK — global cross-file edge resolution ─────────────────
	// Collect all parsed files, build a SymbolTable from ALL nodes,
	// then run the EdgeLinker chain to resolve cross-file references.

	var allParsed []*domain.ParsedFile
	for parsed := range parsedCh {
		allParsed = append(allParsed, parsed)
	}

	// Build global symbol table from ALL parsed nodes
	if len(is.linkers) > 0 && len(allParsed) > 0 {
		is.log.Info("building symbol table", "files", len(allParsed))
		var allNodes []types.CodeNode
		var allEdges []types.CodeEdge
		for _, pf := range allParsed {
			allNodes = append(allNodes, pf.Nodes...)
			allEdges = append(allEdges, pf.Edges...)
		}

		symbols := domain.BuildSymbolTable(allNodes)

		// Run each registered linker
		for _, linker := range is.linkers {
			var stats domain.LinkStats
			allEdges, stats = linker.Link(allEdges, symbols)
			is.log.Info("linker complete",
				"linker", stats.LinkerName,
				"processed", stats.Processed,
				"resolved", stats.Resolved,
				"unresolved", stats.Unresolved,
			)
			if is.tracker != nil {
				is.tracker.AddCallEdgeResolution(stats.Processed, stats.Resolved)
			}
		}

		// Redistribute resolved edges back to per-file parsed data
		edgesByFrom := make(map[string][]types.CodeEdge, len(allParsed))
		for _, e := range allEdges {
			edgesByFrom[e.FromID] = append(edgesByFrom[e.FromID], e)
		}

		// Rebuild per-file edge lists from the globally-resolved edges
		nodeFileMap := make(map[string]int) // node ID → allParsed index
		for i, pf := range allParsed {
			for _, n := range pf.Nodes {
				nodeFileMap[n.ID] = i
			}
			pf.Edges = nil // clear old edges, will be reassigned
		}
		for _, e := range allEdges {
			if idx, ok := nodeFileMap[e.FromID]; ok {
				allParsed[idx].Edges = append(allParsed[idx].Edges, e)
			} else if len(allParsed) > 0 {
				// Orphan edge (e.g., defines from global linker) → attach to first file
				allParsed[0].Edges = append(allParsed[0].Edges, e)
			}
		}
	}

	// Feed resolved parsed files into the embed/store pipeline
	resolvedCh := make(chan *domain.ParsedFile, len(allParsed))
	go func() {
		defer close(resolvedCh)
		for _, pf := range allParsed {
			resolvedCh <- pf
		}
	}()

	// Stage 3: Embed (API, limit to 4 concurrent)
	if is.tracker != nil {
		if req.Fast || req.Reparse {
			is.tracker.SkipStage(types.StageSummarize)
		} else {
			is.tracker.StartStage(types.StageSummarize)
		}
		is.tracker.StartStage(types.StageEmbed)
	}
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
		batcher := NewEmbedBatcher(is.embedder, is.cfg.BatchSize)
		for parsed := range resolvedCh {
			embedGroup.Go(func() error {
				// Summarize nodes before embedding (enriches Summary + Concepts).
				// Skip in fast/reparse mode to avoid expensive LLM calls.
				if is.summarizer != nil && !req.Fast && !req.Reparse {
					// Count nodes that get summaries (before: empty, after: non-empty)
					beforeSummary := 0
					for _, n := range parsed.Nodes {
						if n.Summary != "" {
							beforeSummary++
						}
					}
					is.summarizer.SummarizeNodes(embedCtx, parsed.Nodes)
					if is.tracker != nil {
						afterSummary := 0
						for _, n := range parsed.Nodes {
							if n.Summary != "" {
								afterSummary++
							}
						}
						is.tracker.AddSummarized(afterSummary - beforeSummary)
						is.tracker.IncrStage(types.StageSummarize, len(parsed.Nodes))
					}
				}

				// Count nodes that already have embeddings (will be skipped by batcher)
				preEmbedded := 0
				for _, n := range parsed.Nodes {
					if len(n.Embedding) > 0 {
						preEmbedded++
					}
				}

				nodes, err := batcher.Process(embedCtx, parsed.Nodes, is.builder)
				if err != nil {
					is.log.Warn("embed failed", "file", parsed.Path, "err", err)
					run.addError()
					if is.tracker != nil {
						is.tracker.AddStageError(types.StageEmbed, parsed.Path, "", err.Error())
					}
					return nil // non-fatal
				}

				// Count how many nodes now have embeddings
				newlyEmbedded := 0
				for _, n := range nodes {
					if len(n.Embedding) > 0 {
						newlyEmbedded++
					}
				}
				if is.tracker != nil {
					is.tracker.AddEmbedded(newlyEmbedded)
					is.tracker.IncrStage(types.StageEmbed, len(nodes))
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
		if is.tracker != nil {
			is.tracker.CompleteStage(types.StageSummarize)
			is.tracker.CompleteStage(types.StageEmbed)
		}
	}()

	// Stage 4: Store (SurrealDB, limit to 8 concurrent)
	if is.tracker != nil {
		is.tracker.StartStage(types.StageStore)
	}
	storeGroup, storeCtx := errgroup.WithContext(ctx)
	storeGroup.SetLimit(is.cfg.Index.MaxWorkersStore)

	for embedded := range embedCh {
		storeGroup.Go(func() error {
			// Context cancellation is fatal — propagate for graceful shutdown
			if err := storeCtx.Err(); err != nil {
				return err
			}
			if err := is.graph.PutBatch(storeCtx, embedded.Nodes, embedded.Edges); err != nil {
				is.log.Error("upsert failed", "err", err)
				run.addError()
				if is.tracker != nil {
					is.tracker.AddStageError(types.StageStore, "", "", err.Error())
				}
				return nil // non-fatal store error
			}
			if is.tracker != nil {
				is.tracker.AddStored(len(embedded.Nodes), len(embedded.Edges))
				is.tracker.IncrStage(types.StageStore, len(embedded.Nodes))
			}
			return nil
		})
	}

	if err := storeGroup.Wait(); err != nil {
		return nil, fmt.Errorf("store stage: %w", err)
	}
	if is.tracker != nil {
		is.tracker.CompleteStage(types.StageStore)
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
	// Skip on --reparse/--fast: reembed is expensive (re-embeds ALL nodes with graph context).
	if req.Reparse || req.Fast {
		is.log.Info("skipping neighborhood re-embed (fast/reparse mode)", "repo", req.RepoSlug)
		if is.tracker != nil {
			is.tracker.SkipStage(types.StageReembed)
		}
	} else {
		if is.tracker != nil {
			is.tracker.StartStage(types.StageReembed)
		}
		if _, err := is.ReembedNeighborhood(ctx, req.RepoSlug); err != nil {
			is.log.Warn("neighborhood reembed failed", "repo", req.RepoSlug, "err", err)
			if is.tracker != nil {
				is.tracker.FailStage(types.StageReembed, err.Error())
			}
		} else if is.tracker != nil {
			is.tracker.CompleteStage(types.StageReembed)
		}
	}

	// Temporal indexing: walk recent git history to stamp introduced_commit /
	// last_modified_commit on nodes and edges.
	if is.temporalSvc != nil && req.RepoPath != "" {
		if is.tracker != nil {
			is.tracker.StartStage(types.StageTemporal)
		}
		is.log.Info("running temporal indexing", "repo", req.RepoSlug)
		if err := is.temporalSvc.IndexCommitRange(ctx, TemporalIndexRequest{
			RepoPath:   req.RepoPath,
			RepoSlug:   req.RepoSlug,
			FromCommit: "HEAD~50", // last 50 commits for incremental
			ToCommit:   "HEAD",
		}); err != nil {
			is.log.Warn("temporal indexing failed", "repo", req.RepoSlug, "err", err)
			if is.tracker != nil {
				is.tracker.FailStage(types.StageTemporal, err.Error())
			}
		} else if is.tracker != nil {
			is.tracker.CompleteStage(types.StageTemporal)
		}
	} else if is.tracker != nil {
		is.tracker.SkipStage(types.StageTemporal)
	}

	// Cleanup: remove nodes for files that no longer exist on disk.
	if req.RepoPath != "" {
		if is.tracker != nil {
			is.tracker.StartStage(types.StageCleanup)
		}
		is.cleanupStaleNodes(ctx, req.RepoSlug, req.RepoPath)
		if is.tracker != nil {
			is.tracker.CompleteStage(types.StageCleanup)
		}
	} else if is.tracker != nil {
		is.tracker.SkipStage(types.StageCleanup)
	}

	// Mark repo as indexed using MERGE (doesn't wipe other fields).
	if err := is.graph.UpdateRepoIndexedAt(ctx, req.RepoSlug, time.Now()); err != nil {
		is.log.Warn("failed to update LastIndexedAt", "repo", req.RepoSlug, "err", err)
	}

	return result, nil
}

// cleanupStaleNodes removes nodes for files that no longer exist on disk.
func (is *IndexService) cleanupStaleNodes(ctx context.Context, repoSlug, repoPath string) {
	// Use lightweight ListNodeIDs + GetNode(file_path only) instead of
	// ListAllNodes which transfers ALL fields (body, embedding) for every node.
	fileNodes, err := is.graph.ListFilePaths(ctx, repoSlug)
	if err != nil {
		// Fallback: try the old way if ListFilePaths isn't available.
		is.log.Warn("cleanup: list file paths failed, skipping", "err", err)
		return
	}

	// Collect unique file paths from the graph.
	graphFiles := make(map[string]bool)
	for _, fp := range fileNodes {
		if fp != "" {
			graphFiles[fp] = true
		}
	}

	// Check each file path against the filesystem.
	var deleted int
	for filePath := range graphFiles {
		fullPath := filepath.Join(repoPath, filePath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			is.log.Info("cleanup: removing stale file nodes", "file", filePath)
			if err := is.graph.DeleteByFile(ctx, repoSlug, filePath); err != nil {
				is.log.Warn("cleanup: delete failed", "file", filePath, "err", err)
			} else {
				deleted++
			}
		}
	}
	if deleted > 0 {
		is.log.Info("cleanup: removed stale nodes", "files_deleted", deleted)
	}
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
	builder := NewContextBuilderWithGraph(32768, is.graph)

	ids, err := is.graph.ListNodes(ctx, repoSlug, domain.ListOpts{IDsOnly: true})
	if err != nil {
		return nil, fmt.Errorf("list node ids: %w", err)
	}

	batchSize := is.cfg.BatchSize
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
			node, err := is.graph.GetNode(ctx, id.ID)
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
			if err := is.graph.PutNode(ctx, &n); err != nil {
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

// ProgressFunc is called with incremental indexing progress.
type ProgressFunc func(filesIndexed, nodesCreated int)

// IndexWithProgress runs Index with a progress callback and an optional
// tracker for comprehensive stage-level progress reporting.
func (is *IndexService) IndexWithProgress(ctx context.Context, req IndexRequest, onProgress ProgressFunc, tracker *IndexTracker) (*IndexResult, error) {
	is.progressFn = onProgress
	is.tracker = tracker
	defer func() {
		is.progressFn = nil
		is.tracker = nil
	}()
	return is.Index(ctx, req)
}

// indexRun tracks statistics during indexing.
type indexRun struct {
	mu           sync.Mutex
	filesIndexed int
	nodesCreated int
	edgesCreated int
	errors       int
	onProgress   ProgressFunc
}

func (r *indexRun) addFilesIndexed(n int) {
	r.mu.Lock()
	r.filesIndexed += n
	fn := r.onProgress
	files := r.filesIndexed
	nodes := r.nodesCreated
	r.mu.Unlock()
	if fn != nil {
		fn(files, nodes)
	}
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

// ReEmbedResult holds the result of a re-embedding operation.
type ReEmbedResult struct {
	NodesEmbedded int
	NodesTotal    int
	Provider      string
}

// ReEmbed re-embeds all nodes for a repo using the current embedding provider.
// This is faster than full re-indexing because it skips filesystem walk,
// tree-sitter parsing, and LLM summarization — only calls the embedding API.
func (is *IndexService) ReEmbed(ctx context.Context, repoSlug string, onProgress func(done, total int)) (*ReEmbedResult, error) {
	is.log.Info("starting re-embed", "repo", repoSlug)

	nodeIDs, err := is.graph.ListNodes(ctx, repoSlug, domain.ListOpts{IDsOnly: true})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	total := len(nodeIDs)
	if total == 0 {
		return &ReEmbedResult{Provider: is.cfg.EmbedProvider}, nil
	}

	batchSize := 100
	done := 0

	for i := 0; i < len(nodeIDs); i += batchSize {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		end := i + batchSize
		if end > len(nodeIDs) {
			end = len(nodeIDs)
		}
		batch := nodeIDs[i:end]

		// Fetch nodes and build embedding inputs.
		var inputs []domain.EmbedInput
		var nodes []*types.CodeNode
		for _, id := range batch {
			node, err := is.graph.GetNode(ctx, id.ID)
			if err != nil {
				is.log.Debug("skip node", "id", id, "err", err)
				continue
			}
			if node.Embedding != nil {
				done++
				continue // already embedded with current provider
			}
			text := is.builder.ForNode(node)
			inputs = append(inputs, domain.EmbedInput{
				ID:   node.ID,
				Text: text,
			})
			nodes = append(nodes, node)
		}

		if len(inputs) == 0 {
			continue
		}

		// Embed batch.
		results, err := is.embedder.EmbedBatch(ctx, inputs)
		if err != nil {
			is.log.Warn("embed batch failed", "err", err)
			continue
		}

		// Update nodes with new embeddings.
		resultMap := make(map[string][]float32, len(results))
		for _, r := range results {
			resultMap[r.ID] = r.Vector
		}
		for _, node := range nodes {
			if vec, ok := resultMap[node.ID]; ok {
				node.Embedding = vec
				if err := is.graph.PutNode(ctx, node); err != nil {
					is.log.Warn("upsert failed", "id", node.ID, "err", err)
				}
				done++
			}
		}

		if onProgress != nil {
			onProgress(done, total)
		}
	}

	is.log.Info("re-embed complete", "repo", repoSlug, "embedded", done, "total", total)
	return &ReEmbedResult{
		NodesEmbedded: done,
		NodesTotal:    total,
		Provider:      is.cfg.EmbedProvider,
	}, nil
}
