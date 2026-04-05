# Directory Layout

Full annotated file tree for commit0. Load this when navigating unfamiliar areas of the codebase.

## Layer Summary

```
cmd/               → thin CLI; parse flags, wire.go for DI, delegate to services
internal/app/      → application services; only layer that composes multiple ports
internal/domain/   → port interfaces + domain errors; ZERO external imports
internal/adapters/ → SurrealDB, Gemini, tree-sitter, HTTP, walker adapters
pkg/types/         → exported types (CodeNode, CodeEdge, QueryResult, …)
assets/            → embedded static files (schema.surql)
```

## Full Tree

```
commit0/
├── main.go                          # package main — wires Cobra root command
├── cmd/                             # CLI driving adapter (thin)
│   ├── root.go                      # Global flags, config init
│   ├── wire.go                      # Dependency injection — wires adapters + services
│   ├── index.go                     # commit0 index <path|url>
│   ├── query.go                     # commit0 query "<question>"
│   ├── trace.go                     # commit0 trace <symbol>
│   ├── blast.go                     # commit0 blast <symbol>
│   ├── repo.go                      # commit0 repo list|add|rm
│   ├── serve.go                     # commit0 serve (HTTP)
│   └── db.go                        # commit0 db start|stop
├── internal/
│   ├── domain/                      # PORT INTERFACES + domain errors (no external deps)
│   │   ├── ports.go                 # GraphStore, VectorIndex, TextIndex, Embedder, LLMExplainer, Parser, FileWalker
│   │   └── errors.go                # DomainError types
│   ├── app/                         # APPLICATION SERVICES (orchestration)
│   │   ├── index_service.go         # Walk → parse → embed → store pipeline
│   │   ├── query_service.go         # Embed → parallel search → RRF → explain
│   │   ├── trace_service.go         # Symbol resolve → graph traverse → explain
│   │   ├── blast_service.go         # Reverse transitive traversal → explain
│   │   ├── repo_service.go          # Repository CRUD + lifecycle
│   │   ├── session_service.go       # Multi-turn conversation context
│   │   ├── context_builder.go       # Code + graph neighborhood → embedding text
│   │   ├── embed_batcher.go         # Batch 100/request to Gemini API
│   │   ├── fusion.go                # Reciprocal Rank Fusion
│   │   └── stubs_test.go            # In-memory port stubs for unit tests
│   ├── adapters/
│   │   ├── surreal/                 # SurrealDB 3.0 adapter (GraphStore + VectorIndex + TextIndex)
│   │   │   ├── client.go            # WebSocket conn, auth, reconnect
│   │   │   ├── schema.go            # ApplySchema() — embedded schema.surql
│   │   │   ├── graph_store.go       # → GraphStore
│   │   │   ├── vector_index.go      # → VectorIndex (HNSW)
│   │   │   ├── text_index.go        # → TextIndex (BM25)
│   │   │   └── lifecycle.go         # Start/stop local SurrealDB
│   │   ├── gemini/                  # Gemini API adapter
│   │   │   ├── embedder.go          # → Embedder (batch, retry, cache)
│   │   │   └── explainer.go         # → LLMExplainer (streaming)
│   │   ├── treesitter/              # tree-sitter adapter (CGO)
│   │   │   ├── parser.go            # → Parser
│   │   │   ├── resolver.go          # Type resolution (methods, interfaces)
│   │   │   └── lang/                # Per-language: golang.go, python.go, typescript.go, javascript.go
│   │   ├── http/                    # Echo HTTP server
│   │   │   ├── server.go            # Route registration, middleware
│   │   │   ├── middleware.go        # CORS, request ID, logging, recovery
│   │   │   └── handlers.go          # Request → Service → SSE/JSON
│   │   └── walker/
│   │       └── fs_walker.go         # → FileWalker (.gitignore-aware)
│   ├── infra/retry/retry.go         # Exponential backoff + jitter
│   └── config/config.go             # Typed config struct, Viper binding
├── pkg/types/                       # Exported types (potential SDK use)
│   ├── ast.go                       # CodeNode, CodeEdge, NodeKind, EdgeKind
│   └── result.go                    # QueryResult, TraceResult, BlastResult, TimingInfo
├── assets/
│   ├── assets.go                    # go:embed declarations
│   └── schema.surql                 # SurrealDB 3.0 DDL
└── docs/
    ├── ARCHITECTURE.md              # High-level vision, tech stack
    ├── BACKEND.md                   # Core backend architecture, services, adapters
    ├── DATABASE.md                  # SurrealDB 3.0 schema, indexes, queries
    └── LAYOUT.md                    # This file
```
