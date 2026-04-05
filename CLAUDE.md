# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# commit0 — Claude Coding Guide

> Graph-based source code analyzer with conversational query and blast radius analysis.
> Single Go binary. SurrealDB 3.0 (graph + vector + FTS). Gemini Embedding 2. tree-sitter.

## Project Status

**Pre-implementation phase**: Architecture and domain design complete. No Go code yet. Start with `go mod init` and scaffold the directory layout per the Directory Layout section below.

## Quick Reference

- **Language:** Go 1.22+ (single static binary)
- **Architecture:** Ports-and-adapters (hexagonal)
- **Database:** SurrealDB 3.0 (HNSW vectors, graph traversal, BM25 FTS)
- **Embeddings:** Gemini Embedding 2 (`gemini-embedding-2-preview`, 3072-dim)
- **LLM:** Gemini 2.0 Flash (streaming explanations)
- **AST parsing:** tree-sitter via `smacker/go-tree-sitter` (CGO)
- **CLI:** Cobra + Viper
- **HTTP:** Echo v4
- **Concurrency:** `golang.org/x/sync/errgroup` bounded worker pools

## Directory Layout

```
commit0/
├── main.go                     # package main — wires Cobra root command
├── cmd/                        # CLI driving adapter (thin)
│   ├── root.go                 # Global flags, config init
│   ├── wire.go                 # Dependency injection — wires adapters + services
│   ├── index.go                # commit0 index <path|url>
│   ├── query.go                # commit0 query "<question>"
│   ├── trace.go                # commit0 trace <symbol>
│   ├── blast.go                # commit0 blast <symbol>
│   ├── serve.go                # commit0 serve (HTTP)
│   └── db.go                   # commit0 db start|stop
├── internal/
│   ├── domain/                 # PORT INTERFACES + domain errors (no external deps)
│   │   ├── ports.go            # GraphStore, VectorIndex, TextIndex, Embedder, LLMExplainer, Parser, FileWalker
│   │   └── errors.go           # DomainError types
│   ├── app/                    # APPLICATION SERVICES (orchestration)
│   │   ├── index_service.go    # Walk → parse → embed → store pipeline
│   │   ├── query_service.go    # Embed → parallel search → RRF → explain
│   │   ├── trace_service.go    # Symbol resolve → graph traverse → explain
│   │   ├── blast_service.go    # Reverse transitive traversal → explain
│   │   ├── repo_service.go     # Repository CRUD + lifecycle
│   │   ├── session_service.go  # Multi-turn conversation context
│   │   ├── context_builder.go  # Code + graph neighborhood → embedding text
│   │   ├── embed_batcher.go    # Batch 100/request to Gemini API
│   │   └── fusion.go           # Reciprocal Rank Fusion
│   ├── adapters/
│   │   ├── surreal/            # SurrealDB 3.0 adapter
│   │   │   ├── client.go       # WebSocket conn, auth, reconnect
│   │   │   ├── schema.go       # ApplySchema() — embedded schema.surql
│   │   │   ├── graph_store.go  # → GraphStore
│   │   │   ├── vector_index.go # → VectorIndex (HNSW)
│   │   │   ├── text_index.go   # → TextIndex (BM25)
│   │   │   └── lifecycle.go    # Start/stop local SurrealDB
│   │   ├── gemini/             # Gemini API adapter
│   │   │   ├── embedder.go     # → Embedder (batch, retry, cache)
│   │   │   └── explainer.go    # → LLMExplainer (streaming)
│   │   ├── treesitter/         # tree-sitter adapter (CGO)
│   │   │   ├── parser.go       # → Parser
│   │   │   ├── resolver.go     # Type resolution (methods, interfaces)
│   │   │   └── lang/           # Per-language: golang.go, python.go, typescript.go, javascript.go
│   │   ├── http/               # Echo HTTP server
│   │   │   ├── server.go       # Route registration, middleware
│   │   │   ├── middleware.go   # CORS, request ID, logging, recovery
│   │   │   └── handlers.go    # Request → Service → SSE/JSON
│   │   └── walker/
│   │       └── fs_walker.go    # → FileWalker (.gitignore-aware)
│   ├── infra/retry/retry.go    # Exponential backoff + jitter
│   └── config/config.go        # Typed config struct, Viper binding
├── pkg/types/                  # Exported types (potential SDK use)
│   ├── ast.go                  # CodeNode, CodeEdge, NodeKind, EdgeKind
│   └── result.go               # QueryResult, TraceResult, BlastResult, TimingInfo
├── assets/
│   └── schema.surql            # SurrealDB 3.0 DDL (embedded via go:embed)
└── docs/
    ├── ARCHITECTURE.md          # High-level vision, tech stack
    ├── BACKEND.md               # Core backend architecture, services, adapters
    └── DATABASE.md              # SurrealDB 3.0 schema, indexes, queries
```

