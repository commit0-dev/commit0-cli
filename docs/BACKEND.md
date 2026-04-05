# commit0 — Core Backend Architecture

> Detailed software architecture, API design, concurrency model, and implementation
> patterns for the commit0 backend engine.

**Companion documents:**
- `ARCHITECTURE.md` — High-level vision, tech stack, directory layout, CLI reference
- `DATABASE.md` — SurrealDB 3.0 schema, indexes, query patterns, DEFINE API

---

## 1. Architectural Philosophy

commit0 is a **local-first developer tool** that ships as a single Go binary. The
architecture prioritizes:

| Principle | Implementation |
|---|---|
| Zero friction install | Single binary, no runtime deps, `curl \| sh` or `brew install` |
| Offline-capable | All graph queries work without network; only embedding/LLM need Gemini API |
| Fast incremental updates | SHA-256 cache, git-diff aware, changefeed-driven re-indexing |
| Hybrid retrieval | Vector ANN + graph traversal + full-text in one SurrealQL statement |
| Streaming-first | SSE for long explanations; chunked progress for indexing |
| Extensible by interface | Every subsystem behind a Go interface; swap embedding model, DB, or parser |

### 1.1 Inspiration & Prior Art

commit0 draws from several existing approaches to code intelligence:

**DeepWiki** (Cognition/Devin) generates wiki-style documentation by analyzing
repository structure, extracting modules and dependencies, and using LLMs to
produce human-readable explanations with Mermaid diagrams. commit0 borrows the
idea of LLM-generated explanations anchored to code structure, but replaces
DeepWiki's document-generation model with an interactive query model backed by a
persistent knowledge graph.

**Neo4j Codebase Knowledge Graphs** demonstrate building call graphs and
dependency trees from source code using graph databases. commit0 extends this
pattern by embedding every node in a shared vector space, enabling queries that
combine structural reachability with semantic similarity — something a pure
graph approach cannot do.

**GraphGen4Code** (IBM Research) built knowledge graphs for 1.3M Python programs,
proving that tree-sitter-based extraction scales to massive codebases. commit0
uses the same tree-sitter foundation but adds real-time incremental indexing
and fused multimodal embeddings.

**Hybrid RAG architectures** (2025-2026) combine vector search, keyword
matching, and knowledge graph traversal for code retrieval. commit0 implements
this natively via SurrealDB's ability to execute all three retrieval strategies
in a single query.

---

## 2. Layered Architecture

