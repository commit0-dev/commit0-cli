# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# commit0 — Claude Coding Guide

> Graph-based source code analyzer. Go binary. SurrealDB 3.0 + multi-provider embeddings + tree-sitter + ADK agent.

## Critical Rules

- `internal/domain/` and `pkg/types/` must **never** import external packages.
- All external access goes through port interfaces in `internal/domain/ports.go`.
- `internal/app/` services compose ports only — no direct adapter imports.
- Use domain error types from `internal/domain/errors.go`, not raw `fmt.Errorf`.
- Never unbounded goroutines — use `errgroup.WithContext` + `SetLimit(N)`.
- HTTP clients use Resty v3 (`resty.dev/v3`) — never raw `net/http` for outbound calls.
- HTTP server uses Gin (`github.com/gin-gonic/gin`) — never Echo.

## Stack

- **Module:** `github.com/commit0-dev/commit0` | **Go:** 1.26+ (CGO required for tree-sitter)
- **DB:** SurrealDB 3.0 | **Embeddings:** Gemini / Voyage / Ollama (configurable) | **LLM:** Gemini / OpenRouter / Ollama
- **AST:** `smacker/go-tree-sitter` | **CLI:** Cobra + Viper
- **HTTP Server:** Gin + gin-contrib/cors + gin-contrib/requestid
- **HTTP Client:** Resty v3 (all outbound API calls: OpenRouter, Voyage, Ollama)
- **Agent:** Google ADK v1.0.0 (`google.golang.org/adk`) with multi-model support via OpenRouter

## Commands

```bash
make build          # CGO_ENABLED=1 go build -trimpath … -o commit0 .
make test           # go test -count=1 -timeout=5m ./...
make test-race      # + race detector
make test-cover     # internal/app coverage — enforces 98% threshold
make lint           # golangci-lint (mirrors pre-push hook and CI)
make lint-fix       # golangci-lint --fix
make install-hooks  # pre-commit (fmt/vet) + pre-push (golangci-lint)

go test -count=1 -run TestQueryService ./internal/app/...
```

## Architecture

### Current State (v0.0.2)

Hexagonal (ports-and-adapters) architecture at the adapter layer, but **CLI commands are directly coupled to services via in-process Go function calls**. Each CLI command calls `wireDeps()` → constructs all adapters → calls service methods directly. The HTTP server (`commit0 serve`) does the same — handlers call services in-process.

```
                  Current: Direct In-Process Coupling
                  ════════════════════════════════════

  CLI (cmd/*.go)           HTTP Server (Gin)
       │                        │
       │  Go func call          │  Go func call
       ▼                        ▼
  ┌─────────────────────────────────────┐
  │  app.QueryService                   │
  │  app.TraceService                   │  ← All in same process
  │  app.AgentService (ADK)             │
  │  app.IndexService                   │
  └─────────────────────────────────────┘
```

**Problem:** CLI must link entire binary (SurrealDB, Gemini, tree-sitter CGO). Cannot deploy server separately. Cannot build thin clients in other languages. Each CLI command opens its own DB connection.

### Target State (v0.1.0) — Streamable HTTP (MCP-inspired)

Following the architecture pattern used by Claude Code, MCP, and the broader AI tooling ecosystem: **Streamable HTTP** — POST for requests, SSE for server→client streaming.

```
                  Target: Protocol-Based Service Boundary
                  ════════════════════════════════════════

  CLI          Web          VSCode        Mobile
  (Go)        (React)       (TS)         (Swift)
   │            │             │             │
   └────────────┴─────────────┴─────────────┘
                       │
              ┌────────┴────────┐
              │  Streamable HTTP │  POST + SSE (JSON)
              │  (MCP-aligned)   │
              └────────┬────────┘
                       │
              ┌────────┴────────┐
              │  commit0 server │
              │  (Gin + services)│
              └─────────────────┘
```

**Transport design (aligned with MCP specification):**
- **Unary ops** (query, trace, blast, repo CRUD): `POST /api/v1/<endpoint>` → JSON response
- **Streaming ops** (agent chat, index progress, find-root): `POST /api/v1/<endpoint>` → SSE stream
- **CLI becomes thin HTTP client** — no `wireDeps()`, no CGO, no DB connection
- **Server is the single process** that owns all adapters and service lifecycle

