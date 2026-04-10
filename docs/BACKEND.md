# commit0 — Core Backend Architecture

> Detailed architecture, service design, concurrency model, and implementation patterns.

**Companion documents:**
- `ARCHITECTURE.md` — High-level vision, tech stack, directory layout
- `DATABASE.md` — SurrealDB 3.0 schema, indexes, query patterns
- `LAYOUT.md` — Full annotated file tree

---

## 1. Architectural Philosophy

| Principle | Implementation |
|---|---|
| Zero friction install | Single binary, no runtime deps, `curl \| sh` or `brew install` |
| Offline-capable | All graph queries work without network; only embedding/LLM need API |
| Fast incremental updates | SHA-256 cache, git-diff aware re-indexing |
| Hybrid retrieval | Vector ANN + graph traversal + full-text in one SurrealQL statement |
| Streaming-first | SSE for long operations; async polling for indexing |
| Extensible by interface | Every subsystem behind a Go interface; swap any adapter |
| Client-server separation | CLI is thin HTTP client; server owns all adapters |

### Prior Art

- **DeepWiki** — LLM-generated docs from code structure. commit0 adds interactive query + persistent knowledge graph.
- **Neo4j Codebase Knowledge Graphs** — Call graphs in graph DB. commit0 adds vector embeddings for semantic queries.
- **GraphGen4Code** — tree-sitter extraction at scale. commit0 adds real-time incremental indexing + multimodal embeddings.
- **Hybrid RAG** — Vector + keyword + graph retrieval. commit0 implements natively via SurrealDB.

---

## 2. Layered Architecture

Ports-and-adapters (hexagonal) with Streamable HTTP client-server separation.

### Layers

| Layer | Location | Responsibility |
|-------|----------|---------------|
| **Domain Core** | `internal/domain/`, `pkg/types/` | Port interfaces, domain errors, types. Zero external imports. |
| **Application Services** | `internal/app/` | Orchestrate ports. Only layer composing multiple interfaces. |
| **Driven Adapters** | `internal/adapters/surreal/`, `gemini/`, `openrouter/`, etc. | Implement port interfaces against external systems. |
| **Driving Adapters** | `internal/adapters/http/` (server), `internal/adapters/client/` (CLI HTTP client) | Translate HTTP ↔ service calls. |
| **CLI** | `cmd/` | Thin HTTP client. `serve.go` starts server; others call it via HTTP. |

### Port Interfaces (`internal/domain/ports.go`)

| Port | Methods | Implementations |
|------|---------|----------------|
| `GraphStore` | UpsertNode, GetNode, TraceForward/Reverse, BlastRadius, UpsertFileBatch, Repo CRUD | SurrealDB |
| `VectorIndex` | Search (ANN over embeddings) | SurrealDB (HNSW) |
| `TextIndex` | Search (BM25 full-text) | SurrealDB |
| `Embedder` | EmbedBatch (≤100 inputs), EmbedQuery | Gemini, Voyage AI, Ollama |
| `LLMExplainer` | Explain (streaming), ExplainStructured (JSON) | Gemini, Ollama |
| `Parser` | Parse (file → AST nodes + edges), SupportedLanguages | tree-sitter (CGO) |
| `FileWalker` | Walk (repo → file entries, .gitignore-aware) | OS filesystem |
| `AgentRunner` | Chat (streaming events channel) | Google ADK |
| `GitWalker` | Diff, Blame, Log | git CLI adapter |
| `SessionStore` | Create/Get/List/Append sessions | SurrealDB |
| `MemoryStore` | Store/Retrieve/List memories (vector-indexed) | SurrealDB |

### Key Domain Types (`pkg/types/`)

| Type | File | Fields |
|------|------|--------|
| `CodeNode` | `ast.go` | ID, Kind, Name, Qualified, FilePath, RepoSlug, Language, Signature, Body, Embedding, ContentHash |
| `CodeEdge` | `ast.go` | Kind, FromID, ToID, CallSite, IsDynamic, CallType, Metadata |
| `QueryResult` | `result.go` | Nodes ([]ScoredNode), Explanation, Timing |
| `TraceResult` | `result.go` | Root, Tree ([]TraceHop), Direction, Explanation, Timing |
| `BlastResult` | `result.go` | Target, Affected ([]AffectedNode), Summary, Timing |
| `APISurface` | `api.go` | Endpoints ([]APIEndpointDetail), Timing |