commit0 follows a **ports-and-adapters** (hexagonal) pattern adapted for a CLI
tool. The core domain logic has no knowledge of SurrealDB, Gemini, or
tree-sitter — it operates on interfaces. This makes every external dependency
swappable and every domain operation unit-testable without infrastructure.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          DRIVING ADAPTERS (input)                           │
│                                                                             │
│   ┌────────────────┐  ┌────────────────┐  ┌─────────────────────────────┐  │
│   │  CLI (Cobra)   │  │  HTTP (Echo)   │  │  SurrealDB DEFINE API       │  │
│   │  cmd/*.go      │  │  server/*.go   │  │  (DB-native endpoints)      │  │
│   └───────┬────────┘  └───────┬────────┘  └────────────┬────────────────┘  │
│           │                   │                        │                    │
│           └───────────────────┼────────────────────────┘                    │
│                               │                                             │
│                               ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                    APPLICATION SERVICES                               │   │
│  │                    (orchestration layer)                               │   │
│  │                                                                       │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │   │
│  │  │  IndexService │  │ QueryService │  │ TraceService │               │   │
│  │  │              │  │              │  │              │               │   │
│  │  │  Orchestrates │  │  NL → embed  │  │  Symbol →   │               │   │
│  │  │  walk→parse→  │  │  → search →  │  │  graph walk │               │   │
│  │  │  embed→store  │  │  rerank →    │  │  → tree →   │               │   │
│  │  │              │  │  explain     │  │  explain    │               │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘               │   │
│  │                                                                       │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │   │
│  │  │ BlastService │  │ RepoService  │  │SessionService│               │   │
│  │  │              │  │              │  │              │               │   │
│  │  │  Reverse      │  │  CRUD repos  │  │  Multi-turn  │               │   │
│  │  │  transitive   │  │  manage      │  │  conversation│               │   │
│  │  │  traversal    │  │  lifecycle   │  │  context     │               │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘               │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                               │                                             │
│                               ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                       DOMAIN CORE                                     │   │
│  │                   (pure Go, no dependencies)                          │   │
│  │                                                                       │   │
│  │  ┌────────────────────────────────────────────────────────────────┐  │   │
│  │  │  Domain Types                                                   │  │   │
│  │  │  ──────────────                                                 │  │   │
│  │  │  CodeNode (File | Function | Class | Module)                    │  │   │
│  │  │  CodeEdge (Calls | Imports | Defines | Inherits | Uses)        │  │   │
│  │  │  Repo, IndexRun, Session, QueryResult, TraceResult, BlastResult │  │   │
│  │  └────────────────────────────────────────────────────────────────┘  │   │
│  │                                                                       │   │
│  │  ┌────────────────────────────────────────────────────────────────┐  │   │
│  │  │  Port Interfaces (contracts)                                    │  │   │
│  │  │  ─────────────────────────                                      │  │   │
│  │  │  GraphStore   — CRUD nodes, edges, graph traversal              │  │   │
│  │  │  VectorIndex  — ANN search, similarity scoring                  │  │   │
│  │  │  TextIndex    — Full-text search, BM25 scoring                  │  │   │
│  │  │  Embedder     — Text/code/image → vector                        │  │   │
│  │  │  LLMExplainer — Code context → natural language explanation      │  │   │
│  │  │  Parser       — Source file → AST nodes + edges                 │  │   │
│  │  │  FileWalker   — Repo path → file entries                        │  │   │
│  │  └────────────────────────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                               │                                             │
│                               ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                      DRIVEN ADAPTERS (output)                         │   │
│  │                                                                       │   │
│  │  ┌───────────────┐  ┌────────────────┐  ┌────────────────────────┐  │   │
│  │  │  SurrealDB    │  │  Gemini SDK    │  │  tree-sitter (CGO)    │  │   │
│  │  │  Adapter      │  │  Adapter       │  │  Adapter              │  │   │
│  │  │               │  │                │  │                        │  │   │
│  │  │  Implements:  │  │  Implements:   │  │  Implements:           │  │   │
│  │  │  GraphStore   │  │  Embedder      │  │  Parser                │  │   │
│  │  │  VectorIndex  │  │  LLMExplainer  │  │                        │  │   │
│  │  │  TextIndex    │  │                │  │                        │  │   │
│  │  └───────────────┘  └────────────────┘  └────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.1 Why Ports and Adapters for a CLI Tool?

A CLI tool doesn't typically need hexagonal architecture — but commit0 is not a
typical CLI. It orchestrates three complex external systems (SurrealDB, Gemini
API, tree-sitter C libraries) that each have their own failure modes, version
changes, and rate limits. Isolating the domain means:

- **Testability**: IndexService can be tested with in-memory stubs for GraphStore
  and Embedder, without SurrealDB or Gemini credentials.
- **Swappability**: Replace Gemini with OpenAI embeddings by implementing the
  Embedder interface — zero changes to business logic.
- **SurrealDB DEFINE API coexistence**: The same domain logic serves both the
  Echo HTTP server and SurrealDB's native API endpoints via different driving
  adapters.

---

## 3. Domain Core

### 3.1 Domain Types

All core types live in `pkg/types/` (exported for potential SDK use) and
`internal/domain/` (internal orchestration types). No external imports.

```go
// pkg/types/ast.go — Code graph nodes

type NodeKind string

const (
    NodeFile     NodeKind = "file"
    NodeFunction NodeKind = "function"
    NodeClass    NodeKind = "class"
    NodeModule   NodeKind = "module"
)

type CodeNode struct {
    ID           string            // SurrealDB record ID (e.g., "function:pkg⋅Handler⋅ServeHTTP")
    Kind         NodeKind
    Name         string            // Short name
    Qualified    string            // Fully qualified: pkg.Receiver.Method
    FilePath     string            // Relative to repo root
    RepoSlug     string
    Language     string
    StartLine    int
    EndLine      int
    Signature    string            // Function params + return types
    Docstring    string
    Body         string            // Raw source code
    ContentHash  string            // SHA-256 of embedding input
    Embedding    []float32         // 3072-dim vector (nil if not yet embedded)
    Visibility   string            // public | private | protected | internal
}

type EdgeKind string

const (
    EdgeCalls    EdgeKind = "calls"
    EdgeImports  EdgeKind = "imports"
    EdgeDefines  EdgeKind = "defines"
    EdgeInherits EdgeKind = "inherits"
    EdgeUses     EdgeKind = "uses"
)

type CodeEdge struct {
    Kind      EdgeKind
    FromID    string              // Source node record ID
    ToID      string              // Target node record ID
    CallSite  string              // "file.go:42" (calls edges only)
    IsDynamic bool                // Interface dispatch (calls edges only)
    CallType  string              // direct | interface | callback | goroutine | deferred
    Metadata  map[string]string   // Edge-specific: alias, usage_type, inherit_kind
}
```

```go
// pkg/types/result.go — Query result types

type QueryResult struct {
    Nodes       []ScoredNode
    Explanation string              // Gemini-generated prose
    Query       string              // Original user question
    RepoSlug    string
    Timing      TimingInfo
}

type ScoredNode struct {
    Node         CodeNode
    VectorScore  float64           // Cosine similarity (0-1)
    FTSScore     float64           // BM25 relevance
    FusedScore   float64           // RRF-combined final score
    Centrality   int               // In-degree + out-degree in call graph
}

type TraceResult struct {
    Root         CodeNode
    Tree         []TraceHop         // Ordered by hop distance
    Direction    string             // "forward" | "reverse"
    Explanation  string
    Timing       TimingInfo
}

type TraceHop struct {
    Depth    int
    Node     CodeNode
    Edge     CodeEdge              // The edge that led here
    Children []TraceHop            // Sub-tree (recursive)
}

type BlastResult struct {
    Target       CodeNode
    Affected     []AffectedNode
    Summary      string             // Gemini-generated impact summary
    Timing       TimingInfo
}

type AffectedNode struct {
    Node     CodeNode
    HopCount int
    Module   string                // Grouped by package/module
    Path     string                // Call path from target to this node
}

type TimingInfo struct {
    EmbedMS   int64
    SearchMS  int64
    GraphMS   int64
    ExplainMS int64
    TotalMS   int64
}
```

### 3.2 Port Interfaces

These interfaces define the contracts between domain logic and infrastructure.
Each interface is minimal — one responsibility, easy to mock.

```go
// internal/domain/ports.go

// GraphStore handles CRUD for code nodes and edges in the graph database.
type GraphStore interface {
    // Node operations
    UpsertNode(ctx context.Context, node *types.CodeNode) error
    GetNode(ctx context.Context, id string) (*types.CodeNode, error)
    GetNodeByQualified(ctx context.Context, repo, qualified string) (*types.CodeNode, error)
    DeleteNode(ctx context.Context, id string) error
    DeleteNodesByRepo(ctx context.Context, repo string) error

    // Edge operations
    UpsertEdge(ctx context.Context, edge *types.CodeEdge) error
    DeleteEdgesForNode(ctx context.Context, nodeID string) error

    // Graph traversal
    TraceForward(ctx context.Context, startID string, depth int) ([]types.TraceHop, error)
    TraceReverse(ctx context.Context, startID string, depth int) ([]types.TraceHop, error)
    BlastRadius(ctx context.Context, targetID string, maxDepth int) ([]types.AffectedNode, error)

    // Batch operations (transactional)
    UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error

    // Repo management
    UpsertRepo(ctx context.Context, repo *types.Repo) error
    GetRepo(ctx context.Context, slug string) (*types.Repo, error)
    ListRepos(ctx context.Context) ([]types.Repo, error)

    // Schema
    ApplySchema(ctx context.Context) error
    GetSchemaVersion(ctx context.Context) (int, error)
}

// VectorIndex handles approximate nearest-neighbor search over embeddings.
type VectorIndex interface {
    Search(ctx context.Context, query []float32, opts VectorSearchOpts) ([]types.ScoredNode, error)
}

type VectorSearchOpts struct {
    RepoSlug  string
    TopK      int
    MinScore  float64
    NodeKinds []types.NodeKind    // Filter: only functions, only classes, etc.
    Effort    int                  // HNSW effort parameter (higher = more accurate)
}

// TextIndex handles BM25 full-text search.
type TextIndex interface {
    Search(ctx context.Context, query string, opts TextSearchOpts) ([]types.ScoredNode, error)
}

type TextSearchOpts struct {
    RepoSlug  string
    TopK      int
    Fields    []string            // name, qualified, docstring
    NodeKinds []types.NodeKind
}

// Embedder converts text/code/images into dense vector embeddings.
type Embedder interface {
    // EmbedBatch sends up to 100 inputs in one API call.
    EmbedBatch(ctx context.Context, inputs []EmbedInput) ([]EmbedResult, error)

    // EmbedQuery embeds a user's natural language question (query-side prefix).
    EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

type EmbedInput struct {
    ID          string             // Node record ID (for matching results)
    Text        string             // Pre-formatted embedding input text
    Images      [][]byte           // Optional: architecture diagrams
    ImageMIMEs  []string           // "image/png", "image/jpeg"
    ContentHash string             // SHA-256 for cache check
}

type EmbedResult struct {
    ID        string
    Vector    []float32            // 3072-dim
    Cached    bool                 // True if hash matched, no API call made
}

// LLMExplainer generates natural-language explanations from code context.
type LLMExplainer interface {
    // Explain sends code context to the LLM and streams back an explanation.
    Explain(ctx context.Context, req ExplainRequest) (<-chan ExplainChunk, error)
}

type ExplainRequest struct {
    QueryType   string             // "search" | "trace" | "blast"
    UserQuery   string
    CodeContext []CodeExcerpt       // Relevant code snippets with metadata
    GraphContext string             // Serialized graph neighborhood
}

type ExplainChunk struct {
    Text  string
    Done  bool
    Error error
}

type CodeExcerpt struct {
    Qualified string
    FilePath  string
    Lines     string               // "42-68"
    Snippet   string               // Trimmed code body
    Score     float64              // Retrieval score
}

// Parser extracts AST nodes and edges from source code files.
type Parser interface {
    // Parse returns all extractable symbols and relationships from a file.
    Parse(ctx context.Context, file FileEntry) (*ParsedFile, error)

    // SupportedLanguages returns the set of languages this parser handles.
    SupportedLanguages() []string
}

type FileEntry struct {
    Path     string                // Relative to repo root
    AbsPath  string                // Absolute filesystem path
    Language string                // Detected language
    Content  []byte                // Raw file bytes
}

type ParsedFile struct {
    Path        string
    Language    string
    ContentHash string             // SHA-256 of file content
    Nodes       []types.CodeNode
    Edges       []types.CodeEdge
    LineCount   int
    SizeBytes   int
}

// FileWalker traverses a repository and yields file entries.
type FileWalker interface {
    // Walk yields files matching the configured language and size filters.
    Walk(ctx context.Context, repoPath string, opts WalkOpts) (<-chan FileEntry, <-chan error)
}

type WalkOpts struct {
    Languages  []string            // Filter by language
    Exclude    []string            // Glob patterns to skip
    MaxFileKB  int                 // Skip files larger than this
}
```

---

## 4. Application Services

Application services orchestrate the ports to implement business workflows.
They live in `internal/app/` and are the only layer that composes multiple
ports together.

### 4.1 IndexService — The Indexing Pipeline

The indexing pipeline is the most complex workflow: walk files, parse ASTs,
build embedding context, batch-embed, and transactionally upsert into SurrealDB.

```go
// internal/app/index_service.go

type IndexService struct {
    walker    domain.FileWalker
    parser    domain.Parser
    embedder  domain.Embedder
    store     domain.GraphStore
    builder   *ContextBuilder         // Constructs embedding input text
    cfg       *config.Config
    log       *slog.Logger
}

func NewIndexService(
    walker domain.FileWalker,
    parser domain.Parser,
    embedder domain.Embedder,
    store domain.GraphStore,
    cfg *config.Config,
) *IndexService { ... }
```

#### 4.1.1 Pipeline Stages

The pipeline uses a staged concurrent architecture with bounded channels
between stages:

```
Stage 1: Walk          Stage 2: Parse         Stage 3: Embed         Stage 4: Store
(1 goroutine)          (N workers)            (M workers)            (P workers)
                       errgroup               errgroup               errgroup
                       N = GOMAXPROCS         M = 4 (API bound)     P = 8

  repoPath             fileCh                 parsedCh               embedCh
     │                 chan FileEntry          chan ParsedFile         chan EmbeddedFile
     ▼                    │                      │                       │
  ┌────────┐             ▼                      ▼                       ▼
  │ Walker │──────▶ ┌──────────┐ ────────▶ ┌───────────┐ ────────▶ ┌──────────┐
  │        │       │ Parser   │           │ Embedder  │           │ Upsert   │
  │ Walk() │       │ workers  │           │ workers   │           │ workers  │
  └────────┘       └──────────┘           └───────────┘           └──────────┘
                                                                       │
                                                                       ▼
                                                                   SurrealDB
```

```go
func (s *IndexService) Index(ctx context.Context, req IndexRequest) (*IndexResult, error) {
    run := s.startRun(ctx, req)
    defer s.completeRun(ctx, run)

    // Stage 1: Walk files
    fileCh, errCh := s.walker.Walk(ctx, req.RepoPath, WalkOpts{
        Languages: req.Languages,
        Exclude:   req.Exclude,
        MaxFileKB: s.cfg.MaxFileKB,
    })

    // Stage 2: Parse (CPU-bound, N = GOMAXPROCS)
    parsedCh := make(chan *ParsedFile, 64)
    parseGroup, parseCtx := errgroup.WithContext(ctx)
    parseGroup.SetLimit(runtime.GOMAXPROCS(0))

    go func() {
        defer close(parsedCh)
        for file := range fileCh {
            file := file
            parseGroup.Go(func() error {
                parsed, err := s.parser.Parse(parseCtx, file)
                if err != nil {
                    s.log.Warn("parse failed", "file", file.Path, "err", err)
                    run.IncrErrors()
                    return nil  // non-fatal: skip file, continue
                }
                parsedCh <- parsed
                return nil
            })
        }
        parseGroup.Wait()
    }()

    // Stage 3: Build context + Embed (I/O-bound, M = 4 concurrent API calls)
    embedCh := make(chan *EmbeddedFile, 32)
    embedGroup, embedCtx := errgroup.WithContext(ctx)
    embedGroup.SetLimit(4) // Gemini API concurrency limit

    go func() {
        defer close(embedCh)
        batcher := NewEmbedBatcher(s.embedder, s.builder, 100) // batch size

        for parsed := range parsedCh {
            parsed := parsed
            embedGroup.Go(func() error {
                embedded, err := batcher.Process(embedCtx, parsed)
                if err != nil {
                    s.log.Warn("embed failed", "file", parsed.Path, "err", err)
                    run.IncrErrors()
                    return nil  // non-fatal
                }
                embedCh <- embedded
                return nil
            })
        }
        embedGroup.Wait()
    }()

    // Stage 4: Upsert to SurrealDB (I/O-bound, P = 8 concurrent DB writes)
    storeGroup, storeCtx := errgroup.WithContext(ctx)
    storeGroup.SetLimit(8)

    for embedded := range embedCh {
        embedded := embedded
        storeGroup.Go(func() error {
            if err := s.store.UpsertFileBatch(storeCtx, embedded.Nodes, embedded.Edges); err != nil {
                s.log.Error("upsert failed", "file", embedded.Path, "err", err)
                run.IncrErrors()
                return nil  // non-fatal
            }
            run.IncrUpserted(len(embedded.Nodes), len(embedded.Edges))
            return nil
        })
    }

    if err := storeGroup.Wait(); err != nil {
        return nil, fmt.Errorf("store stage: %w", err)
    }

    // Check for walker errors
    if err := <-errCh; err != nil {
        return nil, fmt.Errorf("walk stage: %w", err)
    }

    return run.Result(), nil
}
```

#### 4.1.2 Embed Batcher

The embed batcher accumulates nodes until the batch reaches the Gemini API limit
(100 inputs per request), then flushes in a single API call. This minimizes
round trips while respecting the API's batch constraint.

```go
// internal/app/embed_batcher.go

type EmbedBatcher struct {
    embedder  domain.Embedder
    builder   *ContextBuilder
    batchSize int
    mu        sync.Mutex
    pending   []domain.EmbedInput
}

func (b *EmbedBatcher) Process(ctx context.Context, pf *ParsedFile) (*EmbeddedFile, error) {
    inputs := make([]domain.EmbedInput, 0, len(pf.Nodes))

    for _, node := range pf.Nodes {
        text := b.builder.BuildContext(&node) // Construct embedding input text
        hash := sha256Hex(text)

        if node.ContentHash == hash && node.Embedding != nil {
            continue // Cache hit — skip re-embedding
        }

        inputs = append(inputs, domain.EmbedInput{
            ID:          node.ID,
            Text:        text,
            ContentHash: hash,
        })
    }

    if len(inputs) == 0 {
        return &EmbeddedFile{ParsedFile: pf, AllCached: true}, nil
    }

    // Batch in chunks of batchSize
    results := make(map[string][]float32)
    for i := 0; i < len(inputs); i += b.batchSize {
        end := min(i+b.batchSize, len(inputs))
        batch := inputs[i:end]

        res, err := b.embedder.EmbedBatch(ctx, batch)
        if err != nil {
            return nil, fmt.Errorf("embed batch: %w", err)
        }
        for _, r := range res {
            results[r.ID] = r.Vector
        }
    }

    // Apply embeddings to nodes
    for i := range pf.Nodes {
        if vec, ok := results[pf.Nodes[i].ID]; ok {
            pf.Nodes[i].Embedding = vec
            pf.Nodes[i].ContentHash = sha256Hex(b.builder.BuildContext(&pf.Nodes[i]))
        }
    }

    return &EmbeddedFile{ParsedFile: pf}, nil
}
```

### 4.2 QueryService — Hybrid Search + LLM Explanation

```go
// internal/app/query_service.go

type QueryService struct {
    embedder  domain.Embedder
    vectorIdx domain.VectorIndex
    textIdx   domain.TextIndex
    store     domain.GraphStore
    explainer domain.LLMExplainer
    cfg       *config.Config
}

func (s *QueryService) Query(ctx context.Context, req QueryRequest) (*types.QueryResult, error) {
    timer := NewTimer()

    // 1. Embed the user's question
    timer.Start("embed")
    queryVec, err := s.embedder.EmbedQuery(ctx, req.Question)
    if err != nil {
        return nil, fmt.Errorf("embed query: %w", err)
    }
    timer.Stop("embed")

    // 2. Parallel: vector search + full-text search
    timer.Start("search")
    var vectorHits, ftsHits []types.ScoredNode
    g, gCtx := errgroup.WithContext(ctx)

    g.Go(func() error {
        var err error
        vectorHits, err = s.vectorIdx.Search(gCtx, queryVec, domain.VectorSearchOpts{
            RepoSlug: req.RepoSlug,
            TopK:     req.TopK * 2, // Over-fetch for fusion
            MinScore: req.MinScore,
        })
        return err
    })

    g.Go(func() error {
        var err error
        ftsHits, err = s.textIdx.Search(gCtx, req.Question, domain.TextSearchOpts{
            RepoSlug: req.RepoSlug,
            TopK:     req.TopK * 2,
        })
        return err
    })

    if err := g.Wait(); err != nil {
        return nil, fmt.Errorf("search: %w", err)
    }
    timer.Stop("search")

    // 3. Reciprocal Rank Fusion (RRF)
    fused := ReciprocalRankFusion(vectorHits, ftsHits, RRFParams{
        K:               60,
        CentralityBoost: true,
    })

    // 4. Take top K
    if len(fused) > req.TopK {
        fused = fused[:req.TopK]
    }

    // 5. LLM explanation (streaming internally, collected for CLI)
    timer.Start("explain")
    explanation, err := s.explain(ctx, req.Question, fused)
    if err != nil {
        // Non-fatal: return results without explanation
        explanation = ""
    }
    timer.Stop("explain")

    return &types.QueryResult{
        Nodes:       fused,
        Explanation: explanation,
        Query:       req.Question,
        RepoSlug:    req.RepoSlug,
        Timing:      timer.Info(),
    }, nil
}
```

#### 4.2.1 Reciprocal Rank Fusion (RRF)

RRF combines ranked lists from different retrieval strategies without requiring
score normalization. It assigns each result a reciprocal rank score
`1/(k + rank)` from each list, then sums them. The `k` parameter (default 60)
controls how much weight is given to top-ranked results.

```go
// internal/app/fusion.go

type RRFParams struct {
    K               int     // Smoothing constant (default: 60)
    CentralityBoost bool    // Multiply by log(1 + centrality)
}

func ReciprocalRankFusion(
    vectorHits, ftsHits []types.ScoredNode,
    params RRFParams,
) []types.ScoredNode {
    scores := make(map[string]float64)
    nodes := make(map[string]types.ScoredNode)
    k := float64(params.K)

    // Score from vector ranking
    for rank, hit := range vectorHits {
        scores[hit.Node.ID] += 1.0 / (k + float64(rank+1))
        nodes[hit.Node.ID] = hit
    }

    // Score from FTS ranking
    for rank, hit := range ftsHits {
        scores[hit.Node.ID] += 1.0 / (k + float64(rank+1))
        if _, exists := nodes[hit.Node.ID]; !exists {
            nodes[hit.Node.ID] = hit
        }
    }

    // Centrality boost
    if params.CentralityBoost {
        for id, score := range scores {
            node := nodes[id]
            scores[id] = score * math.Log(1+float64(node.Centrality))
        }
    }

    // Sort by fused score descending
    result := make([]types.ScoredNode, 0, len(scores))
    for id, score := range scores {
        node := nodes[id]
        node.FusedScore = score
        result = append(result, node)
    }
    sort.Slice(result, func(i, j int) bool {
        return result[i].FusedScore > result[j].FusedScore
    })

    return result
}
```

### 4.3 TraceService — Call Chain Traversal

```go
// internal/app/trace_service.go

type TraceService struct {
    store     domain.GraphStore
    vectorIdx domain.VectorIndex
    embedder  domain.Embedder
    explainer domain.LLMExplainer
}

func (s *TraceService) Trace(ctx context.Context, req TraceRequest) (*types.TraceResult, error) {
    // 1. Resolve symbol — exact match first, then vector search fallback
    node, err := s.resolveSymbol(ctx, req.RepoSlug, req.Symbol)
    if err != nil {
        return nil, fmt.Errorf("symbol not found: %s", req.Symbol)
    }

    // 2. Graph traversal
    var hops []types.TraceHop
    if req.Reverse {
        hops, err = s.store.TraceReverse(ctx, node.ID, req.Depth)
    } else {
        hops, err = s.store.TraceForward(ctx, node.ID, req.Depth)
    }
    if err != nil {
        return nil, fmt.Errorf("trace: %w", err)
    }

    // 3. LLM explanation of the call chain
    explanation, _ := s.explainTrace(ctx, node, hops, req.Reverse)

    return &types.TraceResult{
        Root:        *node,
        Tree:        hops,
        Direction:   directionStr(req.Reverse),
        Explanation: explanation,
    }, nil
}

func (s *TraceService) resolveSymbol(ctx context.Context, repo, symbol string) (*types.CodeNode, error) {
    // Exact qualified name lookup
    node, err := s.store.GetNodeByQualified(ctx, repo, symbol)
    if err == nil {
        return node, nil
    }

    // Fuzzy fallback: embed the symbol name and vector search
    vec, err := s.embedder.EmbedQuery(ctx, symbol)
    if err != nil {
        return nil, err
    }

    hits, err := s.vectorIdx.Search(ctx, vec, domain.VectorSearchOpts{
        RepoSlug: repo,
        TopK:     1,
        MinScore: 0.8, // High threshold for symbol resolution
    })
    if err != nil || len(hits) == 0 {
        return nil, fmt.Errorf("could not resolve symbol: %s", symbol)
    }

    return &hits[0].Node, nil
}
```

### 4.4 BlastService — Impact Analysis

```go
// internal/app/blast_service.go

type BlastService struct {
    store     domain.GraphStore
    explainer domain.LLMExplainer
}

func (s *BlastService) Blast(ctx context.Context, req BlastRequest) (*types.BlastResult, error) {
    // 1. Resolve target symbol
    target, err := s.store.GetNodeByQualified(ctx, req.RepoSlug, req.Symbol)
    if err != nil {
        return nil, fmt.Errorf("symbol not found: %s", req.Symbol)
    }

    // 2. Reverse transitive traversal
    affected, err := s.store.BlastRadius(ctx, target.ID, req.MaxDepth)
    if err != nil {
        return nil, fmt.Errorf("blast radius: %w", err)
    }

    // 3. Deduplicate, group by module, sort by hop distance
    affected = deduplicateAndGroup(affected)

    // 4. LLM impact summary
    summary, _ := s.explainBlast(ctx, target, affected)

    return &types.BlastResult{
        Target:   *target,
        Affected: affected,
        Summary:  summary,
    }, nil
}
```

---

## 5. Driven Adapters

### 5.1 SurrealDB Adapter

The SurrealDB adapter implements three port interfaces (`GraphStore`,
`VectorIndex`, `TextIndex`) through a single connection, leveraging SurrealDB
3.0's unified query engine.

```go
// internal/adapters/surreal/client.go

type SurrealAdapter struct {
    db     *surrealdb.DB
    ns     string
    dbName string
    log    *slog.Logger
}

// Implements: domain.GraphStore, domain.VectorIndex, domain.TextIndex
var (
    _ domain.GraphStore  = (*SurrealAdapter)(nil)
    _ domain.VectorIndex = (*SurrealAdapter)(nil)
    _ domain.TextIndex   = (*SurrealAdapter)(nil)
)
```

#### 5.1.1 Connection Management

```go
func NewSurrealAdapter(cfg *config.SurrealConfig) (*SurrealAdapter, error) {
    db, err := surrealdb.New(cfg.URL)
    if err != nil {
        return nil, fmt.Errorf("connect surrealdb: %w", err)
    }

    if _, err := db.Signin(map[string]interface{}{
        "user": cfg.User,
        "pass": cfg.Pass,
    }); err != nil {
        return nil, fmt.Errorf("auth surrealdb: %w", err)
    }

    if _, err := db.Use(cfg.Namespace, cfg.Database); err != nil {
        return nil, fmt.Errorf("use ns/db: %w", err)
    }

    return &SurrealAdapter{db: db, ns: cfg.Namespace, dbName: cfg.Database}, nil
}

// Health check with automatic reconnect
func (a *SurrealAdapter) Ping(ctx context.Context) error {
    _, err := a.db.Query(ctx, "INFO FOR DB", nil)
    return err
}
```

#### 5.1.2 Transactional Batch Upsert

Uses SurrealDB 3.0 client-side transactions for atomic file ingestion:

```go
func (a *SurrealAdapter) UpsertFileBatch(
    ctx context.Context,
    nodes []types.CodeNode,
    edges []types.CodeEdge,
) error {
    tx, err := a.db.BeginTransaction(ctx)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Cancel(ctx)

    for _, node := range nodes {
        if _, err := tx.Query(ctx, upsertNodeQuery(node.Kind), nodeParams(node)); err != nil {
            return fmt.Errorf("upsert node %s: %w", node.ID, err)
        }
    }

    for _, edge := range edges {
        if _, err := tx.Query(ctx, relateEdgeQuery(edge.Kind), edgeParams(edge)); err != nil {
            return fmt.Errorf("relate edge %s->%s: %w", edge.FromID, edge.ToID, err)
        }
    }

    return tx.Commit(ctx)
}
```

#### 5.1.3 Hybrid Search (Vector + FTS in One Query)

```go
func (a *SurrealAdapter) HybridSearch(
    ctx context.Context,
    vec []float32,
    text string,
    opts HybridSearchOpts,
) ([]types.ScoredNode, error) {
    query := `
        LET $q_vec = $vec;
        LET $q_text = $text;

        SELECT
            *,
            vector::distance::knn() AS vec_dist,
            search::score(1) AS fts_score,
            centrality
        FROM function
        WHERE repo = type::record('repo', $repo)
          AND (
              embedding <|$topk, $effort|> $q_vec
              OR name @1@ $q_text
              OR qualified @1@ $q_text
              OR docstring @1@ $q_text
          )
        ORDER BY (1.0 / (60.0 + vector::distance::knn()))
               * math::log(1.0 + centrality) DESC
        LIMIT $limit;
    `

    result, err := a.db.Query(ctx, query, map[string]interface{}{
        "vec":    vec,
        "text":   text,
        "repo":   opts.RepoSlug,
        "topk":   opts.TopK * 2,
        "effort": opts.Effort,
        "limit":  opts.TopK,
    })
    if err != nil {
        return nil, err
    }

    return a.scanScoredNodes(result)
}
```

### 5.2 Gemini Adapter

Implements `Embedder` and `LLMExplainer` using the Google `genai` Go SDK.

```go
// internal/adapters/gemini/embedder.go

type GeminiAdapter struct {
    client     *genai.Client
    embedModel string
    llmModel   string
    dims       int
    log        *slog.Logger
}

// Implements: domain.Embedder, domain.LLMExplainer
var (
    _ domain.Embedder     = (*GeminiAdapter)(nil)
    _ domain.LLMExplainer = (*GeminiAdapter)(nil)
)
```

#### 5.2.1 Batch Embedding with Rate Limiting

```go
func (g *GeminiAdapter) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
    if len(inputs) > 100 {
        return nil, fmt.Errorf("batch size %d exceeds limit 100", len(inputs))
    }

    contents := make([]*genai.Content, len(inputs))
    for i, input := range inputs {
        parts := []genai.Part{
            genai.Text(input.Text),
        }
        // Attach images if present (multimodal embedding)
        for j, img := range input.Images {
            parts = append(parts, genai.Blob{
                MIMEType: input.ImageMIMEs[j],
                Data:     img,
            })
        }
        contents[i] = &genai.Content{Parts: parts}
    }

    // Retry with exponential backoff + jitter
    var result *genai.EmbedContentResponse
    err := retry.Do(ctx, retry.Config{
        MaxAttempts:  5,
        InitialDelay: 500 * time.Millisecond,
        MaxDelay:     30 * time.Second,
        Multiplier:   2.0,
        Jitter:       0.2,
        RetryOn:      isRetryableGeminiError,
    }, func() error {
        var err error
        result, err = g.client.Models.EmbedContent(ctx, g.embedModel,
            &genai.EmbedContentRequest{
                Contents:            contents,
                OutputDimensionality: ptr(g.dims),
            },
        )
        return err
    })
    if err != nil {
        return nil, fmt.Errorf("gemini embed: %w", err)
    }

    results := make([]domain.EmbedResult, len(inputs))
    for i, emb := range result.Embeddings {
        results[i] = domain.EmbedResult{
            ID:     inputs[i].ID,
            Vector: emb.Values,
        }
    }
    return results, nil
}

func (g *GeminiAdapter) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    input := domain.EmbedInput{
        Text: "task: search query | query: " + query,
    }
    results, err := g.EmbedBatch(ctx, []domain.EmbedInput{input})
    if err != nil {
        return nil, err
    }
    return results[0].Vector, nil
}
```

#### 5.2.2 Streaming LLM Explanation

```go
func (g *GeminiAdapter) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
    prompt := buildExplainPrompt(req)
    ch := make(chan domain.ExplainChunk, 16)

    go func() {
        defer close(ch)

        iter := g.client.Models.GenerateContentStream(ctx, g.llmModel,
            genai.Text(prompt),
            &genai.GenerateContentConfig{
                Temperature:     ptr(float32(0.3)),
                MaxOutputTokens: ptr(int32(2048)),
            },
        )

        for {
            resp, err := iter.Next()
            if err == iterator.Done {
                ch <- domain.ExplainChunk{Done: true}
                return
            }
            if err != nil {
                ch <- domain.ExplainChunk{Error: err, Done: true}
                return
            }
            for _, cand := range resp.Candidates {
                for _, part := range cand.Content.Parts {
                    if text, ok := part.(genai.Text); ok {
                        ch <- domain.ExplainChunk{Text: string(text)}
                    }
                }
            }
        }
    }()

    return ch, nil
}
```

### 5.3 Tree-Sitter Adapter

Implements the `Parser` interface using the `smacker/go-tree-sitter` CGO bindings.

```go
// internal/adapters/treesitter/parser.go

type TreeSitterAdapter struct {
    extractors map[string]LanguageExtractor  // keyed by language name
    log        *slog.Logger
}

var _ domain.Parser = (*TreeSitterAdapter)(nil)

// LanguageExtractor defines per-language AST extraction logic.
type LanguageExtractor interface {
    Language() *sitter.Language
    ExtractNodes(tree *sitter.Tree, source []byte, filePath string) []types.CodeNode
    ExtractEdges(tree *sitter.Tree, source []byte, nodes []types.CodeNode) []types.CodeEdge
}
```

#### 5.3.1 Language Extractor Interface

Each supported language (Go, Python, TypeScript, JavaScript) implements
`LanguageExtractor` with tree-sitter query patterns specific to that language's
grammar.

```go
// internal/adapters/treesitter/lang/golang.go

type GoExtractor struct{}

func (e *GoExtractor) Language() *sitter.Language {
    return golang.GetLanguage()
}

func (e *GoExtractor) ExtractNodes(tree *sitter.Tree, source []byte, filePath string) []types.CodeNode {
    var nodes []types.CodeNode
    root := tree.RootNode()

    // Query: function declarations
    fnQuery := `(function_declaration
        name: (identifier) @fn_name
        parameters: (parameter_list) @params
        result: (_)? @return_type
        body: (block) @body
    ) @fn`

    q, _ := sitter.NewQuery([]byte(fnQuery), golang.GetLanguage())
    cursor := sitter.NewQueryCursor()
    cursor.Exec(q, root)

    for {
        match, ok := cursor.NextMatch()
        if !ok { break }

        // Extract each capture into a CodeNode
        node := extractFunctionNode(match, source, filePath)
        nodes = append(nodes, node)
    }

    // Similarly: method_declaration, type_spec (struct/interface), import_spec
    // ...

    return nodes
}

func (e *GoExtractor) ExtractEdges(tree *sitter.Tree, source []byte, nodes []types.CodeNode) []types.CodeEdge {
    var edges []types.CodeEdge

    // Query: call expressions within function bodies
    callQuery := `(call_expression
        function: [
            (identifier) @callee
            (selector_expression
                operand: (_) @receiver
                field: (field_identifier) @method
            )
        ]
    ) @call`

    // For each function node, find call expressions within its body range
    // and create calls edges
    // ...

    return edges
}
```

#### 5.3.2 Type Resolution Pass

After initial tree-sitter extraction, a type-resolution pass improves call
graph accuracy for method calls, interface dispatch, and package-qualified
identifiers:

```go
// internal/adapters/treesitter/resolver.go

type TypeRegistry struct {
    types     map[string]TypeInfo     // qualified name → type info
    imports   map[string]string       // alias → package path
    methods   map[string][]string     // type → method names
}

func (r *TypeRegistry) ResolveCall(
    callerFile string,
    receiverExpr string,
    methodName string,
) (qualifiedCallee string, isDynamic bool) {
    // 1. Look up receiver type from local declarations
    // 2. If interface → mark as dynamic dispatch
    // 3. If concrete type → resolve to specific method
    // 4. If package-qualified → resolve via import map
    // ...
}
```

---

## 6. Context Builder

The context builder is the critical bridge between AST parsing and embedding.
It constructs the text that gets sent to Gemini Embedding 2, fusing the code
body with its graph neighborhood to create semantically rich embeddings.

```go
// internal/app/context_builder.go

type ContextBuilder struct {
    store domain.GraphStore   // For fetching graph neighbors
}

func (b *ContextBuilder) BuildContext(node *types.CodeNode) string {
    switch node.Kind {
    case types.NodeFunction:
        return b.buildFunctionContext(node)
    case types.NodeClass:
        return b.buildClassContext(node)
    case types.NodeFile:
        return b.buildFileContext(node)
    case types.NodeModule:
        return b.buildModuleContext(node)
    default:
        return node.Body
    }
}

func (b *ContextBuilder) buildFunctionContext(fn *types.CodeNode) string {
    var sb strings.Builder

    // Task prefix for asymmetric retrieval
    sb.WriteString("task: search result | query: [FUNCTION] ")
    sb.WriteString(fn.Qualified)
    sb.WriteString("\n")

    // Metadata
    fmt.Fprintf(&sb, "Language: %s  File: %s:%d-%d\n", fn.Language, fn.FilePath, fn.StartLine, fn.EndLine)
    fmt.Fprintf(&sb, "Signature: %s\n", fn.Signature)

    // Graph neighborhood (callers + callees)
    if callees := b.getCallees(fn.ID); len(callees) > 0 {
        fmt.Fprintf(&sb, "Calls: %s\n", strings.Join(callees, ", "))
    }
    if callers := b.getCallers(fn.ID); len(callers) > 0 {
        fmt.Fprintf(&sb, "Called by: %s\n", strings.Join(callers, ", "))
    }

    // Docstring
    if fn.Docstring != "" {
        fmt.Fprintf(&sb, "Doc: %s\n", fn.Docstring)
    }

    // Code body
    sb.WriteString("---\n")
    sb.WriteString(fn.Body)

    return sb.String()
}
```

**Why graph neighborhood matters for embeddings:** A function named `process()`
could be anything — but when the embedding input includes "Called by:
PaymentHandler.post" and "Calls: StripeClient.charge, OrderRepo.save", the
resulting vector captures not just what the code does but where it fits in the
system. A query for "payment processing" will rank this `process()` higher than
an identically-named function in an unrelated module.

---

## 7. HTTP API Layer (Echo)

The Echo HTTP server is a driving adapter that translates HTTP requests into
application service calls and streams responses back.

### 7.1 Server Bootstrap

```go
// internal/adapters/http/server.go

type Server struct {
    echo       *echo.Echo
    querySvc   *app.QueryService
    traceSvc   *app.TraceService
    blastSvc   *app.BlastService
    indexSvc   *app.IndexService
    repoSvc    *app.RepoService
    cfg        *config.ServerConfig
}

func NewServer(deps ServerDeps) *Server {
    e := echo.New()
    e.HideBanner = true

    // Middleware
    e.Use(middleware.RequestID())
    e.Use(middleware.Recover())
    e.Use(slogMiddleware(deps.Logger))
    e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
        AllowOrigins: deps.Cfg.CORSOrigins,
    }))

    s := &Server{echo: e, /* ... */}
    s.registerRoutes()
    return s
}