**Why not gRPC:** Claude Code, MCP, and the AI tooling ecosystem standardized on HTTP + SSE. Browser-native. curl-debuggable. No proxy needed. Protobuf typing is valuable but can be added later as an optimization without changing the transport.

### Key Architecture Components

**Driven adapters (output):**

| Port | Implementations | HTTP Client |
|------|----------------|-------------|
| `GraphStore` + `VectorIndex` + `TextIndex` | SurrealDB 3.0 | surrealdb.go SDK |
| `Embedder` | Gemini, Voyage AI, Ollama | Resty v3 |
| `LLMExplainer` | Gemini, Ollama | Resty v3 |
| `Parser` | tree-sitter (CGO) | N/A (in-process) |
| `FileWalker` | OS filesystem | N/A (in-process) |
| `AgentRunner` | Google ADK + OpenRouter/Gemini | Resty v3 (OpenRouter), genai SDK (Gemini) |

**Driving adapters (input):**

| Adapter | Transport | Status |
|---------|-----------|--------|
| HTTP Server (Gin) | REST + SSE | ✅ Implemented |
| CLI (Cobra) | Direct function calls | ⚠️ To migrate → thin HTTP client |
| TUI | Removed | ❌ Pending redesign |

**Agent orchestration:**
- Root agent: `internal/app/agent/service.go` — ADK `runner.Runner` with model.LLM interface
- Sub-agents: `internal/app/agent/delegate.go` — spawned via `ModelFactory` (injected, never imports concrete adapters)
- Scratchpad: `internal/app/agent/scratchpad.go` — evidence scoring, convergence gates, cost budget
- Model support: Gemini (default) or OpenRouter (200+ models) via `LLM_PROVIDER` config

### Pipelines

**Index pipeline:** Walk → Parse → Embed → Store via buffered channels. Non-fatal errors skip + log + continue.
**Query pipeline:** Vector ANN + BM25 FTS in parallel → Reciprocal Rank Fusion → LLM stream (`internal/app/fusion.go`).
**Embed batching:** Up to 100 inputs per `EmbedBatch` call; SHA-256 content hash for deduplication.

## Testing

`internal/app/` unit tests use in-memory stubs for all ports (`stubs_test.go`). Coverage: **98%** threshold on `internal/app/...`. Adapter integration tests require live SurrealDB + API keys. Compile-time check pattern: `_ domain.GraphStore = (*SurrealAdapter)(nil)`.

## SurrealDB 3.0 Specifics

Use `HNSW` (not `MTREE`), `COMPUTED` (not `<future>`), `LET $var`, `type::record()`, `rand::id()`, `FULLTEXT ANALYZER`, `REFERENCE ON DELETE CASCADE`. See [docs/DATABASE.md](docs/DATABASE.md).

## Embedding Providers

Three embedding providers, selected by `EMBED_PROVIDER` env var:

| Provider | Model | Dimensions | Config |
|----------|-------|-----------|--------|
| `gemini` (default) | `gemini-embedding-2-preview` | 3072 | `GEMINI_API_KEY` |
| `voyage` | `voyage-code-3` | 1024 | `VOYAGE_API_KEY` |
| `ollama` | `nomic-embed-text` | 768 | `OLLAMA_URL` (local) |

**Index (documents):** `"title: [KIND] {Qualified} | text: {description}"` — produced by `ContextBuilder`.
**Query:** `"task: code retrieval | query: {user_question}"` — prepended by embedder.

## LLM Providers

Three LLM providers, selected by `LLM_PROVIDER` env var:

| Provider | Model | Usage |
|----------|-------|-------|
| `gemini` (default) | `gemini-2.5-flash` | ADK agent + explanation |
| `openrouter` | Configurable (200+ models) | ADK agent via `model.LLM` adapter |
| `ollama` | Local models (e.g. `gemma3:4b`) | Explanation only (no ADK support) |

## Custom Skills

`.claude/skills/`: `commit0-go`, `commit0-surrealdb`, `commit0-treesitter`, `commit0-gemini`

## Further Reading

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) · [docs/BACKEND.md](docs/BACKEND.md) · [docs/DATABASE.md](docs/DATABASE.md)