---

## 3. Application Services

### 3.1 IndexService — Walk → Parse → Embed → Store

Staged concurrent pipeline with bounded channels:

```
Walk (1 goroutine) → Parse (N=GOMAXPROCS workers) → Embed (M=4 workers) → Store (P=4 workers)
     FileEntry           ParsedFile                   EmbeddedFile          SurrealDB
```

- **SHA-256 dedup**: Content hash skips re-embedding unchanged nodes
- **Batch embedding**: Up to 100 inputs per EmbedBatch call
- **Non-fatal errors**: Parse/embed failures skip + log + continue
- **Transactional upsert**: `UpsertFileBatch` atomically writes all nodes + edges for a file
- **Location**: `internal/app/index_service.go`

### 3.2 QueryService — Embed → Search → Fuse → Explain

1. **Embed** user question via `EmbedQuery`
2. **Parallel search**: Vector ANN + BM25 FTS via `errgroup`
3. **Reciprocal Rank Fusion** (`fusion.go`): Combines ranked lists with `score = Σ 1/(k + rank)`, optional centrality boost
4. **Top-K selection** and LLM explanation (streaming, non-fatal if fails)
- **Location**: `internal/app/query_service.go`, `fusion.go`

### 3.3 TraceService — Symbol → Graph Walk → Explain

1. **Resolve symbol**: Exact qualified match → fallback vector search (min score 0.8)
2. **Graph traversal**: `TraceForward` or `TraceReverse` on GraphStore
3. **LLM explanation** of call chain
- **Location**: `internal/app/trace_service.go`

### 3.4 BlastService — Reverse Transitive Impact

1. **Resolve target** symbol
2. **Reverse traversal** via `BlastRadius` (all transitive callers)
3. **Dedup + group** by module, sort by hop distance
4. **LLM summary** of impact
- **Location**: `internal/app/blast_service.go`

### 3.5 Additional Services

| Service | Location | Purpose |
|---------|----------|---------|
| `RepoService` | `repo_service.go` | Repository CRUD + lifecycle |
| `AgentService` | `agent/service.go` | ADK runner with model.LLM injection, streaming ChatEvents |
| `FieldFlowService` | `field_flow_service.go` | Field-level data flow tracing |
| `TemporalService` | `temporal_service.go` | Git history + temporal queries |
| `RootCauseAnalysisService` | `rootcause_analysis_service.go` | End-to-end root cause analysis |
| `APISurfaceService` | `api_surface_service.go` | API endpoint discovery + OpenAPI generation |
| `MemoryManager` | `memory/manager.go` | 3-tier memory: working → session → persistent |

---

## 4. Driven Adapters

### 4.1 SurrealDB Adapter (`internal/adapters/surreal/`)

Unified adapter implementing `GraphStore`, `VectorIndex`, `TextIndex`, `SessionStore`, `MemoryStore`. Single WebSocket connection shared across all services via `wireServeServices()`.

- **Connection**: WebSocket with retry (exponential backoff, configurable retries)
- **Schema**: Embedded `schema.surql` applied via `ApplySchema()` with version tracking
- **Batch upsert**: Client-side transactions for atomic file ingestion
- **Hybrid search**: Vector ANN (HNSW) + BM25 FTS in single SurrealQL query with RRF

### 4.2 Embedding Adapters

| Adapter | Location | Transport | Features |
|---------|----------|-----------|----------|
| Gemini | `gemini/embedder.go` | genai SDK | Batch ≤100, multimodal (text+image), retry via `infra/retry` |
| Voyage AI | `voyage/embedder.go` | Resty v3 | Batch ≤128, code-optimized model, auto-retry |
| Ollama | `local/embedder.go` | Resty v3 | Local inference, model-specific prefixes |

### 4.3 LLM Adapters

