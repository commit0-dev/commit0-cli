# Directory Layout

## Layers

```
server/cmd/              Server entry point, dependency injection
cli/cmd/                 CLI commands (HTTP clients)
server/internal/app/     Application services (composes port interfaces)
server/internal/domain/  Port interfaces and domain errors (no external imports)
server/internal/adapters SurrealDB, Gemini, OpenRouter, Voyage, Ollama, tree-sitter, HTTP
pkg/types/               Exported types shared between server, CLI, and SDK
server/assets/           Embedded files (schema.surql)
```

## Tree

```
commit0/
├── server/
│   ├── main.go
│   ├── cmd/
│   │   ├── root.go                      Global flags, config init
│   │   ├── wire.go                      Dependency injection
│   │   ├── serve.go                     HTTP server startup
│   │   └── db.go                        Local SurrealDB management
│   ├── internal/
│   │   ├── domain/
│   │   │   ├── open_code_graph.go       OpenCodeGraph interface
│   │   │   ├── ports.go                 Embedder, Parser, FileWalker, etc.
│   │   │   ├── ports_sync.go            P2P sync interfaces
│   │   │   ├── errors.go               Domain error types
│   │   │   ├── symbol_table.go          Cross-file name resolution
│   │   │   └── edge_linker.go           EdgeLinker interface
│   │   ├── app/
│   │   │   ├── index_service.go         Indexing pipeline
│   │   │   ├── index_tracker.go         Progress tracking
│   │   │   ├── query_service.go         Search and ranking
│   │   │   ├── trace_service.go         Call-chain traversal
│   │   │   ├── blast_service.go         Impact analysis
│   │   │   ├── repo_service.go          Repository CRUD
│   │   │   ├── field_flow_service.go    Data flow tracing
│   │   │   ├── rootcause_analysis_service.go  Root cause analysis
│   │   │   ├── analysis_service.go      Security analysis
│   │   │   ├── api_surface_service.go   Endpoint discovery
│   │   │   ├── sync_service.go          P2P graph sync
│   │   │   ├── context_builder.go       Embedding text construction
│   │   │   ├── embed_batcher.go         Batch embedding
│   │   │   ├── fusion.go               Reciprocal Rank Fusion
│   │   │   ├── agent/
│   │   │   │   ├── service.go           ADK agent runner
│   │   │   │   ├── delegate.go          Sub-agent delegation
│   │   │   │   ├── scratchpad.go        Evidence tracking
│   │   │   │   ├── tools.go             Agent tools
│   │   │   │   └── instructions.go      System prompts
│   │   │   ├── linkers/
│   │   │   │   ├── call_linker.go       Resolves calls edges
│   │   │   │   ├── dataflow_linker.go   Resolves data_flow edges
│   │   │   │   ├── defines_linker.go    Generates defines edges
│   │   │   │   ├── field_access_linker.go  Resolves reads/writes
│   │   │   │   └── route_linker.go      Resolves route targets
│   │   │   └── memory/
│   │   │       └── manager.go           Three-tier memory
│   │   ├── adapters/
│   │   │   ├── surreal/                 SurrealDB adapter
│   │   │   │   ├── client.go            Connection and pools
│   │   │   │   ├── open_code_graph.go   OpenCodeGraph bridge
│   │   │   │   ├── graph_store.go       CRUD, traversal, batch
│   │   │   │   ├── vector_index.go      HNSW search
│   │   │   │   ├── text_index.go        BM25 search
│   │   │   │   ├── schema.go            DDL and versioning
│   │   │   │   └── session_store.go     Chat persistence
│   │   │   ├── gemini/                  Gemini embedder + explainer
│   │   │   ├── openrouter/              OpenRouter LLM adapter
│   │   │   ├── voyage/                  Voyage AI embedder
│   │   │   ├── local/                   Ollama embedder + explainer
│   │   │   ├── treesitter/              tree-sitter parser (CGO)
│   │   │   │   ├── parser.go
│   │   │   │   └── lang/                Go, Python, TypeScript, JavaScript extractors
│   │   │   ├── http/                    Gin HTTP server
│   │   │   │   ├── server.go            Routes and middleware
│   │   │   │   ├── handlers.go          Request handlers
│   │   │   │   └── handlers_*.go        Domain-specific handlers
│   │   │   ├── client/                  CLI HTTP client (Resty v3)
│   │   │   ├── git/                     Git history adapter
│   │   │   ├── walker/                  File system walker
│   │   │   ├── sync/                    P2P sync codec and auth
│   │   │   ├── quic/                    QUIC transport
│   │   │   ├── consul/                  Consul discovery
│   │   │   └── mdns/                    mDNS discovery
│   │   ├── infra/retry/                 Exponential backoff
│   │   └── config/                      Configuration loading
│   └── assets/
│       └── schema.surql                 SurrealDB DDL
├── cli/
│   ├── main.go
│   └── cmd/                             CLI commands
│       ├── query.go  trace.go  blast.go  index.go
│       ├── flow.go  findroot.go
│       ├── repo.go  api.go  analyze.go
│       └── report.go
├── sdk/                                 Go SDK
├── pkg/types/
│   ├── ast.go                           CodeNode, CodeEdge
│   ├── graph.go                         GraphNode, GraphEdge
│   ├── result.go                        Query, Trace, Blast results
│   └── api.go                           API surface types
└── docs/
    ├── ARCHITECTURE.md
    ├── BACKEND.md
    ├── DATABASE.md
    ├── OPEN_CODE_GRAPH.md
    └── LAYOUT.md                        This file
```