## Architecture Rules

1. **Domain core has ZERO external imports.** `internal/domain/` and `pkg/types/` must never import SurrealDB, Gemini, or tree-sitter packages.
2. **All external access goes through port interfaces** defined in `internal/domain/ports.go`.
3. **Application services** in `internal/app/` compose ports — they are the only layer that combines multiple adapters.
4. **Adapters** in `internal/adapters/` implement exactly one or more port interfaces. The SurrealDB adapter implements GraphStore, VectorIndex, and TextIndex.
5. **CLI commands** in `cmd/` are thin — they parse flags, call `wire.go` for DI, and delegate to services.
6. **Errors** use domain error types from `internal/domain/errors.go`, not raw `fmt.Errorf` in domain/app layers.

## Key Patterns

- **Concurrency:** Use `errgroup.WithContext` + `SetLimit(N)` for bounded worker pools. Never unbounded goroutines.
- **Pipeline:** 4-stage Walk→Parse→Embed→Store with buffered channels between stages. Non-fatal errors skip the unit, log, and continue.
- **Embedding batching:** Accumulate up to 100 inputs, flush in one `EmbedBatch` API call. SHA-256 content hash for cache.
- **Hybrid search:** Vector ANN + BM25 FTS in parallel, fused via Reciprocal Rank Fusion with centrality boost.
- **Transactions:** SurrealDB 3.0 client-side transactions for atomic per-file upserts. Retry up to 3x on conflict.

## SurrealDB 3.0 Specifics

- Use `HNSW` not `MTREE` for vector indexes
- Use `COMPUTED` not `<future>` for derived fields
- Use `LET $var` not bare `$var = value`
- Use `type::record()` not `type::thing()`
- Use `rand::id()` not `rand::guid()`
- Use `FULLTEXT ANALYZER` not `SEARCH ANALYZER`
- Use `REFERENCE ON DELETE CASCADE` for containment relationships
- Use `RELATE` edges for relationships with metadata (calls, imports, inherits, uses)

## Gemini Embedding 2 Format

Task instruction prefix (not enum-based):
- Index time: `"task: search result | query: {content}"`
- Query time: `"task: search query | query: {user_question}"`
- SDK: `google.golang.org/genai` — `client.Models.EmbedContent(ctx, "gemini-embedding-2-preview", ...)`

## Go SDK References

- SurrealDB: `surrealdb/surrealdb.go/v2` (WebSocket + HTTP)
- Gemini: `google.golang.org/genai` (unified Google AI Go SDK)
- tree-sitter: `github.com/smacker/go-tree-sitter`

## Development Setup

Before implementing, ensure:
- Go 1.22+ installed
- `go mod init github.com/yourorg/commit0` (or appropriate module path)
- SurrealDB 3.0 available (local or remote)
- Gemini API key exported as `GEMINI_API_KEY`
- tree-sitter C libraries (installed via Homebrew on macOS: `brew install tree-sitter`)

Create `internal/domain/ports.go` first—it's the contract that all adapters implement.

## Custom Skills

Project-specific Claude Code skills are in `.claude/skills/`:
- `commit0-go` — Go conventions and module scaffolding
- `commit0-surrealdb` — Schema design patterns and SurrealDB 3.0 specifics
- `commit0-treesitter` — Parser integration and language-specific handling
- `commit0-gemini` — Embedding and LLM prompt patterns

Load these with `/read .claude/skills/<skill>/SKILL.md` for implementation guidance.

## Build & Run

```bash
# Not yet available — scaffold cmd/ first
go build -o commit0 .
./commit0 db start
./commit0 index ./my-project
./commit0 query "where is JWT validation?"
./commit0 trace pkg.Handler.ServeHTTP
./commit0 blast UserService.Create
./commit0 serve  # HTTP API on :8080
```

## Testing Strategy

- Domain/app layer: unit tests with in-memory stubs for all ports
- Adapters: integration tests requiring running SurrealDB + Gemini API key
- Use `_ domain.GraphStore = (*SurrealAdapter)(nil)` compile-time interface checks

## Further Reading

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — High-level vision and architecture diagram
- [docs/BACKEND.md](docs/BACKEND.md) — Service layer, adapter implementations, concurrency patterns
- [docs/DATABASE.md](docs/DATABASE.md) — SurrealDB 3.0 schema, indexes, and query patterns