func (s *Server) registerRoutes() {
    v1 := s.echo.Group("/api/v1")

    // Repo management
    v1.GET("/repos", s.handleListRepos)

    // Indexing
    v1.POST("/index", s.handleStartIndex)
    v1.GET("/index/:job_id", s.handleIndexStatus)

    // Query
    v1.POST("/query", s.handleQuery)

    // Trace (SSE streaming)
    v1.POST("/trace", s.handleTrace)

    // Blast radius
    v1.POST("/blast-radius", s.handleBlastRadius)

    // Health
    s.echo.GET("/health", s.handleHealth)

    // Web UI (Phase 3 — embedded via go:embed)
    // s.echo.Static("/", "web/dist")
}
```

### 7.2 SSE Streaming for Trace & Query Explanations

Long-running operations (trace, blast, query with explanation) stream results
via Server-Sent Events (SSE). This allows the CLI, web UI, and IDE extensions
to show progressive output.

```go
// internal/adapters/http/handlers.go

func (s *Server) handleTrace(c echo.Context) error {
    var req TraceHTTPRequest
    if err := c.Bind(&req); err != nil {
        return echo.NewHTTPError(400, "invalid request")
    }

    // Set SSE headers
    c.Response().Header().Set("Content-Type", "text/event-stream")
    c.Response().Header().Set("Cache-Control", "no-cache")
    c.Response().Header().Set("Connection", "keep-alive")
    c.Response().WriteHeader(200)

    ctx := c.Request().Context()
    flusher, ok := c.Response().Writer.(http.Flusher)
    if !ok {
        return echo.NewHTTPError(500, "streaming not supported")
    }

    // Phase 1: Stream graph traversal results as they're discovered
    result, err := s.traceSvc.Trace(ctx, domain.TraceRequest{
        RepoSlug: req.Repo,
        Symbol:   req.Symbol,
        Depth:    req.Depth,
        Reverse:  req.Reverse,
    })
    if err != nil {
        writeSSE(c.Response(), "error", err.Error())
        flusher.Flush()
        return nil
    }

    // Send call tree nodes
    for _, hop := range result.Tree {
        writeSSE(c.Response(), "hop", hop)
        flusher.Flush()
    }

    // Phase 2: Stream LLM explanation
    explainCh, err := s.traceSvc.ExplainStream(ctx, result)
    if err == nil {
        for chunk := range explainCh {
            if chunk.Error != nil {
                writeSSE(c.Response(), "error", chunk.Error.Error())
                break
            }
            writeSSE(c.Response(), "explain", chunk.Text)
            flusher.Flush()
        }
    }

    writeSSE(c.Response(), "done", "")
    flusher.Flush()
    return nil
}

