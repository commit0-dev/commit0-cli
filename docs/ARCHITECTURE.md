# commit0 — Architecture

> Graph-based source code analyzer. Single Go binary. Streamable HTTP client-server architecture.

**See also:** [BACKEND.md](BACKEND.md) (services, adapters, API) · [DATABASE.md](DATABASE.md) (schema, indexes) · [PIPELINE.md](PIPELINE.md) (indexing, retrieval) · [LAYOUT.md](LAYOUT.md) (file tree)

---

## 1. Vision

commit0 indexes any codebase into a knowledge graph — every function, class, and file is a node, every call/import/inheritance is an edge, and every entity carries a dense embedding. Users query in plain English, trace call chains, and analyze blast radius.

```
curl -fsSL https://install.commit0.dev | sh
commit0 db start
commit0 serve &
commit0 index https://github.com/owner/repo
commit0 query "where is the JWT middleware?"
commit0 trace auth.ValidateToken --direction forward
commit0 blast UserService.Create --max-depth 5
```

### Single Binary Philosophy

One binary, no runtime deps, no Python, no Docker. SurrealDB is the only external dependency (commit0 can manage it locally via `commit0 db start`).

---

## 2. Client-Server Architecture

Streamable HTTP (POST + SSE), aligned with MCP specification patterns. The server owns all adapters and services. CLI commands are thin HTTP clients.

```
  CLI (Go)       Web (React)     VSCode (TS)      Mobile
    │                │               │               │
    └────────────────┴───────────────┴───────────────┘
                          │
                 ┌────────┴────────┐
                 │  Streamable HTTP │  POST + SSE (JSON)
                 └────────┬────────┘
                          │
              ┌───────────┴───────────┐
              │    commit0 server     │
              │                       │
              │  ┌─────────────────┐  │
              │  │ Application     │  │
              │  │ Services        │  │  Index, Query, Trace, Blast, Repo,
              │  │ (internal/app/) │  │  Agent, FieldFlow, Temporal, RootCause
              │  └────────┬────────┘  │
              │           │           │
              │  ┌────────┴────────┐  │
              │  │ Driven Adapters │  │  SurrealDB, Gemini, OpenRouter,
              │  │ (adapters/*)    │  │  Voyage, Ollama, tree-sitter
              │  └─────────────────┘  │
              └───────────────────────┘
```

### Communication Patterns

| Pattern | Transport | Used By |
|---------|-----------|---------|
| Request-response | POST → JSON | query, trace, blast, repo CRUD, api |
| SSE streaming | POST → `text/event-stream` | agent chat, trace (hop-by-hop), find-root |
| Async polling | POST (start) → GET (poll) | index (long-running) |

---

## 3. Hexagonal Architecture

Ports-and-adapters pattern. Domain logic has zero knowledge of SurrealDB, Gemini, or tree-sitter — those are swappable adapters behind port interfaces.

| Layer | Location | Rule |
|-------|----------|------|
| Domain Core | `internal/domain/`, `pkg/types/` | Zero external imports. Defines port interfaces + types. |
| Application Services | `internal/app/` | Composes ports only. Never imports adapters. |
| Driven Adapters | `internal/adapters/surreal/`, `gemini/`, etc. | Implements port interfaces. |
| Driving Adapters | `internal/adapters/http/` (server), `client/` (CLI) | Translates HTTP ↔ service calls. |
| CLI | `cmd/` | Thin HTTP client via `internal/adapters/client/`. |

### Port Interfaces

| Port | Implementations |
|------|----------------|
| `OpenCodeGraph` | SurrealDB 3.0 (graph + HNSW vector + BM25 FTS — unified) |
| `Embedder` | Gemini, Voyage AI, Ollama |
| `LLMExplainer` | Gemini, Ollama |
| `AgentRunner` | Google ADK (Gemini or OpenRouter via ModelFactory) |
| `Parser` | tree-sitter (CGO) — Go, Python, TypeScript, JavaScript |
| `FileWalker` | OS filesystem (.gitignore-aware) |
| `TemporalStore` | SurrealDB |
| `SessionStore` + `MemoryStore` | SurrealDB |
| `GitWalker` | git CLI adapter |

