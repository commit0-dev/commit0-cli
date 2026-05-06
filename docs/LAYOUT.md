# Directory Layout

## Layers

```
server/cmd/              Server entry point, dependency injection
server/internal/app/     Application services (composes port interfaces)
server/internal/domain/  Port interfaces and domain errors (no external imports)
server/internal/adapters SurrealDB, Gemini, OpenRouter, Voyage, Unsloth, Ollama,
                         Eino, tree-sitter, Gin HTTP, QUIC, mDNS, Consul
pkg/types/               Exported types shared with the standalone CLI
server/assets/           Embedded files (schema.surql)
```

> The CLI lives in [`commit0-cli`](https://github.com/commit0-dev/commit0-cli) (pure Go, no CGO). It depends only on `pkg/types` (republished as a standalone module) and Resty v3.

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
│   │   │   │   ├── service.go           Eino agent runner
│   │   │   │   ├── delegate.go          Sub-agent delegation (SubRunnerFactory)
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
│   │   │   ├── voyage/                  Voyage AI embedder
│   │   │   ├── unsloth/                 Unsloth embedder + LLM (vLLM-compatible)
│   │   │   ├── local/                   Ollama embedder + explainer
│   │   │   ├── eino/                    CloudWeGo Eino agent runner + factory
│   │   │   ├── treesitter/              tree-sitter parser (CGO)
│   │   │   │   ├── parser.go
│   │   │   │   └── lang/                Go, Python, TypeScript, JavaScript extractors
│   │   │   ├── http/                    Gin HTTP server
│   │   │   │   ├── server.go            Routes and middleware
│   │   │   │   ├── handlers.go          Request handlers
│   │   │   │   └── handlers_*.go        Domain-specific handlers
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