func writeSSE(w io.Writer, event string, data interface{}) {
    jsonData, _ := json.Marshal(data)
    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
}
```

### 7.3 Indexing Job Management

Indexing is a long-running operation. The HTTP API starts it asynchronously and
returns a job ID for polling.

```go
// internal/adapters/http/handlers_index.go

type indexJobStore struct {
    mu   sync.RWMutex
    jobs map[string]*IndexJob
}

type IndexJob struct {
    ID        string
    Status    string    // "indexing" | "completed" | "failed"
    Progress  float64   // 0.0 - 1.0
    Stats     IndexStats
    Error     string
    StartedAt time.Time
}

func (s *Server) handleStartIndex(c echo.Context) error {
    var req IndexHTTPRequest
    if err := c.Bind(&req); err != nil {
        return echo.NewHTTPError(400, "invalid request")
    }

    job := &IndexJob{
        ID:        ulid.Make().String(),
        Status:    "indexing",
        StartedAt: time.Now(),
    }
    s.jobs.Store(job.ID, job)

    // Run indexing in background goroutine
    go func() {
        result, err := s.indexSvc.Index(context.Background(), domain.IndexRequest{
            RepoPath:  req.RepoPath,
            Languages: req.Languages,
            Exclude:   req.Exclude,
        })
        if err != nil {
            job.Status = "failed"
            job.Error = err.Error()
            return
        }
        job.Status = "completed"
        job.Stats = result.Stats
        job.Progress = 1.0
    }()

    return c.JSON(202, map[string]interface{}{
        "job_id":  job.ID,
        "status":  "indexing",
        "progress": 0,
    })
}
```

---

## 8. CLI Layer

The CLI is the thinnest possible adapter — it parses flags, wires dependencies,
calls application services, and formats output.

### 8.1 Dependency Injection via Wire Function

```go
// cmd/wire.go

