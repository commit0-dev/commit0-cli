---
name: commit0-go
description: Go coding patterns for commit0. TRIGGER when: writing Go code, implementing ports/adapters/services, scaffolding CLI commands, dependency injection, concurrency with errgroup, domain errors, or hexagonal architecture in this repo. DO NOT TRIGGER when: working with SurrealDB schema, tree-sitter, or Gemini API.
---

# commit0 Go Coding Skill

Use this skill when writing Go code for the commit0 project. This covers hexagonal architecture patterns, domain types, application services, CLI commands, dependency injection, concurrency patterns, error handling, and testing.

---

## Architecture: Ports and Adapters

commit0 uses strict hexagonal architecture. Every Go file you write must respect the dependency rule:

```
cmd/ → internal/app/ → internal/domain/ (ports.go, errors.go)
                     → pkg/types/ (exported types)
cmd/ → internal/adapters/* (only via wire.go DI)
internal/adapters/* → internal/domain/ (implement interfaces)
```

**NEVER** import adapter packages from domain or app layers. If you need to add a new external dependency, create a port interface first.

---

## Domain Types (pkg/types/)

All core data types live in `pkg/types/`. They have ZERO external imports.

### Node Types

```go
type NodeKind string

const (
    NodeFile     NodeKind = "file"
    NodeFunction NodeKind = "function"
    NodeClass    NodeKind = "class"
    NodeModule   NodeKind = "module"
)

type CodeNode struct {
    ID           string            // SurrealDB record ID (e.g., "function:pkg.Handler.ServeHTTP")
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
```

### Edge Types

```go
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
    FromID    string
    ToID      string
    CallSite  string              // "file.go:42"
    IsDynamic bool
    CallType  string              // direct | interface | callback | goroutine | deferred
    Metadata  map[string]string
}
```

### Result Types

```go
type QueryResult struct {
    Nodes       []ScoredNode
    Explanation string
    Query       string
    RepoSlug    string
    Timing      TimingInfo
}

type ScoredNode struct {
    Node         CodeNode
    VectorScore  float64
    FTSScore     float64
    FusedScore   float64
    Centrality   int
}

type TraceResult struct {
    Root         CodeNode
    Tree         []TraceHop
    Direction    string         // "forward" | "reverse"
    Explanation  string
    Timing       TimingInfo
}

type TraceHop struct {
    Depth    int
    Node     CodeNode
    Edge     CodeEdge
    Children []TraceHop
}

type BlastResult struct {
    Target       CodeNode
    Affected     []AffectedNode
    Summary      string
    Timing       TimingInfo
}

type AffectedNode struct {
    Node     CodeNode
    HopCount int
    Module   string
    Path     string
}

type TimingInfo struct {
    EmbedMS   int64
    SearchMS  int64
    GraphMS   int64
    ExplainMS int64
    TotalMS   int64
}
```

---

## Port Interfaces (internal/domain/ports.go)

7 port interfaces define the boundary between domain and infrastructure:

```go
// GraphStore — CRUD nodes/edges, graph traversal, transactions
type GraphStore interface {
    UpsertNode(ctx context.Context, node *types.CodeNode) error
    GetNode(ctx context.Context, id string) (*types.CodeNode, error)
    GetNodeByQualified(ctx context.Context, repo, qualified string) (*types.CodeNode, error)
    DeleteNode(ctx context.Context, id string) error
    DeleteNodesByRepo(ctx context.Context, repo string) error
    UpsertEdge(ctx context.Context, edge *types.CodeEdge) error
    DeleteEdgesForNode(ctx context.Context, nodeID string) error
    TraceForward(ctx context.Context, startID string, depth int) ([]types.TraceHop, error)
    TraceReverse(ctx context.Context, startID string, depth int) ([]types.TraceHop, error)
    BlastRadius(ctx context.Context, targetID string, maxDepth int) ([]types.AffectedNode, error)
    UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error
    UpsertRepo(ctx context.Context, repo *types.Repo) error
    GetRepo(ctx context.Context, slug string) (*types.Repo, error)
    ListRepos(ctx context.Context) ([]types.Repo, error)
    ApplySchema(ctx context.Context) error
    GetSchemaVersion(ctx context.Context) (int, error)
}

// VectorIndex — ANN search over embeddings (HNSW)
type VectorIndex interface {
    Search(ctx context.Context, query []float32, opts VectorSearchOpts) ([]types.ScoredNode, error)
}

// TextIndex — BM25 full-text search
type TextIndex interface {
    Search(ctx context.Context, query string, opts TextSearchOpts) ([]types.ScoredNode, error)
}

// Embedder — Text/code/image → vector (batch, cache)
type Embedder interface {
    EmbedBatch(ctx context.Context, inputs []EmbedInput) ([]EmbedResult, error)
    EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// LLMExplainer — Code context → streaming NL explanation
type LLMExplainer interface {
    Explain(ctx context.Context, req ExplainRequest) (<-chan ExplainChunk, error)
}

// Parser — Source file → AST nodes + edges
type Parser interface {
    Parse(ctx context.Context, file FileEntry) (*ParsedFile, error)
    SupportedLanguages() []string
}

// FileWalker — Repo path → file entries (.gitignore-aware)
type FileWalker interface {
    Walk(ctx context.Context, repoPath string, opts WalkOpts) (<-chan FileEntry, <-chan error)
}
```

