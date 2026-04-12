# commit0 — Backend Architecture

> Services, adapters, HTTP API, agent orchestration, and concurrency patterns.

**Companion documents:**
- [ARCHITECTURE.md](ARCHITECTURE.md) — High-level design, tech stack
- [DATABASE.md](DATABASE.md) — SurrealDB schema, indexes, query patterns
- [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md) — Unified graph abstraction
- [LAYOUT.md](LAYOUT.md) — Annotated file tree

---

## 1. Layered Architecture

Ports-and-adapters (hexagonal) with Streamable HTTP client-server separation.

| Layer | Location | Responsibility |
|-------|----------|---------------|
| **Domain Core** | `internal/domain/`, `pkg/types/` | Port interfaces, domain errors, types. Zero external imports. |
| **Application Services** | `internal/app/` | Orchestrate ports. Only layer composing multiple interfaces. |
| **Driven Adapters** | `internal/adapters/surreal/`, `gemini/`, `openrouter/`, etc. | Implement port interfaces against external systems. |
| **Driving Adapters** | `internal/adapters/http/` (server), `internal/adapters/client/` (CLI HTTP client) | Translate HTTP <> service calls. |
| **CLI** | `cmd/` | Thin HTTP client. `serve.go` starts server; others call it via HTTP. |

---

## 2. Port Interfaces

### OpenCodeGraph (`internal/domain/open_code_graph.go`)

The single graph port for all node/edge CRUD, traversal, search, and repo operations. All services depend on this one interface.

| Method Group | Methods |
|-------------|---------|
| Node CRUD | `PutNode`, `GetNode`, `FindNode`, `DeleteNode` |
| Edge CRUD | `PutEdge`, `DeleteEdgesFrom` |
| Batch | `PutBatch`, `DeleteByRepo`, `DeleteByFile` |
| Traversal | `TraverseGraph` (label-parameterized), `Neighbors` |
| Search | `VectorSearch` (HNSW ANN), `TextSearch` (BM25 FTS) |
| Listing | `ListNodes` (with filter opts), `ListEdges` (by label), `ListFilePaths` |
| Repo | `PutRepo`, `GetRepo`, `ListRepos`, `DeleteRepo`, `FindRepoByRemoteURL`, `UpdateRepoIndexedAt` |
| Schema | `ApplySchema` |

### Other Ports (`internal/domain/ports.go`)

| Port | Methods | Implementations |
|------|---------|----------------|
| `Embedder` | `EmbedBatch` (<=100 inputs), `EmbedQuery` | Gemini, Voyage AI, Ollama |
| `LLMExplainer` | `Explain` (streaming), `ExplainStructured` (JSON) | Gemini, Ollama |
| `Parser` | `Parse` (file -> AST nodes + edges), `SupportedLanguages` | tree-sitter (CGO) |
| `FileWalker` | `Walk` (repo -> file entries, .gitignore-aware) | OS filesystem |
| `AgentRunner` | `Chat` (streaming events channel) | Google ADK |
| `TemporalStore` | Commit-aware upsert, mark removed, query ranges, node history | SurrealDB |
| `MemoryStore` | Store/Retrieve/List memories (vector-indexed) | SurrealDB |
| `GitWalker` | `ListCommits`, `DiffCommit`, `ReadFileAtCommit`, `CommitInfo` | git CLI adapter |

### Key Domain Types (`pkg/types/`)

| Type | File | Purpose |
|------|------|---------|
| `CodeNode` | `ast.go` | Function/class/file/module with embedding, signature, body |
| `CodeEdge` | `ast.go` | Typed relationship with call site, metadata, dynamic flag |
| `QueryResult` | `result.go` | Scored nodes + explanation + timing |
| `TraceResult` | `result.go` | Hop tree + direction + explanation |
| `BlastResult` | `result.go` | Affected nodes + hop count + summary |
| `FieldFlowResult` | `result.go` | Data flow chains + mutation points |
| `RootCauseReport` | `result.go` | Suspect commits + causal chain + fix suggestion |
| `APISurface` | `api.go` | Discovered endpoints + taint flows |

---

## 3. Application Services

### IndexService — Walk -> Parse -> Link -> Embed -> Store

Two-phase pipeline with bounded concurrency:

```
Phase 1: EXTRACT (per-file, parallel)
  Walk -> Parse (N=GOMAXPROCS workers) -> accumulate all nodes + edges

Phase 2: LINK (global, sequential)
  Build SymbolTable from ALL nodes -> run EdgeLinker chain

Phase 3: PROCESS (per-batch, parallel)
  Summarize (optional) -> Embed (M=4 workers) -> Store (P=4 workers)
```

- **EdgeLinker chain**: `CallLinker`, `DataFlowLinker`, `DefinesLinker`, `FieldAccessLinker`, `RouteLinker` — resolves cross-file edges against the complete SymbolTable
- **SHA-256 dedup**: Content hash skips re-embedding unchanged nodes
- **`--fast` flag**: Skips LLM summarization and neighborhood re-embedding
- **`--reparse` flag**: Forces re-parsing while preserving existing summaries/embeddings
- **Location**: `internal/app/index_service.go`, `internal/app/linkers/`

### QueryService — Embed -> Search -> Fuse -> Explain

1. Embed user question via `EmbedQuery`
2. Parallel search: Vector ANN + BM25 FTS via `errgroup`
3. Reciprocal Rank Fusion (`fusion.go`): `score = sum(1/(k + rank))`, centrality boost
4. Top-K selection and LLM explanation (streaming, non-fatal if fails)
- **Location**: `internal/app/query_service.go`, `fusion.go`

### TraceService — Symbol -> Graph Walk -> Explain

1. Resolve symbol: exact qualified match -> fallback vector search (min score 0.8)
2. Label-parameterized traversal via `OpenCodeGraph.TraverseGraph`
3. LLM explanation of call chain
- **Location**: `internal/app/trace_service.go`

### BlastService — Reverse Transitive Impact

1. Resolve target symbol
2. Reverse traversal via `TraverseGraph` with `direction: "reverse"`
3. Dedup + group by module, sort by hop distance
4. LLM summary of impact
- **Location**: `internal/app/blast_service.go`

### Additional Services

| Service | Location | Purpose |
|---------|----------|---------|
| `RepoService` | `repo_service.go` | Repository CRUD + lifecycle |
| `AgentService` | `agent/service.go` | ADK runner with `model.LLM` injection, streaming ChatEvents |
| `FieldFlowService` | `field_flow_service.go` | Field-level data flow tracing via `TraverseGraph` with data_flow/reads/writes labels |
| `TemporalService` | `temporal_service.go` | Git history + temporal graph queries |
| `RootCauseAnalysisService` | `rootcause_analysis_service.go` | End-to-end root cause analysis |
| `APISurfaceService` | `api_surface_service.go` | API endpoint discovery + OpenAPI generation |
| `MemoryManager` | `memory/manager.go` | 3-tier memory: working -> session -> persistent |

---

## 4. Driven Adapters

### SurrealDB Adapter (`internal/adapters/surreal/`)

Unified adapter implementing `OpenCodeGraph`, `TemporalStore`, `MemoryStore`, `SessionStore`, and sync interfaces. Dual WebSocket connection pools (read + write) shared across all services.

| File | Responsibility |
|------|---------------|
| `client.go` | Connection, auth, pools, helpers |
| `open_code_graph.go` | `OpenCodeGraph` interface adapter (delegates to implementation files) |
| `graph_store.go` | Node/edge CRUD, traversal, batch upsert, neighborhood |
| `vector_index.go` | HNSW ANN search across node tables |
| `text_index.go` | BM25 full-text search |
| `field_flow_store.go` | Data flow traversal with field/mutation metadata |
| `schema.go` | Schema DDL with version tracking |
| `session_store.go` | Chat session persistence |

### Embedding Adapters

| Adapter | Location | Transport | Features |
|---------|----------|-----------|----------|
| Gemini | `gemini/embedder.go` | genai SDK | Batch <=100, multimodal (text+image), retry |
| Voyage AI | `voyage/embedder.go` | Resty v3 | Batch <=128, code-optimized model |
| Ollama | `local/embedder.go` | Resty v3 | Local inference, model-specific prefixes |

### LLM Adapters

| Adapter | Location | Transport | Features |
|---------|----------|-----------|----------|
| Gemini | `gemini/explainer.go` | genai SDK | Streaming, structured JSON output |
| OpenRouter | `openrouter/` | Resty v3 + EventSource SSE | ADK `model.LLM` interface, 200+ models |
| Ollama | `local/ollama.go` | Resty v3 | Local inference, /api/chat endpoint |