type App struct {
    IndexSvc   *app.IndexService
    QuerySvc   *app.QueryService
    TraceSvc   *app.TraceService
    BlastSvc   *app.BlastService
    RepoSvc    *app.RepoService
    Server     *http.Server
    SurrealDB  *surreal.SurrealAdapter
    Config     *config.Config
}

func WireApp(cfg *config.Config) (*App, error) {
    // 1. Driven adapters (infrastructure)
    surrealAdapter, err := surreal.NewSurrealAdapter(&cfg.Surreal)
    if err != nil {
        return nil, fmt.Errorf("surrealdb: %w", err)
    }

    geminiAdapter, err := gemini.NewGeminiAdapter(&cfg.Gemini)
    if err != nil {
        return nil, fmt.Errorf("gemini: %w", err)
    }

    tsAdapter := treesitter.NewTreeSitterAdapter(
        treesitter.WithGo(),
        treesitter.WithPython(),
        treesitter.WithTypeScript(),
        treesitter.WithJavaScript(),
    )

    walker := walker.NewFSWalker()
    contextBuilder := app.NewContextBuilder(surrealAdapter)

    // 2. Application services
    indexSvc := app.NewIndexService(walker, tsAdapter, geminiAdapter, surrealAdapter, cfg)
    querySvc := app.NewQueryService(geminiAdapter, surrealAdapter, surrealAdapter, surrealAdapter, geminiAdapter, cfg)
    traceSvc := app.NewTraceService(surrealAdapter, surrealAdapter, geminiAdapter, geminiAdapter)
    blastSvc := app.NewBlastService(surrealAdapter, geminiAdapter)

    // 3. Driving adapters
    server := httpAdapter.NewServer(httpAdapter.ServerDeps{
        QuerySvc: querySvc,
        TraceSvc: traceSvc,
        BlastSvc: blastSvc,
        IndexSvc: indexSvc,
        Cfg:      &cfg.Server,
    })

    return &App{
        IndexSvc:  indexSvc,
        QuerySvc:  querySvc,
        TraceSvc:  traceSvc,
        BlastSvc:  blastSvc,
        Server:    server,
        SurrealDB: surrealAdapter,
        Config:    cfg,
    }, nil
}
```

### 8.2 CLI Output Formatting

```go
// cmd/query.go