---

## Application Services (internal/app/)

Each service takes port interfaces via constructor injection. Pattern:

```go
type IndexService struct {
    walker    domain.FileWalker
    parser    domain.Parser
    embedder  domain.Embedder
    store     domain.GraphStore
    builder   *ContextBuilder
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

### Service List

| Service | File | Responsibility |
|---|---|---|
| IndexService | index_service.go | Walk → parse → embed → store pipeline |
| QueryService | query_service.go | Embed → parallel search → RRF → explain |
| TraceService | trace_service.go | Symbol resolve → graph traverse → explain |
| BlastService | blast_service.go | Reverse transitive traversal → explain |
| RepoService | repo_service.go | Repository CRUD + lifecycle |
| SessionService | session_service.go | Multi-turn conversation context |

### Supporting Components

| Component | File | Purpose |
|---|---|---|
| ContextBuilder | context_builder.go | Construct embedding input text with task prefix |
| EmbedBatcher | embed_batcher.go | Batch up to 100 inputs per Gemini API call |
| RRF | fusion.go | Reciprocal Rank Fusion combining vector + FTS + centrality |

---

## Concurrency Patterns

### Bounded Worker Pool (standard pattern)

```go
g, gCtx := errgroup.WithContext(ctx)
g.SetLimit(N) // ALWAYS set a limit

for item := range inputCh {
    item := item // capture loop variable
    g.Go(func() error {
        result, err := process(gCtx, item)
        if err != nil {
            log.Warn("non-fatal", "err", err)
            return nil // non-fatal: log and continue
        }
        outputCh <- result
        return nil
    })
}

if err := g.Wait(); err != nil {
    return fmt.Errorf("stage failed: %w", err)
}
```

### Concurrency Limits

| Stage | Limit | Reason |
|---|---|---|
| Parse (tree-sitter) | `runtime.GOMAXPROCS(0)` | CPU-bound |
| Embed (Gemini API) | 4 | API rate limit |
| Store (SurrealDB) | 8 | DB write throughput |

### Pipeline Stage Template

```go
outputCh := make(chan *OutputType, bufferSize)
group, groupCtx := errgroup.WithContext(ctx)
group.SetLimit(concurrencyLimit)

go func() {
    defer close(outputCh)
    for input := range inputCh {
        input := input
        group.Go(func() error {
            // process...
            return nil
        })
    }
    group.Wait()
}()
```

---

## CLI Commands (cmd/)

Each CLI command file follows this pattern:

```go
// cmd/query.go
var queryCmd = &cobra.Command{
    Use:   "query <question>",
    Short: "Ask a natural language question about the codebase",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        svc, cleanup, err := wireQueryService(cmd)
        if err != nil {
            return err
        }
        defer cleanup()

        result, err := svc.Query(cmd.Context(), app.QueryRequest{
            Question: args[0],
            RepoSlug: viper.GetString("repo"),
            TopK:     viper.GetInt("top-k"),
        })
        if err != nil {
            return err
        }

        return renderQueryResult(cmd.OutOrStdout(), result)
    },
}
```

### Dependency Injection (cmd/wire.go)

```go
func wireQueryService(cmd *cobra.Command) (*app.QueryService, func(), error) {
    cfg := loadConfig()

    surreal, err := surreal.NewSurrealAdapter(&cfg.Surreal)
    if err != nil {
        return nil, nil, err
    }

    gemini, err := gemini.NewGeminiAdapter(&cfg.Gemini)
    if err != nil {
        return nil, nil, err
    }

    svc := app.NewQueryService(gemini, surreal, surreal, surreal, gemini, cfg)

    cleanup := func() {
        surreal.Close()
        gemini.Close()
    }

    return svc, cleanup, nil
}
```

---

## Error Handling

### Domain Errors (internal/domain/errors.go)

```go
type ErrorCode string

