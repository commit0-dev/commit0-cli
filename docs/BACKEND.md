# Backend Architecture

**See also:** [ARCHITECTURE.md](ARCHITECTURE.md) · [DATABASE.md](DATABASE.md) · [OPEN_CODE_GRAPH.md](OPEN_CODE_GRAPH.md) · [LAYOUT.md](LAYOUT.md)

---

## 1. Layered Architecture

| Layer | Location | Responsibility |
|-------|----------|---------------|
| Domain | `internal/domain/`, `pkg/types/` | Port interfaces, domain errors, types. No external imports. |
| Application | `internal/app/` | Service orchestration. Composes port interfaces only. |
| Driven adapters | `internal/adapters/surreal/`, `gemini/`, `openrouter/`, etc. | Implement port interfaces. |
| Driving adapters | `internal/adapters/http/` (server), `internal/adapters/client/` (CLI) | Translate between HTTP and service calls. |
| CLI | `cli/cmd/` | HTTP client. `serve` command starts the server; all others call it over HTTP. |

---

## 2. Port Interfaces

### OpenCodeGraph (`internal/domain/open_code_graph.go`)

Single interface for all graph operations. Every application service depends on this interface.

| Method group | Methods |
|-------------|---------|
| Node CRUD | `PutNode`, `GetNode`, `FindNode`, `DeleteNode` |
| Edge CRUD | `PutEdge`, `DeleteEdgesFrom` |
| Batch | `PutBatch`, `DeleteByRepo`, `DeleteByFile` |
| Traversal | `TraverseGraph` (label-parameterized), `Neighbors` |
| Search | `VectorSearch` (HNSW ANN), `TextSearch` (BM25) |
| Listing | `ListNodes`, `ListEdges`, `ListFilePaths` |
| Repo | `PutRepo`, `GetRepo`, `ListRepos`, `DeleteRepo`, `FindRepoByRemoteURL`, `UpdateRepoIndexedAt` |
| Schema | `ApplySchema` |

### Other Ports (`internal/domain/ports.go`)

| Interface | Methods | Implementations |
|-----------|---------|----------------|
| `Embedder` | `EmbedBatch`, `EmbedQuery` | Gemini, Voyage AI, Ollama |
| `LLMExplainer` | `Explain` (streaming), `ExplainStructured` (JSON) | Gemini, Ollama |
| `Parser` | `Parse`, `SupportedLanguages` | tree-sitter (CGO) |
| `FileWalker` | `Walk` | OS filesystem (.gitignore-aware) |
| `AgentRunner` | `Chat` | Google ADK |
| `TemporalStore` | Commit-aware upsert, mark removed, query ranges, history | SurrealDB |
| `MemoryStore` | `StoreMemory`, `RetrieveMemories`, `ListSessionMemories` | SurrealDB |
| `GitWalker` | `ListCommits`, `DiffCommit`, `ReadFileAtCommit`, `CommitInfo` | git CLI |

### Domain Types (`pkg/types/`)

| Type | File | Description |
|------|------|-------------|
| `CodeNode` | `ast.go` | Function, class, file, or module with embedding and metadata |
| `CodeEdge` | `ast.go` | Typed relationship between nodes |
| `QueryResult` | `result.go` | Scored nodes with optional explanation |
| `TraceResult` | `result.go` | Tree of hops from a graph traversal |
| `BlastResult` | `result.go` | Affected nodes grouped by module |
| `FieldFlowResult` | `result.go` | Data flow chains with mutation points |
| `RootCauseReport` | `result.go` | Suspect commits with causal chain |
| `APISurface` | `api.go` | Discovered HTTP endpoints |

---

## 3. Application Services

### IndexService

Staged pipeline: walk files, parse ASTs, resolve cross-file edges, embed nodes, store in database.

```
Phase 1: EXTRACT (per-file, parallel)
  Walk files -> Parse with tree-sitter -> accumulate nodes and edges

Phase 2: LINK (global, sequential)
  Build SymbolTable -> run EdgeLinker chain (CallLinker, DataFlowLinker,
  DefinesLinker, FieldAccessLinker, RouteLinker)

Phase 3: PROCESS (per-batch, parallel)
  Summarize (optional) -> Embed -> Store
```

