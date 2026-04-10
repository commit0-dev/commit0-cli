# Directory Layout

Full annotated file tree for commit0. Load this when navigating unfamiliar areas of the codebase.

## Layer Summary

```
cmd/               → thin CLI; parse flags, wire.go for DI, delegate to services
internal/app/      → application services; only layer that composes multiple ports
internal/domain/   → port interfaces + domain errors; ZERO external imports
internal/adapters/ → SurrealDB, Gemini, OpenRouter, Voyage, Ollama, tree-sitter, HTTP, walker adapters
pkg/types/         → exported types (CodeNode, CodeEdge, QueryResult, …)
assets/            → embedded static files (schema.surql)
```

## Full Tree

```
commit0/
├── main.go                          # package main — wires Cobra root command
├── cmd/                             # CLI driving adapter (thin → will become HTTP client)
│   ├── root.go                      # Global flags, config init
│   ├── wire.go                      # Dependency injection — wires adapters + services
│   ├── index.go                     # commit0 index <path|url>
│   ├── query.go                     # commit0 query "<question>"
│   ├── trace.go                     # commit0 trace <symbol>
│   ├── blast.go                     # commit0 blast <symbol>
│   ├── repo.go                      # commit0 repo list|add|rm
│   ├── api.go                       # commit0 api discover|spec
│   ├── serve.go                     # commit0 serve (HTTP server)
│   ├── db.go                        # commit0 db start|stop
│   ├── report.go                    # Report rendering (markdown → terminal)
│   ├── repo_source.go               # Git clone / URL resolution for index
│   └── color.go                     # Terminal color helpers
├── internal/
│   ├── domain/                      # PORT INTERFACES + domain errors (no external deps)
│   │   ├── ports.go                 # GraphStore, VectorIndex, TextIndex, Embedder,
│   │   │                            # LLMExplainer, Parser, FileWalker, AgentRunner,
│   │   │                            # MemoryStore, SessionStore, GitWalker
│   │   └── errors.go                # DomainError types (NotFound, Validation, Conflict, etc.)
│   ├── app/                         # APPLICATION SERVICES (orchestration)
│   │   ├── index_service.go         # Walk → parse → embed → store pipeline
│   │   ├── query_service.go         # Embed → parallel search → RRF → explain
│   │   ├── trace_service.go         # Symbol resolve → graph traverse → explain
│   │   ├── blast_service.go         # Reverse transitive traversal → explain
│   │   ├── repo_service.go          # Repository CRUD + lifecycle
│   │   ├── field_flow_service.go    # Field-level data flow tracing
│   │   ├── temporal_service.go      # Git history + temporal queries
│   │   ├── rootcause_analysis_service.go # End-to-end root cause analysis
│   │   ├── analysis_service.go      # Security analysis (taint, patterns)
│   │   ├── api_surface_service.go   # API endpoint discovery + OpenAPI gen
│   │   ├── review_service.go        # Code review analysis
│   │   ├── watcher_service.go       # File system watcher for incremental index
│   │   ├── summarizer.go            # Node summarization via LLM
│   │   ├── session_service.go       # Session persistence interface
│   │   ├── context_builder.go       # Code + graph neighborhood → embedding text
│   │   ├── embed_batcher.go         # Batch 100/request to embedding API
│   │   ├── fusion.go                # Reciprocal Rank Fusion
│   │   ├── stubs_test.go            # In-memory port stubs for unit tests
│   │   ├── agent/                   # ADK agent orchestration
│   │   │   ├── service.go           # AgentService — ADK runner + model.LLM injection
│   │   │   ├── delegate.go          # Sub-agent delegation with ModelFactory
│   │   │   ├── scratchpad.go        # Evidence scoring, convergence gates, cost budget
│   │   │   ├── scratchpad_tools.go  # ADK tools: update/read/check/plan/persist
│   │   │   ├── tools.go             # ADK analysis tools (search, trace, blast, flow, etc.)
│   │   │   └── instructions.go      # Analyst + sub-agent system prompts
│   │   └── memory/                  # 3-tier memory management
│   │       └── manager.go           # Working → session → persistent memory lifecycle
│   ├── adapters/
│   │   ├── surreal/                 # SurrealDB 3.0 adapter (unified)
│   │   │   ├── client.go            # WebSocket conn, auth, reconnect
│   │   │   ├── schema.go            # ApplySchema() — embedded schema.surql
│   │   │   ├── graph_store.go       # → GraphStore
│   │   │   ├── vector_index.go      # → VectorIndex (HNSW)
│   │   │   ├── text_index.go        # → TextIndex (BM25)
│   │   │   ├── field_flow_store.go  # → field flow graph queries
│   │   │   ├── session_store.go     # → SessionStore (chat persistence)
│   │   │   └── lifecycle.go         # Start/stop local SurrealDB
│   │   ├── gemini/                  # Gemini API adapter
│   │   │   ├── embedder.go          # → Embedder (batch, retry, cache)
│   │   │   └── explainer.go         # → LLMExplainer (streaming)
│   │   ├── openrouter/              # OpenRouter adapter (multi-model LLM gateway)
│   │   │   ├── client.go            # Resty v3 HTTP client + EventSource SSE + cost tracking
│   │   │   ├── model.go             # → ADK model.LLM interface adapter
│   │   │   ├── types.go             # OpenAI-compatible request/response structs
│   │   │   └── translate.go         # genai ↔ OpenAI content translation
│   │   ├── voyage/                  # Voyage AI embeddings adapter
│   │   │   └── embedder.go          # → Embedder (Resty v3 client)
│   │   ├── local/                   # Local Ollama adapters
│   │   │   ├── embedder.go          # → Embedder (Resty v3, /api/embed)
│   │   │   └── ollama.go            # → LLMExplainer (Resty v3, /api/chat)
│   │   ├── treesitter/              # tree-sitter adapter (CGO)
│   │   │   ├── parser.go            # → Parser
│   │   │   ├── resolver.go          # Type resolution (methods, interfaces)
│   │   │   └── lang/                # Per-language: golang.go, python.go, typescript.go, javascript.go
│   │   ├── http/                    # Gin HTTP server (driving adapter)
│   │   │   ├── server.go            # Route registration, middleware, Start/Shutdown
│   │   │   ├── middleware.go        # CORS (gin-contrib), RequestID, slog logging
│   │   │   ├── handlers.go          # REST: query, trace, blast, repos, nodes + SSE helper
│   │   │   ├── handlers_agent.go    # SSE: agent chat streaming
│   │   │   ├── handlers_index.go    # REST: async index with job tracking
│   │   │   ├── handlers_rootcause.go # REST: flow, history + SSE: find-root
│   │   │   └── handlers_test.go     # Handler tests with gin.CreateTestContext
│   │   ├── git/                     # Git adapter
│   │   │   └── walker.go            # → GitWalker (commit diff, blame)
│   │   └── walker/
│   │       └── fs_walker.go         # → FileWalker (.gitignore-aware)
│   ├── infra/retry/retry.go         # Exponential backoff + jitter (used by SurrealDB, Gemini SDK)
│   └── config/config.go             # Typed config struct, Viper binding, .env auto-discovery
├── pkg/types/                       # Exported types (potential SDK use)
│   ├── ast.go                       # CodeNode, CodeEdge, NodeKind, EdgeKind
│   ├── result.go                    # QueryResult, TraceResult, BlastResult, TimingInfo
│   └── api.go                       # APIEndpoint, APISurface, TaintFlow
├── assets/
│   ├── assets.go                    # go:embed declarations
│   └── schema.surql                 # SurrealDB 3.0 DDL
└── docs/
    ├── ARCHITECTURE.md              # High-level vision, tech stack
    ├── BACKEND.md                   # Core backend architecture, services, adapters
    ├── DATABASE.md                  # SurrealDB 3.0 schema, indexes, queries
    └── LAYOUT.md                    # This file
```