| Adapter | Location | Transport | Features |
|---------|----------|-----------|----------|
| Gemini | `gemini/explainer.go` | genai SDK | Streaming, structured JSON output |
| OpenRouter | `openrouter/` | Resty v3 + EventSource SSE | ADK `model.LLM` interface, 200+ models, cost tracking |
| Ollama | `local/ollama.go` | Resty v3 | Local inference, /api/chat endpoint |

### 4.4 tree-sitter Parser (`internal/adapters/treesitter/`)

CGO-linked parser with per-language extractors. Extracts nodes (functions, classes, modules) and edges (calls, imports, defines, inherits, routes, control_flow, data_dep).

### 4.5 Agent Orchestration (`internal/app/agent/`)

Single user prompt → autonomous comprehensive code analysis. The **Analyst Agent** plans, delegates to sub-agents, evaluates evidence quality, follows up on gaps, and converges on a report.

| Component | File | Purpose |
|-----------|------|---------|
| AgentService | `service.go` | ADK runner with `model.LLM` injection (Gemini or OpenRouter) |
| Delegate | `delegate.go` | Sub-agent spawning via `ModelFactory` (no concrete adapter imports) |
| Scratchpad | `scratchpad.go` | Evidence scoring, convergence gates, cost budget |
| Tools | `tools.go` | 10+ ADK tools: search, trace, blast, flow, temporal, root cause |
| Instructions | `instructions.go` | Analyst + 4 sub-agent system prompts |

**Scratchpad** — memory + ranking + feedback loop:
- Evidence scored on 4 dimensions: Relevance, Confidence, Novelty, Actionability
- Priority = 0.3×R + 0.3×C + 0.2×N + 0.2×A (server-side validated)
- 5 convergence gates: min 3 delegations, min 5 evidence, no high-priority open questions, novelty decay, hypothesis confirmation

**Sub-agent types**: `search` (discovery), `trace` (structure), `security` (risks), `deep_dive` (code detail) — each with restricted tool subset.

**Feedback loop**: DELEGATE → RECEIVE → EXTRACT → UPDATE HYPOTHESES → GENERATE QUESTIONS → CHECK CONVERGENCE → DECIDE NEXT

**Guardrails**: Token budget, score validation, circuit breaker on failure, contradiction detection, cost control, protocol enforcement (scratchpad must be updated between delegations).

---

## 5. HTTP API Layer (Gin)

The Gin server is the central driving adapter. All clients communicate via Streamable HTTP.

### Routes

| Method | Path | Handler | Response |
|--------|------|---------|----------|
| GET | `/health` | `handleHealth` | JSON |
| GET | `/api/v1/repos` | `handleListRepos` | JSON |
| POST | `/api/v1/repos` | `handleCreateRepo` | JSON (201) |
| GET | `/api/v1/repos/:slug` | `handleGetRepo` | JSON |
| DELETE | `/api/v1/repos/:slug` | `handleDeleteRepo` | JSON |
| POST | `/api/v1/index` | `handleStartIndex` | JSON (202, async) |
| GET | `/api/v1/index/:job_id` | `handleIndexStatus` | JSON (poll) |
| POST | `/api/v1/query` | `handleQuery` | JSON |
| POST | `/api/v1/trace` | `handleTrace` | SSE |
| POST | `/api/v1/trace/json` | `handleTraceJSON` | JSON |
| POST | `/api/v1/blast` | `handleBlast` | JSON |
| POST | `/api/v1/agent/chat` | `handleAgentChat` | SSE |
| POST | `/api/v1/flow` | `handleFieldFlow` | JSON |
| POST | `/api/v1/history` | `handleHistory` | JSON |
| POST | `/api/v1/find-root` | `handleFindRoot` | SSE |
| POST | `/api/v1/api/discover` | `handleAPIDiscover` | JSON |
| POST | `/api/v1/api/spec` | `handleAPISpec` | JSON |
| GET | `/api/v1/nodes/lookup` | `handleNodeLookup` | JSON |
| GET | `/api/v1/nodes/by-file` | `handleNodesByFile` | JSON |
| GET | `/api/v1/nodes/:id/neighborhood` | `handleGetNeighborhood` | JSON |

### SSE Streaming Pattern

```go
c.Header("Content-Type", "text/event-stream")
c.Header("Cache-Control", "no-cache")
c.Status(http.StatusOK)

writeSSE(c, "hop", data)       // event: hop\ndata: {...}\n\n
writeSSE(c, "done", summary)   // event: done\ndata: {...}\n\n
```