var queryCmd = &cobra.Command{
    Use:   "query [question]",
    Short: "Search the code graph with natural language",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        app, err := WireApp(loadConfig())
        if err != nil {
            return err
        }

        result, err := app.QuerySvc.Query(cmd.Context(), domain.QueryRequest{
            Question: args[0],
            RepoSlug: viper.GetString("repo"),
            TopK:     viper.GetInt("top-k"),
            MinScore: viper.GetFloat64("min-score"),
        })
        if err != nil {
            return err
        }

        // Format based on output flag
        switch viper.GetString("output") {
        case "json":
            return json.NewEncoder(os.Stdout).Encode(result)
        default:
            return renderQueryResult(result)
        }
    },
}

func renderQueryResult(r *types.QueryResult) error {
    // Explanation first (if available)
    if r.Explanation != "" {
        color.New(color.FgWhite).Println(r.Explanation)
        fmt.Println()
    }

    // Results table
    tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
    fmt.Fprintln(tw, "#\tScore\tSymbol\tFile\tLines")
    fmt.Fprintln(tw, "-\t-----\t------\t----\t-----")

    for i, node := range r.Nodes {
        fmt.Fprintf(tw, "%d\t%.3f\t%s\t%s\t%d-%d\n",
            i+1, node.FusedScore,
            node.Node.Qualified,
            node.Node.FilePath,
            node.Node.StartLine, node.Node.EndLine,
        )
    }
    tw.Flush()

    // Timing
    color.New(color.FgHiBlack).Printf(
        "\n⏱  embed=%dms search=%dms explain=%dms total=%dms\n",
        r.Timing.EmbedMS, r.Timing.SearchMS, r.Timing.ExplainMS, r.Timing.TotalMS,
    )

    return nil
}
```

---

## 9. SurrealDB Lifecycle Management

commit0 can manage a local SurrealDB instance for zero-config development.

```go
// internal/adapters/surrealdb/lifecycle.go

