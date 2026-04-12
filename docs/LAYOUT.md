# Directory Layout

Full annotated file tree for commit0.

## Layer Summary

```
server/cmd/        → server entry point; wire.go for DI, serve.go for HTTP
cli/cmd/           → thin CLI HTTP client commands
server/internal/app/      → application services; only layer composing multiple ports
server/internal/domain/   → port interfaces + domain errors; ZERO external imports
server/internal/adapters/ → SurrealDB, Gemini, OpenRouter, Voyage, Ollama, tree-sitter, HTTP, walker
pkg/types/         → exported types (CodeNode, CodeEdge, QueryResult, ...)
server/assets/     → embedded static files (schema.surql)
```

## Full Tree

```
commit0/
├── server/
│   ├── main.go                          # Server entry point
│   ├── cmd/                             # Server CLI (Cobra)
│   │   ├── root.go                      # Global flags, config init
│   │   ├── wire.go                      # Dependency injection — wires adapters + services
│   │   ├── serve.go                     # commit0 serve (HTTP server)
│   │   └── db.go                        # commit0 db start|stop
│   ├── internal/
│   │   ├── domain/                      # PORT INTERFACES (no external deps)
│   │   │   ├── open_code_graph.go       # OpenCodeGraph — single graph port (node/edge/traversal/search)
│   │   │   ├── ports.go                 # Embedder, LLMExplainer, Parser, FileWalker, AgentRunner,
│   │   │   │                            # TemporalStore, MemoryStore, GitWalker, Compressor
│   │   │   ├── ports_sync.go            # P2P sync ports (GraphExporter, GraphImporter, PeerStore)
│   │   │   ├── errors.go               # DomainError types (NotFound, Validation, Conflict, etc.)
│   │   │   ├── symbol_table.go          # SymbolTable for cross-file edge resolution
│   │   │   └── edge_linker.go           # EdgeLinker interface + LinkStats
│   │   ├── app/                         # APPLICATION SERVICES
│   │   │   ├── index_service.go         # Walk → parse → link → embed → store pipeline
│   │   │   ├── index_tracker.go         # Thread-safe index progress tracking
│   │   │   ├── query_service.go         # Embed → parallel search → RRF → explain
│   │   │   ├── trace_service.go         # Symbol resolve → graph traverse → explain
│   │   │   ├── blast_service.go         # Reverse transitive traversal → explain
│   │   │   ├── repo_service.go          # Repository CRUD + lifecycle
│   │   │   ├── field_flow_service.go    # Field-level data flow tracing
│   │   │   ├── temporal_service.go      # Git history + temporal queries
│   │   │   ├── rootcause_analysis_service.go # End-to-end root cause analysis
│   │   │   ├── analysis_service.go      # Security analysis (taint, patterns)
│   │   │   ├── api_surface_service.go   # API endpoint discovery + OpenAPI gen
│   │   │   ├── review_service.go        # Code review analysis
│   │   │   ├── sync_service.go          # P2P graph sync
│   │   │   ├── context_builder.go       # Code + graph neighborhood → embedding text
│   │   │   ├── embed_batcher.go         # Batch 100/request to embedding API
│   │   │   ├── fusion.go               # Reciprocal Rank Fusion
│   │   │   ├── summarizer.go            # Node summarization via LLM
│   │   │   ├── session_service.go       # Session persistence interface
│   │   │   ├── stubs_test.go            # In-memory port stubs for unit tests
│   │   │   ├── agent/                   # ADK agent orchestration
│   │   │   │   ├── service.go           # AgentService — ADK runner + model.LLM injection
│   │   │   │   ├── delegate.go          # Sub-agent delegation with ModelFactory
│   │   │   │   ├── scratchpad.go        # Evidence scoring, convergence gates, cost budget
│   │   │   │   ├── scratchpad_tools.go  # ADK tools: update/read/check/plan/persist
│   │   │   │   ├── tools.go            # ADK analysis tools (search, trace, blast, flow, etc.)
│   │   │   │   └── instructions.go      # Analyst + sub-agent system prompts
│   │   │   ├── linkers/                 # EdgeLinker implementations (cross-file resolution)
│   │   │   │   ├── call_linker.go       # Resolves calls edges via SymbolTable
│   │   │   │   ├── dataflow_linker.go   # Resolves data_flow edges
│   │   │   │   ├── defines_linker.go    # Generates file→fn, class→method defines edges
│   │   │   │   ├── field_access_linker.go # Resolves reads/writes with receiver inference
│   │   │   │   └── route_linker.go      # Resolves route handler targets
│   │   │   └── memory/                  # 3-tier memory management
│   │   │       └── manager.go           # Working → session → persistent memory lifecycle
│   │   ├── adapters/
│   │   │   ├── surreal/                 # SurrealDB 3.0 adapter
│   │   │   │   ├── client.go            # WebSocket conn, auth, connection pools
│   │   │   │   ├── open_code_graph.go   # OpenCodeGraph interface bridge
│   │   │   │   ├── schema.go            # ApplySchema() — embedded schema.surql
│   │   │   │   ├── graph_store.go       # Node/edge CRUD, traversal, batch, neighborhood
│   │   │   │   ├── vector_index.go      # HNSW ANN search
│   │   │   │   ├── text_index.go        # BM25 full-text search
│   │   │   │   ├── field_flow_store.go  # Data flow traversal
│   │   │   │   ├── session_store.go     # Chat session persistence
│   │   │   │   ├── conn_pool.go         # Read/write connection pools
│   │   │   │   └── lifecycle.go         # Start/stop local SurrealDB
│   │   │   ├── gemini/                  # Gemini API adapter
│   │   │   │   ├── client.go            # Shared genai client
│   │   │   │   ├── embedder.go          # Embedder (batch, retry, cache)
│   │   │   │   └── explainer.go         # LLMExplainer (streaming)
│   │   │   ├── openrouter/              # OpenRouter adapter (multi-model LLM)
│   │   │   │   ├── client.go            # Resty v3 HTTP client + EventSource SSE
│   │   │   │   ├── model.go             # ADK model.LLM interface adapter
│   │   │   │   ├── types.go             # OpenAI-compatible request/response
│   │   │   │   └── translate.go         # genai ↔ OpenAI content translation
│   │   │   ├── voyage/                  # Voyage AI embeddings adapter
│   │   │   │   └── embedder.go          # Embedder (Resty v3)
│   │   │   ├── local/                   # Local Ollama adapters
│   │   │   │   ├── embedder.go          # Embedder (Resty v3, /api/embed)
│   │   │   │   └── ollama.go            # LLMExplainer (Resty v3, /api/chat)
│   │   │   ├── treesitter/              # tree-sitter adapter (CGO)
│   │   │   │   ├── parser.go            # Parser interface
│   │   │   │   └── lang/                # Per-language extractors
│   │   │   ├── http/                    # Gin HTTP server (driving adapter)
│   │   │   │   ├── server.go            # Route registration, middleware, Start/Shutdown
│   │   │   │   ├── middleware.go         # CORS, RequestID, slog logging
│   │   │   │   ├── handlers.go          # REST: query, trace, blast, repos, nodes
│   │   │   │   ├── handlers_agent.go    # SSE: agent chat streaming
│   │   │   │   ├── handlers_index.go    # REST: async index with job tracking
│   │   │   │   └── handlers_rootcause.go # REST: flow, history + SSE: find-root
│   │   │   ├── client/                  # CLI HTTP client (Resty v3)
│   │   │   │   └── client.go            # Server API client
│   │   │   ├── git/walker.go            # GitWalker (commit diff, file at commit)
│   │   │   ├── walker/fs_walker.go      # FileWalker (.gitignore-aware)
│   │   │   ├── sync/                    # P2P sync (CBOR codec, passphrase auth)
│   │   │   ├── quic/                    # QUIC transport for P2P data plane
│   │   │   ├── consul/                  # Consul service discovery
│   │   │   └── mdns/                    # mDNS LAN discovery
│   │   ├── infra/retry/retry.go         # Exponential backoff + jitter
│   │   └── config/config.go             # Typed config, Viper binding, .env auto-discovery
│   └── assets/
│       ├── assets.go                    # go:embed declarations
│       └── schema.surql                 # SurrealDB DDL
├── cli/
│   ├── main.go                          # CLI entry point
│   └── cmd/                             # CLI commands (thin HTTP clients)
│       ├── root.go                      # Global flags
│       ├── query.go                     # commit0-cli query
│       ├── trace.go                     # commit0-cli trace
│       ├── blast.go                     # commit0-cli blast
│       ├── index.go                     # commit0-cli index
│       ├── repo.go                      # commit0-cli repo
│       ├── flow.go                      # commit0-cli flow
│       ├── history.go                   # commit0-cli history
│       ├── findroot.go                  # commit0-cli find-root
│       ├── api.go                       # commit0-cli api
│       ├── analyze.go                   # commit0-cli analyze
│       └── report.go                    # Report rendering (markdown → terminal)
├── sdk/                                 # Go SDK (HTTP client library)
├── pkg/types/                           # Exported types
│   ├── ast.go                           # CodeNode, CodeEdge, NodeKind, EdgeKind
│   ├── graph.go                         # GraphNode, GraphEdge (label + Props)
│   ├── result.go                        # QueryResult, TraceResult, BlastResult
│   ├── api.go                           # APIEndpoint, APISurface, TaintFlow
│   └── index_progress.go               # IndexProgress, PipelineCoverage
└── docs/
    ├── ARCHITECTURE.md                  # High-level design, tech stack
    ├── BACKEND.md                       # Services, adapters, HTTP API
    ├── DATABASE.md                      # SurrealDB schema, indexes, queries
    ├── OPEN_CODE_GRAPH.md               # Unified graph abstraction
    └── LAYOUT.md                        # This file
```