const (
    ErrNotFound   ErrorCode = "not_found"
    ErrRateLimit  ErrorCode = "rate_limit"
    ErrTimeout    ErrorCode = "timeout"
    ErrConflict   ErrorCode = "conflict"
    ErrValidation ErrorCode = "validation"
)

type DomainError struct {
    Code    ErrorCode
    Message string
    Cause   error
}

func (e *DomainError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *DomainError) Unwrap() error { return e.Cause }
```

### Error Wrapping Convention

- Adapters: wrap infrastructure errors into DomainError types
- App services: wrap with context: `fmt.Errorf("embed query: %w", err)`
- CLI: `return err` (Cobra handles display)

---

## Logging

Use `log/slog` (stdlib). Structured, zero-dependency.

```go
log := slog.Default().With("service", "index")
log.Info("indexing started", "repo", req.RepoSlug, "path", req.RepoPath)
log.Warn("parse failed", "file", file.Path, "err", err)
log.Error("upsert failed", "file", path, "err", err)
```

---

## Testing Patterns

### Unit Tests (domain/app layer)

Use in-memory stubs for port interfaces:

```go
type stubGraphStore struct {
    nodes map[string]*types.CodeNode
}

func (s *stubGraphStore) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
    n, ok := s.nodes[id]
    if !ok {
        return nil, &domain.DomainError{Code: domain.ErrNotFound}
    }
    return n, nil
}
```

### Compile-Time Interface Checks

Every adapter must include:

```go
var (
    _ domain.GraphStore  = (*SurrealAdapter)(nil)
    _ domain.VectorIndex = (*SurrealAdapter)(nil)
    _ domain.TextIndex   = (*SurrealAdapter)(nil)
)
```

### Integration Tests

Tag with `//go:build integration` and require running SurrealDB + API keys.

---

## Configuration (internal/config/config.go)

Viper-based, env-var driven with flag overrides:

```go
type Config struct {
    Surreal SurrealConfig
    Gemini  GeminiConfig
    Index   IndexConfig
    Query   QueryConfig
}

type SurrealConfig struct {
    URL       string `mapstructure:"SURREAL_URL"`       // ws://localhost:8000
    User      string `mapstructure:"SURREAL_USER"`      // root
    Pass      string `mapstructure:"SURREAL_PASS"`      // root
    Namespace string `mapstructure:"SURREAL_NAMESPACE"`  // commit0
    Database  string `mapstructure:"SURREAL_DATABASE"`   // codebase
}

type GeminiConfig struct {
    APIKey          string `mapstructure:"GEMINI_API_KEY"`
    EmbedModel      string `mapstructure:"GEMINI_EMBED_MODEL"`   // gemini-embedding-2-preview
    ExplainModel    string `mapstructure:"GEMINI_EXPLAIN_MODEL"` // gemini-2.0-flash
    EmbedDimension  int    `mapstructure:"GEMINI_EMBED_DIM"`     // 3072
    MaxBatchSize    int    `mapstructure:"GEMINI_BATCH_SIZE"`     // 100
}
```

---

## Retry Pattern (internal/infra/retry/)

```go
func WithRetry(ctx context.Context, maxAttempts int, fn func() error) error {
    for attempt := 0; attempt < maxAttempts; attempt++ {
        err := fn()
        if err == nil {
            return nil
        }
        if !isRetryable(err) {
            return err
        }
        jitter := time.Duration(rand.Intn(50)) * time.Millisecond
        time.Sleep(time.Duration(attempt*100)*time.Millisecond + jitter)
    }
    return fmt.Errorf("failed after %d attempts", maxAttempts)
}
```

---

## Checklist Before Writing Code

1. [ ] Does this code belong in domain, app, or adapter layer?
2. [ ] Are all external dependencies accessed through port interfaces?
3. [ ] Is concurrency bounded with `errgroup.SetLimit()`?
4. [ ] Are errors wrapped with context and using domain error types?
5. [ ] Is `context.Context` the first parameter on all public methods?
6. [ ] Are channels buffered appropriately and closed by the sender?
7. [ ] Is logging structured via `slog`?
8. [ ] Are compile-time interface checks included for adapters?