`OpenCodeGraph` is the single graph port replacing the previous GraphStore, VectorIndex, TextIndex, and FieldFlowStore interfaces. See [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md) for the full design.

---

## 4. Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Language | Go 1.26+ | Single static binary, strong concurrency, CGO for tree-sitter |
| CLI | Cobra + Viper | Industry-standard Go CLI framework |
| HTTP server | Gin + gin-contrib | Fast, middleware ecosystem |
| HTTP clients | Resty v3 | Fluent API, auto-retry, EventSource SSE |
| AST parsing | go-tree-sitter (CGO) | Multi-language, incremental parsing |
| Database | SurrealDB 3.0 | Hybrid graph + vector (HNSW) + FTS (BM25) in single query |
| Embeddings | Gemini / Voyage AI / Ollama | Configurable via `EMBED_PROVIDER` |
| LLM | Gemini / OpenRouter / Ollama | Configurable via `LLM_PROVIDER` |
| Agent | Google ADK for Go | model.LLM interface, tool use, session management |
| Logging | log/slog (stdlib) | Structured, zero dependencies |

---

## 5. Embedding Strategy

Three providers, selected by `EMBED_PROVIDER` env var:

| Provider | Model | Dimensions | Cost/1M tokens |
|----------|-------|-----------|----------------|
| gemini | `gemini-embedding-2-preview` | 3072 | $0.15 |
| voyage | `voyage-code-3` | 1024 | $0.06 |
| ollama | configurable | configurable | Free (local) |

All providers output at a normalized dimension configured by `EMBED_DIM`. HNSW indexes are created at this dimension.

See [EMBEDDING_RESEARCH.md](EMBEDDING_RESEARCH.md) for model comparison and benchmarks.

---

## 6. Graph Data Model

The graph uses string labels (not Go enums) and extensible property maps. Node tables (`function`, `class`, `file`, `module`) are SCHEMAFULL for HNSW vector indexes and COMPUTED fields. Edge tables are SCHEMALESS — new edge types are created automatically via `RELATE`.

13 edge types: `calls`, `imports`, `defines`, `inherits`, `uses`, `data_flow`, `reads`, `writes`, `route`, `control_flow`, `data_dep`, plus system edges.

Every traversal is label-parameterized: the caller specifies which edge labels to follow. Trace, blast, flow, and security analysis are all the same `TraverseGraph` API with different label sets.

See [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md) for the unified graph abstraction and [DATABASE.md](DATABASE.md) for schema details.

---

## 7. Key Design Decisions

**Single binary** — `curl | sh` installs it in seconds. No version conflicts. Works offline.

**Ports and adapters** — Three complex external systems (SurrealDB, Gemini, tree-sitter) each with their own failure modes. Isolating domain logic means every service is unit-testable without infrastructure.

**SurrealDB over Neo4j + Pinecone** — Graph traversal + HNSW vector ANN + BM25 full-text in a single SurrealQL query. Eliminates three separate databases.

**OpenCodeGraph** — One port interface for all graph operations. Traversal is label-parameterized, not method-per-technique. Adding a new analysis technique means registering an EdgeLinker — zero changes to the port, adapter, or services.

**Streamable HTTP over gRPC** — Industry standard for AI tools (Claude Code, MCP, Continue.dev). Browser-native, curl-debuggable.

**Multi-provider** — Embeddings and LLM are separate choices. Use Gemini for embeddings + OpenRouter for LLM, or Ollama for both locally. Agent uses ModelFactory injection so delegate.go never imports concrete adapters.

**Two-phase indexing** — Extract per-file (parallel) → Link globally (sequential with SymbolTable) → Process per-batch (parallel). This architecture enables cross-file edge resolution without requiring a full database round-trip during parsing.