type DBManager struct {
    binaryPath string              // Path to surreal binary
    dataDir    string              // ~/.commit0/db
    port       int
    process    *os.Process
    log        *slog.Logger
}

func (m *DBManager) Start(ctx context.Context) error {
    // 1. Check if surreal binary exists; download if not
    if err := m.ensureBinary(ctx); err != nil {
        return err
    }

    // 2. Start surreal process
    cmd := exec.CommandContext(ctx,
        m.binaryPath, "start",
        "--bind", fmt.Sprintf("0.0.0.0:%d", m.port),
        "--user", "root",
        "--pass", "root",
        "--log", "warn",
        fmt.Sprintf("rocksdb:%s", m.dataDir),
    )
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Start(); err != nil {
        return fmt.Errorf("start surrealdb: %w", err)
    }
    m.process = cmd.Process

    // 3. Wait for ready (poll /health)
    return m.waitReady(ctx, 30*time.Second)
}

func (m *DBManager) Stop() error {
    if m.process != nil {
        return m.process.Signal(syscall.SIGTERM)
    }
    return nil
}

func (m *DBManager) Status() (string, error) {
    // Check if process is running and DB responds to ping
    // Returns "running", "stopped", or "unhealthy"
    // ...
}
```

---

## 10. Error Handling Strategy

### 10.1 Error Classification

```go
// internal/domain/errors.go

type ErrorKind int

const (
    ErrNotFound     ErrorKind = iota  // Symbol/repo not found
    ErrValidation                      // Invalid input
    ErrInfra                           // SurrealDB/Gemini/network failure
    ErrRateLimit                       // Gemini API rate limited
    ErrTimeout                         // Operation exceeded deadline
    ErrParseFailed                     // tree-sitter could not parse file
)

type DomainError struct {
    Kind    ErrorKind
    Message string
    Cause   error
    Retryable bool
}

func (e *DomainError) Error() string { return e.Message }
func (e *DomainError) Unwrap() error { return e.Cause }
```

### 10.2 Retry Policy

```go
// internal/infra/retry/retry.go

type Config struct {
    MaxAttempts  int
    InitialDelay time.Duration
    MaxDelay     time.Duration
    Multiplier   float64
    Jitter       float64           // 0.0 - 1.0
    RetryOn      func(error) bool
}

func Do(ctx context.Context, cfg Config, fn func() error) error {
    delay := cfg.InitialDelay

    for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
        err := fn()
        if err == nil {
            return nil
        }
        if !cfg.RetryOn(err) {
            return err
        }
        if attempt == cfg.MaxAttempts-1 {
            return fmt.Errorf("after %d attempts: %w", cfg.MaxAttempts, err)
        }

        // Exponential backoff with jitter
        jitter := delay.Seconds() * cfg.Jitter * (rand.Float64()*2 - 1)
        sleep := delay + time.Duration(jitter*float64(time.Second))
        if sleep > cfg.MaxDelay {
            sleep = cfg.MaxDelay
        }

        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(sleep):
        }

        delay = time.Duration(float64(delay) * cfg.Multiplier)
    }
    return nil
}
```

### 10.3 Graceful Degradation

The indexing pipeline treats individual file failures as non-fatal. A parse
error in one file does not abort the entire index run:

```
✓ Indexed 1,838 / 1,842 files (4 parse errors, 0 embed errors)
  Skipped: vendor/legacy.js (parse), test/fixtures/malformed.py (parse), ...
```

Query operations degrade gracefully when the LLM explainer is unavailable:
results are returned without the prose explanation, and the timing info shows
`explainMS: -1` to indicate the explanation was skipped.

---

## 11. Configuration Architecture

```go
// internal/config/config.go

type Config struct {
    Surreal  SurrealConfig  `mapstructure:"surreal"`
    Gemini   GeminiConfig   `mapstructure:"gemini"`
    Indexer  IndexerConfig  `mapstructure:"indexer"`
    Query    QueryConfig    `mapstructure:"query"`
    Server   ServerConfig   `mapstructure:"server"`
    DB       DBConfig       `mapstructure:"db"`
}

type SurrealConfig struct {
    URL       string `mapstructure:"url"       env:"COMMIT0_SURREAL_URL"       default:"ws://localhost:8000/rpc"`
    HTTP      string `mapstructure:"http"      env:"COMMIT0_SURREAL_HTTP"      default:"http://localhost:8000"`
    Namespace string `mapstructure:"namespace" env:"COMMIT0_SURREAL_NS"        default:"commit0"`
    Database  string `mapstructure:"database"  env:"COMMIT0_SURREAL_DB"        default:"codebase"`
    User      string `mapstructure:"user"      env:"COMMIT0_SURREAL_USER"      default:"root"`
    Pass      string `mapstructure:"pass"      env:"COMMIT0_SURREAL_PASS"      default:"root"`
    Strict    bool   `mapstructure:"strict"    env:"COMMIT0_SURREAL_STRICT"    default:"true"`
}

type GeminiConfig struct {
    APIKey     string `mapstructure:"api_key"     env:"COMMIT0_GEMINI_API_KEY"`
    EmbedModel string `mapstructure:"embed_model" env:"COMMIT0_GEMINI_EMBED_MODEL" default:"gemini-embedding-2-preview"`
    LLMModel   string `mapstructure:"llm_model"   env:"COMMIT0_GEMINI_LLM_MODEL"   default:"gemini-2.0-flash"`
    EmbedDims  int    `mapstructure:"embed_dims"  env:"COMMIT0_GEMINI_EMBED_DIMS"   default:"3072"`
    EmbedBatch int    `mapstructure:"embed_batch" env:"COMMIT0_GEMINI_EMBED_BATCH"  default:"100"`
}

