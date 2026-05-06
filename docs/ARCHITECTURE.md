# Architecture

**See also:** [BACKEND.md](BACKEND.md) · [DATABASE.md](DATABASE.md) · [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md) · [LAYOUT.md](LAYOUT.md)

---

## 1. Overview

commit0 parses source code into a graph where functions, classes, and files are nodes, and calls, imports, and data flows are edges. Each node carries a vector embedding for semantic search. The system exposes this graph through an HTTP API that supports natural-language queries, call-chain traversal, and impact analysis.

The server is a single Go binary. The CLI is a separate binary that communicates with the server over HTTP.

---

## 2. Client-Server Model

The server owns all adapters and state. Clients communicate via HTTP with JSON request-response and Server-Sent Events (SSE) for streaming.

```
  CLI (Go)       Web (React)     VSCode (TS)
    │                │               │
    └────────────────┴──────────���────┘
                          │
                   Streamable HTTP
                  POST + SSE (JSON)
                          │
              ┌───────────┴───────────┐
              │    commit0 server     │
              │                       │
              │  ┌─────────────────┐  │
              │  │  Application    │  │
              │  │  Services       │  │
              │  └────────┬────────┘  │
              │           │           │
              │  ┌────────┴────────┐  │
              │  │ Driven Adapters │  │
              │  └─────────────────┘  │
              └───────────────────────┘
```

| Pattern | Transport | Used by |
|---------|-----------|---------|
| Request-response | POST, JSON body | query, trace, blast, repo CRUD |
| SSE streaming | POST, `text/event-stream` | agent chat, find-root |
| Async polling | POST (start) + GET (poll) | index |

---

## 3. Hexagonal Architecture

The codebase follows a ports-and-adapters pattern. Domain logic depends only on Go interfaces defined in `internal/domain/`. External systems are accessed through adapter implementations.

| Layer | Location | Rule |
|-------|----------|------|
| Domain | `server/internal/domain/`, `pkg/types/` | No external imports. Defines interfaces and types. |
| Application | `server/internal/app/` | Composes interfaces. Does not import adapters. |
| Driven adapters | `server/internal/adapters/surreal/`, `gemini/`, etc. | Implement domain interfaces. |
| Driving adapters | `server/internal/adapters/http/` (Gin) | Translate HTTP requests to service calls. |

> The CLI lives in the standalone [`commit0-cli`](https://github.com/commit0-dev/commit0-cli) repo. It is a pure-Go Resty v3 HTTP client and never imports server internals.

### Port Interfaces

| Interface | Purpose | Implementations |
|-----------|---------|----------------|
| `OpenCodeGraph` | Graph CRUD, traversal, vector/text search | SurrealDB 3.0 |
| `Embedder` | Text/code to vector embeddings | Gemini, Voyage AI, Ollama |
| `LLMExplainer` | Natural-language explanation generation | Gemini, Ollama |
| `AgentRunner` | Multi-turn agent conversations | CloudWeGo Eino |
| `Parser` | Source file to AST nodes and edges | tree-sitter (CGO) |
| `FileWalker` | Repository file enumeration | OS filesystem |
| `MemoryStore` | Persistent memory with vector retrieval | SurrealDB |
| `GitWalker` | Git history access | git CLI |

`OpenCodeGraph` consolidates all graph operations (node/edge CRUD, traversal, search, listing) into a single interface. See [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md).

---

## 4. Technology Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.26+ (CGO for tree-sitter) |
| CLI framework | Cobra + Viper |
| HTTP server | Gin |
| HTTP clients | Resty v3 |
| AST parsing | go-tree-sitter |
| Database | SurrealDB 3.0 |
| Embeddings | Gemini, Voyage AI, or Ollama |
| LLM | Gemini, OpenRouter, or Ollama |
| Agent framework | CloudWeGo Eino v0.8 (with `SubRunnerFactory` for sub-agent isolation) |
| Logging | log/slog (stdlib) |

---

## 5. Embedding Providers

Selected by the `EMBED_PROVIDER` environment variable. All providers normalize output to the dimension specified by `EMBED_DIM` (default 1024).

| Provider | Model | Native dimensions |
|----------|-------|-------------------|
| `gemini` | gemini-embedding-2-preview | 3072 |
| `voyage` | voyage-code-3 | 1024 |
| `ollama` | configurable | varies |

---

## 6. Graph Data Model

The graph stores four node types (`function`, `class`, `file`, `module`) and thirteen edge types (`calls`, `imports`, `defines`, `inherits`, `uses`, `data_flow`, `reads`, `writes`, `route`, `control_flow`, `data_dep`, and others).

Node and edge types use string labels and extensible property maps rather than fixed Go enums. Node tables are SCHEMAFULL (required for HNSW indexes). Edge tables are SCHEMALESS, allowing new relationship types without schema changes.

Traversal is label-parameterized: the caller specifies which edge labels to follow. Trace, blast, flow, and security analysis all use the same traversal API with different label sets.

See [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md) and [DATABASE.md](DATABASE.md).

---

## 7. Design Decisions

**Ports and adapters.** SurrealDB, Gemini, and tree-sitter each have distinct failure modes and upgrade cycles. Isolating them behind interfaces enables independent testing and replacement.

**SurrealDB as unified store.** Graph traversal, HNSW vector search, and BM25 full-text search run in a single database, avoiding the operational complexity of separate graph, vector, and search systems.

**OpenCodeGraph.** A single interface for all graph operations. Traversal is parameterized by edge labels rather than encoded as separate methods per analysis technique. New techniques register an EdgeLinker without modifying the interface, adapter, or services.

**Streamable HTTP.** JSON request-response for synchronous operations, SSE for streaming. Compatible with browser clients and standard HTTP tooling.

**Multi-provider.** Embedding and LLM providers are independently configurable. The agent uses a ModelFactory abstraction to avoid coupling to any specific provider.

**Two-phase indexing.** Files are parsed in parallel (Phase 1), then a global SymbolTable is built and cross-file edges are resolved sequentially (Phase 2), then embeddings are computed and stored in parallel (Phase 3). This avoids database round-trips during parsing while enabling cross-file resolution.