### Middleware Stack

1. `gin.Recovery()` — panic recovery
2. `requestid.New()` — X-Request-Id header
3. `SlogMiddleware` — structured request logging
4. `cors.New()` — CORS with configurable origins

### Error Mapping

`writeError()` maps `domain.DomainError` codes to HTTP status: NotFound→404, Validation→400, Conflict→409, default→500.

---

## 6. CLI Layer (HTTP Client)

CLI commands are thin HTTP clients using `internal/adapters/client/` (Resty v3).

### Server URL Resolution

`--server-url` flag → `COMMIT0_SERVER_URL` env → default `http://localhost:8080`

### Communication Patterns

| CLI Command | HTTP Call | Pattern |
|-------------|----------|---------|
| `query` (agent) | POST /agent/chat | SSE stream via Resty EventSource |
| `query` (direct) | POST /query | JSON request-response |
| `index` | POST /index + GET /index/:id | Async start + exponential backoff polling |
| `trace` | POST /trace/json | JSON request-response |
| `blast` | POST /blast | JSON request-response |
| `repo list/get/create/delete` | REST CRUD | JSON request-response |
| `api discover/spec` | POST /api/* | JSON request-response |

### Commands Not Using HTTP

| Command | Reason |
|---------|--------|
| `serve` | IS the server — uses `wireServeServices()` |
| `db start/stop` | Local SurrealDB process management |

---

## 7. Error Handling

### Domain Errors (`internal/domain/errors.go`)

| Code | HTTP Status | Use |
|------|-------------|-----|
| `not_found` | 404 | Symbol, repo, job not found |
| `validation` | 400 | Missing required fields, bad input |
| `conflict` | 409 | Duplicate repo slug |
| `rate_limit` | 429 | API provider throttling |
| `timeout` | 408 | Deadline exceeded |

### Non-Fatal Error Policy

Index pipeline: parse/embed/store failures skip the file + log + continue. Query explanation failure returns results without explanation. Agent tool errors are reported to the LLM for recovery.

---

## 8. Configuration (`internal/config/config.go`)

Env-var driven via Viper with `.env` auto-discovery. Key groups:

| Group | Vars | Purpose |
|-------|------|---------|
| `EMBED_PROVIDER` | gemini/voyage/ollama | Embedding model selection |
| `LLM_PROVIDER` | gemini/openrouter/ollama | LLM selection |
| `SURREAL_*` | URL, USER, PASS, NS, DB | Database connection |
| `GEMINI_*` | API_KEY, EMBED_MODEL, EXPLAIN_MODEL | Gemini config |
| `OPENROUTER_*` | API_KEY, MODEL, MAX_TOKENS | OpenRouter config |
| `VOYAGE_*` | API_KEY, MODEL, EMBED_DIM | Voyage config |
| `OLLAMA_*` | URL, MODEL, EMBED_MODEL | Local model config |
| `SERVER_*` | PORT, CORS_ORIGINS, TIMEOUTS | HTTP server |

---

## 9. Testing Strategy

| Level | Location | Approach |
|-------|----------|----------|
| **Unit** | `internal/app/*_test.go` | In-memory stubs (`stubs_test.go`). 98% coverage threshold. |
| **HTTP handlers** | `internal/adapters/http/handlers_test.go` | `gin.CreateTestContext` + `httptest.NewRecorder` |
| **HTTP clients** | `internal/adapters/openrouter/client_test.go` | `httptest.NewServer` + Resty |
| **Adapters** | `internal/adapters/*/` | Live SurrealDB + API keys (integration) |
| **Compile-time** | All adapter files | `var _ domain.X = (*Adapter)(nil)` |

---

## 10. Observability

- **Structured logging**: `log/slog` with consistent field names (service, method, duration_ms, err)
- **Timing breakdown**: Every operation returns `TimingInfo` (embed_ms, search_ms, graph_ms, explain_ms, total_ms)
- **Cost tracking**: OpenRouter client accumulates input/output tokens per generation
- **Index progress**: Job store with polling endpoint, reports files_indexed + nodes_created
