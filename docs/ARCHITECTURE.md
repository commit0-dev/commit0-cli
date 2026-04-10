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

### Why Not gRPC

Claude Code, MCP, and the AI tooling ecosystem standardized on HTTP + SSE. Browser-native, curl-debuggable, no proxy needed. See CLAUDE.md for full rationale.

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
| `GraphStore` + `VectorIndex` + `TextIndex` | SurrealDB 3.0 (unified) |
| `Embedder` | Gemini, Voyage AI, Ollama |
| `LLMExplainer` | Gemini, Ollama |
| `AgentRunner` | Google ADK (Gemini or OpenRouter via ModelFactory) |
| `Parser` | tree-sitter (CGO) — Go, Python, TypeScript, JavaScript |
| `FileWalker` | OS filesystem (.gitignore-aware) |
| `SessionStore` + `MemoryStore` | SurrealDB |
| `GitWalker` | git CLI adapter |

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

Three providers, selected by `EMBED_PROVIDER` env var. Gemini Embedding 2 is the default — the only production model placing text, code, and images in one vector space.

| Provider | Model | Dimensions | Cost/1M tokens |
|----------|-------|-----------|----------------|
| gemini | `gemini-embedding-2-preview` | 3072 | $0.15 |
| voyage | `voyage-code-3` | 1024 | $0.06 |
| ollama | `nomic-embed-text` | 768 | Free (local) |

**Task prefix format** (Gemini Embedding 2 uses instruction-based, not enum):
- Index: `"task: search result | query: {embedding_text}"`
- Query: `"task: search query | query: {user_question}"`

See [EMBEDDING_RESEARCH.md](EMBEDDING_RESEARCH.md) for model comparison and benchmarks.

---

## 6. Graph Data Model

4 node tables (function, class, file, module) + 11 edge types (calls, imports, defines, inherits, uses, data_flow, reads, writes, route, control_flow, data_dep). Every node carries a dense embedding vector indexed with HNSW. Centrality computed on read via `COMPUTED` fields.

See [DATABASE.md](DATABASE.md) for complete schema, index tuning, and query patterns.

---

## 7. Key Design Decisions

**Single binary** — `curl | sh` installs it in seconds. No version conflicts. Works offline. Easy to ship in Docker, CI, dev containers.

**Ports and adapters** — Three complex external systems (SurrealDB, Gemini, tree-sitter) each with their own failure modes. Isolating domain logic means every service is unit-testable without infrastructure, and adapters are independently replaceable.

**SurrealDB over Neo4j + Pinecone** — Graph traversal + HNSW vector ANN + BM25 full-text in a single SurrealQL query. Eliminates three separate databases. COMPUTED fields, REFERENCE constraints, client-side transactions, and changefeeds.

**Streamable HTTP over gRPC** — Industry standard for AI tools (Claude Code, MCP, Continue.dev). Browser-native. curl-debuggable. No proxy needed.

**Multi-provider** — Embeddings and LLM are separate choices. Use Gemini for embeddings + OpenRouter for LLM, or Ollama for both locally. Agent uses ModelFactory injection so delegate.go never imports concrete adapters.

---

## 8. Delivery Status

| Phase | Scope | Status |
|-------|-------|--------|
| 1 | Core indexer: walk→parse→embed→store, query, repo CRUD | Done |
| 2 | Graph traversal: trace, blast, TypeScript/JS, HTTP server | Done |
| 3 | Sessions, SSE streaming, incremental re-indexing | Done |
| 4 | Multi-repo, VSCode extension, watch mode | Done |
| 5 | Find commit zero: data flow, temporal graph, agent, memory | Done |
| 6 | Multi-provider (Voyage, OpenRouter, Ollama), Streamable HTTP | Done |
| Next | TUI redesign, thin CLI binary (no CGO), web UI | Planned |