Content-hash deduplication skips re-embedding for unchanged nodes. The `--fast` flag skips LLM summarization.

**Location:** `internal/app/index_service.go`, `internal/app/linkers/`

### QueryService

Embeds the user question, runs vector ANN and BM25 full-text searches in parallel, fuses results with Reciprocal Rank Fusion, and optionally generates an LLM explanation.

**Location:** `internal/app/query_service.go`, `fusion.go`

### TraceService

Resolves a symbol name to a graph node (exact match, then vector search fallback), traverses the graph along specified edge labels, and optionally explains the result.

**Location:** `internal/app/trace_service.go`

### BlastService

Reverse transitive traversal from a target node. Groups affected nodes by module and sorts by hop distance.

**Location:** `internal/app/blast_service.go`

### Other Services

| Service | File | Description |
|---------|------|-------------|
| `RepoService` | `repo_service.go` | Repository CRUD |
| `AgentService` | `agent/service.go` | ADK agent with tool use and sub-agent delegation |
| `FieldFlowService` | `field_flow_service.go` | Data flow tracing with field-level granularity |
| `TemporalService` | `temporal_service.go` | Git history and temporal graph queries |
| `RootCauseAnalysisService` | `rootcause_analysis_service.go` | Root cause analysis across commit history |
| `APISurfaceService` | `api_surface_service.go` | HTTP endpoint discovery |
| `MemoryManager` | `memory/manager.go` | Three-tier memory: working, session, persistent |

---

## 4. Adapters

### SurrealDB (`internal/adapters/surreal/`)

Implements `OpenCodeGraph`, `TemporalStore`, `MemoryStore`, `SessionStore`, and sync interfaces. Uses dual WebSocket connection pools (read and write).

| File | Responsibility |
|------|---------------|
| `client.go` | Connection management, authentication, pools |
| `open_code_graph.go` | OpenCodeGraph interface bridge |
| `graph_store.go` | Node/edge CRUD, traversal, batch upsert |
| `vector_index.go` | HNSW vector search |
| `text_index.go` | BM25 full-text search |
| `field_flow_store.go` | Data flow traversal |
| `schema.go` | DDL application with version tracking |

### Embedding Adapters

| Adapter | File | Transport |
|---------|------|-----------|
| Gemini | `gemini/embedder.go` | genai SDK |
| Voyage AI | `voyage/embedder.go` | Resty v3 |
| Ollama | `local/embedder.go` | Resty v3 |

### LLM Adapters

| Adapter | File | Transport |
|---------|------|-----------|
| Gemini | `gemini/explainer.go` | genai SDK |
| OpenRouter | `openrouter/` | Resty v3, EventSource SSE |
| Ollama | `local/ollama.go` | Resty v3 |

### Agent (`internal/app/agent/`)

Multi-step code analysis using Google ADK. An analyst agent delegates to specialized sub-agents (search, trace, security, deep-dive), tracks evidence in a scratchpad, and produces a structured report.

| File | Purpose |
|------|---------|
| `service.go` | ADK runner with model injection |
| `delegate.go` | Sub-agent spawning via ModelFactory |
| `scratchpad.go` | Evidence tracking and convergence gates |
| `tools.go` | Graph query tools exposed to the agent |
| `instructions.go` | System prompts |

---

## 5. HTTP API

### Routes

| Method | Path | Response |
|--------|------|----------|
| GET | `/health` | JSON |
| GET | `/api/v1/repos` | JSON |
| POST | `/api/v1/repos` | JSON (201) |
| GET | `/api/v1/repos/*slug` | JSON |
| DELETE | `/api/v1/repos/*slug` | JSON |
| POST | `/api/v1/index` | JSON (202) |
| GET | `/api/v1/index/:job_id` | JSON |
| POST | `/api/v1/query` | JSON |
| POST | `/api/v1/trace` | SSE |
| POST | `/api/v1/trace/json` | JSON |
| POST | `/api/v1/blast` | JSON |
| POST | `/api/v1/agent/chat` | SSE |
| POST | `/api/v1/flow` | JSON |
| POST | `/api/v1/history` | JSON |
| POST | `/api/v1/find-root` | SSE |
| POST | `/api/v1/api/discover` | JSON |
| POST | `/api/v1/api/spec` | JSON |
| GET | `/api/v1/nodes/lookup` | JSON |
| GET | `/api/v1/nodes/by-file` | JSON |
| GET | `/api/v1/nodes/:id/neighborhood` | JSON |