type IndexerConfig struct {
    MaxFileKB int      `mapstructure:"max_file_kb" env:"COMMIT0_MAX_FILE_KB" default:"500"`
    Workers   int      `mapstructure:"workers"     env:"COMMIT0_WORKERS"     default:"0"`
    Exclude   []string `mapstructure:"exclude"     env:"COMMIT0_EXCLUDE"     default:"vendor,.git,node_modules,dist,__pycache__"`
    Languages []string `mapstructure:"languages"   env:"COMMIT0_LANGUAGES"   default:"go,python,typescript,javascript"`
}

type QueryConfig struct {
    TopK       int     `mapstructure:"top_k"      env:"COMMIT0_TOP_K"       default:"10"`
    TraceDepth int     `mapstructure:"trace_depth" env:"COMMIT0_TRACE_DEPTH" default:"8"`
    MinScore   float64 `mapstructure:"min_score"   env:"COMMIT0_MIN_SCORE"   default:"0.70"`
    HNSWEffort int     `mapstructure:"hnsw_effort" env:"COMMIT0_HNSW_EFFORT" default:"40"`
}

type ServerConfig struct {
    Port        int      `mapstructure:"port"        env:"COMMIT0_PORT"        default:"8080"`
    CORSOrigins []string `mapstructure:"cors_origins" env:"COMMIT0_CORS"        default:"*"`
}
```

**Configuration precedence** (highest wins):
1. CLI flags (`--port 9090`)
2. Environment variables (`COMMIT0_PORT=9090`)
3. Config file (`~/.commit0/config.yaml`)
4. Defaults (hardcoded in struct tags)

---

## 12. Updated Directory Layout

Refined from ARCHITECTURE.md to reflect the ports-and-adapters structure:

```
commit0/
├── main.go
├── go.mod / go.sum
├── .env.example
├── docs/
│   ├── ARCHITECTURE.md               # High-level vision
│   ├── DATABASE.md                    # SurrealDB 3.0 schema
│   └── BACKEND.md                     # This document
│
├── cmd/                               # CLI (driving adapter)
│   ├── root.go
│   ├── wire.go                        # Dependency injection
│   ├── index.go
│   ├── query.go
│   ├── trace.go
│   ├── blast.go
│   ├── serve.go
│   └── db.go
│
├── internal/
│   ├── domain/                        # Port interfaces + domain errors
│   │   ├── ports.go                   # GraphStore, Embedder, Parser, etc.
│   │   └── errors.go
│   │
│   ├── app/                           # Application services (orchestration)
│   │   ├── index_service.go
│   │   ├── query_service.go
│   │   ├── trace_service.go
│   │   ├── blast_service.go
│   │   ├── repo_service.go
│   │   ├── session_service.go
│   │   ├── context_builder.go         # Graph neighborhood → embedding text
│   │   ├── embed_batcher.go           # Batch accumulator for Gemini API
│   │   └── fusion.go                  # Reciprocal Rank Fusion
│   │
│   ├── adapters/                      # Driven adapters (infrastructure)
│   │   ├── surreal/                   # SurrealDB adapter
│   │   │   ├── client.go              # Connection, auth, reconnect
│   │   │   ├── schema.go              # ApplySchema from embedded surql
│   │   │   ├── graph_store.go         # GraphStore implementation
│   │   │   ├── vector_index.go        # VectorIndex implementation
│   │   │   ├── text_index.go          # TextIndex implementation
│   │   │   └── lifecycle.go           # Start/stop local SurrealDB
│   │   │
│   │   ├── gemini/                    # Gemini API adapter
│   │   │   ├── embedder.go            # Embedder implementation
│   │   │   └── explainer.go           # LLMExplainer implementation
│   │   │
│   │   ├── treesitter/                # tree-sitter adapter
│   │   │   ├── parser.go              # Parser implementation
│   │   │   ├── resolver.go            # Type resolution pass
│   │   │   └── lang/
│   │   │       ├── golang.go
│   │   │       ├── python.go
│   │   │       ├── typescript.go
│   │   │       └── javascript.go
│   │   │
│   │   ├── http/                      # Echo HTTP server (driving adapter)
│   │   │   ├── server.go
│   │   │   ├── middleware.go
│   │   │   └── handlers.go
│   │   │
│   │   └── walker/                    # Filesystem walker
│   │       └── fs_walker.go
│   │
│   ├── infra/                         # Cross-cutting infrastructure
│   │   └── retry/
│   │       └── retry.go               # Exponential backoff with jitter
│   │
│   └── config/
│       └── config.go
│
├── pkg/types/                         # Exported domain types
│   ├── ast.go                         # CodeNode, CodeEdge, NodeKind, EdgeKind
│   └── result.go                      # QueryResult, TraceResult, BlastResult
│
└── assets/
    └── schema.surql                   # SurrealDB 3.0 DDL (go:embed)
```

---

## 13. Testing Strategy

### 13.1 Unit Tests (Domain + App layer)

Domain logic and application services are tested with mock implementations of
port interfaces:

```go
// internal/app/query_service_test.go

func TestQueryService_HybridSearch(t *testing.T) {
    mockEmbedder := &mocks.MockEmbedder{
        EmbedQueryFn: func(ctx context.Context, q string) ([]float32, error) {
            return testVector, nil
        },
    }
    mockVector := &mocks.MockVectorIndex{
        SearchFn: func(ctx context.Context, q []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
            return vectorHits, nil
        },
    }
    mockFTS := &mocks.MockTextIndex{
        SearchFn: func(ctx context.Context, q string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
            return ftsHits, nil
        },
    }

    svc := app.NewQueryService(mockEmbedder, mockVector, mockFTS, nil, nil, testConfig)

    result, err := svc.Query(context.Background(), domain.QueryRequest{
        Question: "JWT validation",
        RepoSlug: "test-repo",
        TopK:     5,
    })

    assert.NoError(t, err)
    assert.Len(t, result.Nodes, 5)
    assert.True(t, result.Nodes[0].FusedScore >= result.Nodes[1].FusedScore)
}
```

### 13.2 Integration Tests (Adapter layer)

Integration tests run against real SurrealDB (via testcontainers) and validate
the full query path:

```go
func TestSurrealAdapter_HybridSearch(t *testing.T) {
    if testing.Short() {
        t.Skip("requires SurrealDB")
    }

    adapter := setupTestSurreal(t) // Starts SurrealDB in Docker
    defer adapter.Close()

    // Seed test data
    seedTestGraph(t, adapter)

    // Run hybrid search
    results, err := adapter.HybridSearch(ctx, testVector, "validate token", HybridSearchOpts{
        RepoSlug: "test",
        TopK:     5,
    })

    assert.NoError(t, err)
    assert.NotEmpty(t, results)
}
```

### 13.3 End-to-End Tests

Full pipeline tests that index a small Go repository and verify query results:

```go
func TestE2E_IndexAndQuery(t *testing.T) {
    app := wireTestApp(t)

    // Index the test fixture repo
    result, err := app.IndexSvc.Index(ctx, domain.IndexRequest{
        RepoPath:  "testdata/sample-repo",
        Languages: []string{"go"},
    })
    assert.NoError(t, err)
    assert.Greater(t, result.Stats.Functions, 0)

    // Query
    qResult, err := app.QuerySvc.Query(ctx, domain.QueryRequest{
        Question: "HTTP handler",
        RepoSlug: "sample-repo",
        TopK:     3,
    })
    assert.NoError(t, err)
    assert.NotEmpty(t, qResult.Nodes)
}
```

---

## 14. Observability

### 14.1 Structured Logging

All components use `log/slog` with consistent field names:

```go
slog.Info("index complete",
    "repo", req.RepoSlug,
    "files", result.Stats.Files,
    "functions", result.Stats.Functions,
    "edges", result.Stats.Edges,
    "duration_ms", result.Timing.TotalMS,
    "embed_cached", result.Stats.EmbeddingsCached,
)
```

### 14.2 Timing Breakdown

Every operation returns a `TimingInfo` struct so users can identify bottlenecks:

```
⏱  embed=45ms search=12ms graph=8ms explain=820ms total=885ms
```

### 14.3 Progress Reporting

The indexing pipeline reports real-time progress via a channel that drives
both the CLI progress bar and the HTTP job status endpoint:

```go
type ProgressEvent struct {
    Phase    string  // "walk" | "parse" | "embed" | "store"
    Current  int
    Total    int
    FilePath string  // Current file being processed
}
```