### Agent Orchestration (`internal/app/agent/`)

Single user prompt -> autonomous comprehensive code analysis. The Analyst Agent plans, delegates to sub-agents, evaluates evidence, and converges on a report.

| Component | File | Purpose |
|-----------|------|---------|
| AgentService | `service.go` | ADK runner with `model.LLM` injection (Gemini or OpenRouter) |
| Delegate | `delegate.go` | Sub-agent spawning via `ModelFactory` (no concrete adapter imports) |
| Scratchpad | `scratchpad.go` | Evidence scoring, convergence gates, cost budget |
| Tools | `tools.go` | 10+ ADK tools: search, trace, blast, flow, temporal, root cause |
| Instructions | `instructions.go` | Analyst + 4 sub-agent system prompts |

**Sub-agent types**: `search` (discovery), `trace` (structure), `security` (risks), `deep_dive` (code detail).

---

## 5. HTTP API (Gin)

### Routes

| Method | Path | Handler | Response |
|--------|------|---------|----------|
| GET | `/health` | `handleHealth` | JSON |
| GET | `/api/v1/repos` | `handleListRepos` | JSON |
| POST | `/api/v1/repos` | `handleCreateRepo` | JSON (201) |
| GET | `/api/v1/repos/*slug` | `handleGetRepo` | JSON |
| DELETE | `/api/v1/repos/*slug` | `handleDeleteRepo` | JSON |
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

### Middleware Stack

1. `gin.Recovery()` — panic recovery
2. `requestid.New()` — X-Request-Id header
3. `SlogMiddleware` — structured request logging
4. `cors.New()` — CORS with configurable origins

### Error Mapping

`writeError()` maps `domain.DomainError` codes to HTTP status: NotFound->404, Validation->400, Conflict->409, default->500.

---

## 6. CLI Layer (HTTP Client)

CLI commands are thin HTTP clients using `internal/adapters/client/` (Resty v3).

### Server URL Resolution

`--server-url` flag -> `COMMIT0_SERVER_URL` env -> default `http://localhost:8080`

### Communication Patterns

| CLI Command | HTTP Call | Pattern |
|-------------|----------|---------|
| `query` (agent) | POST /agent/chat | SSE stream via Resty EventSource |
| `query` (direct) | POST /query | JSON request-response |
| `index` | POST /index + GET /index/:id | Async start + exponential backoff polling |
| `trace` | POST /trace/json | JSON request-response |
| `blast` | POST /blast | JSON request-response |
| `flow` | POST /flow | JSON request-response |
| `history` | POST /history | JSON request-response |
| `find-root` | POST /find-root | SSE stream |
| `repo list/get/create/delete` | REST CRUD | JSON request-response |
| `api discover/spec` | POST /api/* | JSON request-response |
| `analyze` | POST /agent/chat | SSE stream with analysis-specific prompts |

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
| `EMBED_DIM` | 1024 (default) | Normalized embedding dimension for HNSW |
| `SURREAL_*` | URL, USER, PASS, NS, DB | Database connection |
| `GEMINI_*` | API_KEY, EMBED_MODEL, EXPLAIN_MODEL | Gemini config |
| `OPENROUTER_*` | API_KEY, MODEL, MAX_TOKENS | OpenRouter config |
| `VOYAGE_*` | API_KEY, MODEL | Voyage config |
| `OLLAMA_*` | URL, MODEL, EMBED_MODEL | Local model config |
| `SERVER_*` | PORT, CORS_ORIGINS, TIMEOUTS | HTTP server |

---

## 9. Testing

| Level | Location | Approach |
|-------|----------|----------|
| **Unit** | `internal/app/*_test.go` | In-memory stubs (`stubs_test.go`) |
| **HTTP handlers** | `internal/adapters/http/handlers_test.go` | `gin.CreateTestContext` + `httptest.NewRecorder` |
| **HTTP clients** | `internal/adapters/openrouter/client_test.go` | `httptest.NewServer` + Resty |
| **Adapters** | `internal/adapters/*/` | Live SurrealDB + API keys (integration) |
| **Compile-time** | Adapter files | `var _ domain.OpenCodeGraph = (*adapter)(nil)` |