### Middleware

1. Panic recovery (`gin.Recovery`)
2. Request ID (`requestid.New`)
3. Structured logging (`SlogMiddleware`)
4. CORS (`cors.New`, configurable origins)

### Error Mapping

Domain error codes map to HTTP status codes: `not_found` to 404, `validation` to 400, `conflict` to 409, `rate_limit` to 429, `timeout` to 408.

---

## 6. CLI

CLI commands use `internal/adapters/client/` (Resty v3) to call the server API.

Server URL is resolved from: `--server-url` flag, then `COMMIT0_SERVER_URL` environment variable, then `http://localhost:8080`.

| Command | HTTP method | Pattern |
|---------|------------|---------|
| `query` (agent mode) | POST /agent/chat | SSE stream |
| `query` (direct) | POST /query | JSON |
| `index` | POST /index, GET /index/:id | Async polling |
| `trace` | POST /trace/json | JSON |
| `blast` | POST /blast | JSON |
| `flow` | POST /flow | JSON |
| `find-root` | POST /find-root | SSE stream |
| `repo` | CRUD on /repos | JSON |

The `serve` and `db` commands do not use HTTP; they manage the server and database processes directly.

---

## 7. Error Handling

Domain errors are defined in `internal/domain/errors.go`:

| Code | HTTP status | Usage |
|------|-------------|-------|
| `not_found` | 404 | Missing symbol, repo, or job |
| `validation` | 400 | Invalid input |
| `conflict` | 409 | Duplicate resource |
| `rate_limit` | 429 | Provider throttling |
| `timeout` | 408 | Deadline exceeded |

Non-fatal errors in the index pipeline are logged and skipped. Query explanation failures return results without an explanation. Agent tool errors are surfaced to the LLM for recovery.

---

## 8. Configuration

Environment variables are loaded via Viper. A `.env` file in the working directory is read automatically.

| Group | Variables | Purpose |
|-------|----------|---------|
| Provider | `EMBED_PROVIDER`, `LLM_PROVIDER` | Select embedding and LLM backends |
| Dimension | `EMBED_DIM` | HNSW vector index dimension (default 1024) |
| SurrealDB | `SURREAL_URL`, `SURREAL_USER`, `SURREAL_PASS`, `SURREAL_NAMESPACE`, `SURREAL_DATABASE` | Database connection |
| Gemini | `GEMINI_API_KEY`, `GEMINI_EMBED_MODEL`, `GEMINI_EXPLAIN_MODEL` | Gemini provider config |
| OpenRouter | `OPENROUTER_API_KEY`, `OPENROUTER_MODEL`, `OPENROUTER_MAX_TOKENS` | OpenRouter provider config |
| Voyage | `VOYAGE_API_KEY`, `VOYAGE_MODEL` | Voyage provider config |
| Ollama | `OLLAMA_URL`, `OLLAMA_MODEL`, `OLLAMA_EMBED_MODEL` | Local model config |
| Server | `SERVER_PORT`, `SERVER_CORS_ORIGINS`, `SERVER_READ_TIMEOUT_SEC` | HTTP server settings |

---

## 9. Testing

| Level | Location | Approach |
|-------|----------|----------|
| Unit | `internal/app/*_test.go` | In-memory stubs (`stubs_test.go`) |
| HTTP handlers | `internal/adapters/http/handlers_test.go` | `httptest.NewRecorder` with Gin test context |
| HTTP clients | `internal/adapters/openrouter/client_test.go` | `httptest.NewServer` |
| Integration | `internal/adapters/*/` | Requires running SurrealDB and API keys |
| Compile-time | Adapter files | `var _ domain.OpenCodeGraph = (*adapter)(nil)` |
