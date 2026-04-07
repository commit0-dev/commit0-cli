# commit0 — Code Intelligence Pipeline Architecture

> This document defines the indexing, embedding, and retrieval architecture.
> It is the source of truth for how commit0 understands and searches code.

---

## 1. Problem Statement

Traditional code search treats source code as text — matching keywords and computing vector similarity on raw code tokens. This fails because:

- **Syntactic matching ≠ semantic understanding**: "TestAgentMemory" and "AgentMemoryStore" score identically for a query about "agent memory management"
- **No concept-level reasoning**: embeddings can't distinguish "code that implements caching" from "code that mentions caching"
- **No structural awareness**: flat vector search ignores the call graph — finding `Save()` without finding `Load()` and `Clear()` alongside it
- **Test fixtures pollute results**: test code and implementation code score the same when they share keywords

### What Industry Leaders Do Differently

| Tool | Approach | Key Insight |
|------|----------|-------------|
| **Cursor** | Custom embedding model trained on agent session traces | Learn what code is *useful* for answering questions, not just textually similar |
| **Windsurf** | M-Query: parallel LLM calls per candidate instead of vector similarity | LLM reasoning > vector distance for relevance |
| **Sourcegraph Cody** | Code graph (SCIP) + multi-component ranking | Structural relationships are as important as text similarity |
| **GitHub Copilot** | Symbol-aware indexing + dynamic context selection | Index at the symbol level, not file level |

### commit0's Advantage

commit0 already has a **full code graph** (callers, callees, data flow, blast radius) that none of the above tools have at this depth. The architecture below leverages this graph as a first-class retrieval signal.

---

## 2. Indexing Pipeline

### Overview

```
                    ┌─────────┐
                    │  WALK   │  Traverse filesystem, respect .gitignore
                    └────┬────┘
                         │ FileEntry (path, content, language)
                         ▼
                    ┌─────────┐
                    │  PARSE  │  tree-sitter AST extraction
                    └────┬────┘
                         │ CodeNode[] + CodeEdge[] + Docstrings
                         ▼
                    ┌─────────┐
                    │SUMMARIZE│  LLM generates semantic description
                    └────┬────┘  (batched, cached by content hash)
                         │ CodeNode[] with Summary + Concepts
                         ▼
                    ┌─────────┐
                    │  EMBED  │  Gemini Embedding 2 with semantic context
                    └────┬────┘
                         │ CodeNode[] with Embedding vectors
                         ▼
                    ┌─────────┐
                    │  STORE  │  SurrealDB: nodes, edges, vectors, indexes
                    └────┬────┘
                         │
                         ▼
                    ┌─────────┐
                    │RE-EMBED │  Re-embed with graph neighborhood context
                    └─────────┘  (callers, callees, data flow)
```

### Stage 1: Walk

**Component:** `internal/adapters/walker/fs_walker.go`

- Recursively traverses the repository directory
- Respects `.gitignore` patterns
- Filters by configured languages and file extensions
- Skips `node_modules`, `.git`, `__pycache__`, vendor directories
- Outputs `FileEntry` structs via buffered channel

### Stage 2: Parse

**Component:** `internal/adapters/treesitter/parser.go` + `lang/*.go`

Uses tree-sitter to extract AST nodes:

| Extracted | Source | Example |
|-----------|--------|---------|
| Functions | Function/method declarations | `func HandleRequest(...)` |
| Classes | Type/class/struct declarations | `type UserService struct` |
| Files | Entire source files | `handler.go` |
| Modules | Package/import declarations | `package auth` |
| Edges | Call sites, imports, inheritance | `A calls B`, `A imports M` |
| **Docstrings** | Preceding comment nodes | JSDoc `/** */`, GoDoc `//`, Python `"""` |

**Docstring extraction** (critical for semantic quality):
- Go: comment group preceding function declaration
- TypeScript/JavaScript: JSDoc comment (`/** ... */`) above function
- Python: first string literal in function body (`"""..."""`)

**Output:** `ParsedFile` containing `[]CodeNode` and `[]CodeEdge`

### Stage 3: Summarize (LLM)

**Component:** `internal/app/summarizer.go`

For each function/class with a body > 3 lines, call the LLM to generate:

```json
{
  "summary": "HTTP request handler that authenticates users via JWT token validation, retrieves user data from the database, and returns a JSON response.",
  "concepts": ["authentication", "http-handler", "jwt", "user-management"],
  "category": "api-handler"
}
```

**Design:**
- **Batched**: 10-20 functions per LLM call to reduce API round-trips
- **Cached**: Skip if `ContentHash` matches the previously summarized version
- **Concurrent**: `errgroup.SetLimit(4)` for parallel LLM calls
- **Structured output**: Uses Gemini's `response_json_schema` for consistent format
- **Fallback**: If LLM fails, fall back to docstring or auto-generated metadata
- **Cost control**: Skip trivial functions (< 3 lines, getters, setters)

**Prompt template:**
```
You are a senior engineer. For each function below, write a one-paragraph summary
of what it does, what problem it solves, and what architectural concepts it implements.
Also list 3-5 semantic concept tags.

Function: {qualified_name}
File: {file_path}:{start_line}-{end_line}
Signature: {signature}
Docstring: {docstring}
Body:
{body}
```

### Stage 4: Embed

**Component:** `internal/app/embed_batcher.go` + `context_builder.go`

The embedding input is constructed by `ContextBuilder.ForNode()`:

```
task: code retrieval | document: [FUNCTION] pkg.HandleRequest — HTTP request handler
that authenticates users via JWT token validation, retrieves user data from the database.
Concepts: authentication, http-handler, jwt, user-management.
Signature: func(ctx context.Context, req *http.Request) error
---
func HandleRequest(ctx context.Context, req *http.Request) error {
    token := ctx.Value("auth_token").(string)
    ...
}
```

**Critical design decisions:**

1. **Task prefix alignment**: Both documents and queries use `task: code retrieval |` prefix. Documents use `document:`, queries use `query:`. This places them in the same semantic space per Gemini Embedding 2's task-based retrieval model.

2. **Summary leads the text**: The LLM-generated summary is the FIRST thing the embedding model sees, not buried after metadata. This ensures the embedding captures *what the code does*, not just *where it is*.

3. **Concepts as searchable text**: Concept tags are included in the embedding text so vector similarity naturally captures concept-level matches.

4. **Body included but secondary**: Raw code body is after the `---` separator. The embedding model still sees it but the summary dominates the vector.

**Batching:** Up to 100 inputs per `EmbedBatch` call. SHA-256 content hash for deduplication.

### Stage 5: Store

**Component:** `internal/adapters/surreal/graph_store.go`

Writes to SurrealDB:
- **Node tables**: `function`, `class`, `file`, `module` — with `embedding`, `summary`, `concepts` fields
- **Edge tables**: `calls`, `imports`, `defines`, `inherits`, `uses`, `data_flow`, `reads`, `writes`
- **Indexes**: HNSW vector index on `embedding` field, BM25 full-text on `name`, `qualified`, `summary`
- **Computed fields**: `centrality`, `call_count`, `callee_count`, `is_leaf`, `is_entry_point`

### Stage 6: Re-Embed with Graph Context

**Component:** `internal/app/index_service.go` (re-embed pass)

After all nodes and edges are stored, re-embed each node with its graph neighborhood:

```
task: code retrieval | document: [FUNCTION] pkg.HandleRequest — HTTP request handler...
Callers: main.setupRoutes (route registration), TestHandler (unit test)
Callees: auth.ValidateToken (JWT validation), db.GetUser (database query), http.WriteJSON (response writer)
Data flow to: sessionStore.Set (session persistence)
Reads: req.Header.Authorization, ctx.Value("auth_token")
```

The re-embedding adds **structural context** that the initial embedding lacks because edges weren't yet stored.

---

## 3. Retrieval Pipeline

### Overview: Three-Stage Retrieval

```
User Query: "How does agent memory management work?"
                         │
                         ▼
              ┌──────────────────────┐
              │   STAGE 1: RETRIEVE  │  Parallel multi-signal search
              │                      │
              │  ┌─ Vector ANN ────┐ │  Semantic similarity via HNSW
              │  ├─ BM25 FTS ─────┤ │  Keyword/term matching
              │  └─ Concept Match ─┘ │  Tag-based: WHERE concepts CONTAINS ANY [...]
              └──────────┬───────────┘
                         │ Candidates (union, deduplicated)
                         ▼
              ┌──────────────────────┐
              │   STAGE 2: EXPAND    │  Graph-augmented context
              │                      │
              │  For each top-K:     │
              │  + 1-hop callers     │
              │  + 1-hop callees     │
              │  + same-file nodes   │
              └──────────┬───────────┘
                         │ Expanded candidate pool
                         ▼
              ┌──────────────────────┐
              │   STAGE 3: RANK      │  Multi-signal scoring
              │                      │
              │  RRF fusion          │
              │  + concept boost     │
              │  + centrality boost  │
              │  + (optional) LLM    │
              │    reranking         │
              └──────────┬───────────┘
                         │ Final top-K results
                         ▼
              ┌──────────────────────┐
              │   STAGE 4: EXPLAIN   │  LLM structured explanation
              │                      │
              │  Gemini structured   │
              │  output with JSON    │
              │  schema per query    │
              │  type (search/trace/ │
              │  blast)              │
              └──────────────────────┘
```

### Stage 1: Retrieve (Parallel)

Three retrieval signals run in parallel via `errgroup`:

**A. Vector ANN Search**
- Embed the query: `"task: code retrieval | query: How does agent memory management work?"`
- Search HNSW index in SurrealDB for nearest neighbors
- Returns `[]ScoredNode` with `VectorScore`

**B. BM25 Full-Text Search**
- Search against `name`, `qualified`, `summary`, `docstring` fields
- Returns `[]ScoredNode` with `FTSScore`

**C. Concept Tag Match** (new)
- Extract concept keywords from the query (simple: split + lowercase + dedup)
- Query SurrealDB: `SELECT * FROM function WHERE concepts CONTAINS ANY $concepts AND repo = $repo`
- Returns nodes with exact concept matches
- Injected into fusion as a third signal with dedicated weight

### Stage 2: Expand (Graph-Augmented)

For each node in the top-K after fusion:

1. Fetch 1-hop callers via `GraphStore.GetNeighborhood()`
2. Fetch 1-hop callees
3. Add any same-file nodes (if `MemoryStore.Save` matches, include `MemoryStore.Load`, `MemoryStore.Clear`)
4. Deduplicate the expanded pool
5. Each expanded node carries its relationship to the original match (caller/callee/sibling)

This ensures that when the user asks about "memory management" and we find `MemoryStore.Save()`, we also surface `MemoryStore.Load()`, `MemoryStore.Clear()`, and their callers — providing complete architectural context.

### Stage 3: Rank (Multi-Signal Scoring)

**Reciprocal Rank Fusion (existing, enhanced):**

```
score(node) = w_vector / (K + rank_vector)
            + w_fts    / (K + rank_fts)
            + w_concept / (K + rank_concept)   ← NEW signal

where K = 60 (default)
```

**Default weights:** `w_vector=1.0, w_fts=0.8, w_concept=1.2`

Concept match gets the highest weight because it's the most precise signal — if a node is tagged with `["memory-management", "caching"]` and the query is about memory management, that's a near-certain match.

**Post-fusion boosts:**

```
final_score = fused_score
            * concept_boost   // 2x if query concepts overlap node concepts
            * centrality_boost // 1 + log(centrality+1) * 0.1
```

**Optional LLM Reranking** (enabled via `--rerank` flag):

For the top-30 candidates after fusion, ask the LLM:
```json
{
  "prompt": "Score 0-10: Is this code relevant to 'How does agent memory management work?'",
  "code": "{node.Summary}\n{node.Signature}",
  "response_schema": { "score": "integer", "reason": "string" }
}
```

Final score when reranking is enabled:
```
final = llm_score * 0.6 + fused_score * 0.3 + centrality * 0.1
```

### Stage 4: Explain (Structured LLM Output)

After retrieval and ranking, the top-K results are sent to the LLM for explanation using Gemini's structured output (`response_json_schema`):

**Search query response:**
```json
{
  "overview": "Agent memory management in OpenCode is handled by the MemoryStore class...",
  "evidence": [
    { "function": "MemoryStore.Save", "file": "agent/memory.ts:42", "description": "Persists agent state to disk..." },
    { "function": "MemoryStore.Load", "file": "agent/memory.ts:78", "description": "Restores agent state from cache..." }
  ],
  "insights": ["The caching strategy uses LRU eviction...", "Memory is synced on every 10th operation..."]
}
```

**Trace query response:**
```json
{
  "overview": "The call chain flows from HTTP handler through auth to database...",
  "flow_steps": [
    { "hop": 1, "function": "handleRequest", "action": "Validates JWT token", "data_changes": "Extracts user ID from claims" }
  ],
  "key_insights": ["Error handling at hop 3 swallows database timeouts"]
}
```

**Blast query response:**
```json
{
  "overview": "Changing handleRequest impacts 4 downstream components...",
  "severity": "high",
  "risk_areas": [
    { "function": "authMiddleware", "risk": "Depends on request context format", "mitigation": "Update context key extraction" }
  ],
  "migration_steps": ["1. Update authMiddleware first...", "2. Then update route handlers..."]
}
```

---

## 4. Data Model

### CodeNode Fields

| Field | Type | Source | Used By |
|-------|------|--------|---------|
| ID | string | tree-sitter | All |
| Name | string | tree-sitter | FTS, display |
| Qualified | string | tree-sitter | FTS, lookup |
| Kind | NodeKind | tree-sitter | Filtering |
| FilePath | string | tree-sitter | Navigation |
| Signature | string | tree-sitter | Display, embedding |
| Body | string | tree-sitter | Embedding, summarization |
| Docstring | string | tree-sitter (comments) | Embedding, summarization |
| **Summary** | string | **LLM** | **Embedding, FTS, display** |
| **Concepts** | []string | **LLM** | **Concept retrieval, boosting** |
| Embedding | []float32 | Gemini | Vector ANN search |
| ContentHash | string | SHA-256(body) | Cache invalidation |
| StartLine | int | tree-sitter | Navigation |
| EndLine | int | tree-sitter | Navigation |
| Language | string | walker | Filtering |
| Visibility | string | tree-sitter | Filtering |
| Centrality | int (computed) | SurrealDB | Ranking boost |

### Edge Types

| Edge | Meaning | Example |
|------|---------|---------|
| `calls` | Function A calls function B | `handleRequest → validateToken` |
| `imports` | File/module imports another | `handler.go → auth` |
| `defines` | Class defines a method | `UserService → Create` |
| `inherits` | Class extends another | `AdminUser → User` |
| `uses` | Function uses a type | `handleRequest → UserService` |
| `data_flow` | Data passes from A to B | `password → hashPassword` |
| `reads` | Function reads a field | `Login reads User.Email` |
| `writes` | Function writes a field | `Register writes User.ID` |

### SurrealDB Schema Additions

```sql
-- Semantic fields on node tables
DEFINE FIELD OVERWRITE summary   ON `function` TYPE option<string>;
DEFINE FIELD OVERWRITE concepts  ON `function` TYPE option<array<string>>;
DEFINE FIELD OVERWRITE summary   ON class TYPE option<string>;
DEFINE FIELD OVERWRITE concepts  ON class TYPE option<array<string>>;
DEFINE FIELD OVERWRITE summary   ON file TYPE option<string>;
DEFINE FIELD OVERWRITE concepts  ON file TYPE option<array<string>>;

-- Full-text search on summary for BM25
DEFINE ANALYZER OVERWRITE summary_analyzer TOKENIZERS blank, class FILTERS lowercase, snowball(english);
DEFINE INDEX OVERWRITE fn_summary_ft ON `function` FIELDS summary SEARCH ANALYZER summary_analyzer BM25;
DEFINE INDEX OVERWRITE cls_summary_ft ON class FIELDS summary SEARCH ANALYZER summary_analyzer BM25;
```

---

## 5. Embedding Strategy

### Task Prefix Alignment

Per Gemini Embedding 2 documentation, task-based prefixes place vectors in aligned semantic spaces:

| Context | Prefix | Example |
|---------|--------|---------|
| Document (index time) | `task: code retrieval \| document:` | `task: code retrieval \| document: [FUNCTION] HandleRequest — HTTP handler that authenticates...` |
| Query (search time) | `task: code retrieval \| query:` | `task: code retrieval \| query: How does authentication work?` |

Both prefixed identically (`task: code retrieval`) so they land in the same embedding sub-space.

### Embedding Content Priority

The embedding text is structured so the most semantic information comes first:

```
1. Task prefix + Kind + Qualified name
2. LLM-generated summary (MOST IMPORTANT — what the code DOES)
3. Concept tags
4. Signature
5. Graph context (callers, callees)
6. --- separator ---
7. Code body (truncated if needed)
```

The embedding model weights earlier tokens more heavily, so putting the semantic summary first ensures the vector captures meaning, not just code syntax.

### Dimensions and Model

- **Model**: `gemini-embedding-2-preview` (or latest stable)
- **Dimensions**: 3072 (Gemini default) or 1024 (Voyage fallback)
- **Batch size**: 100 inputs per API call
- **Deduplication**: SHA-256 content hash — skip embedding if hash matches stored version

---

## 6. Cost and Performance

| Operation | Cost | Latency |
|-----------|------|---------|
| Parse (tree-sitter) | Free (local) | ~50ms/file |
| Summarize (LLM) | ~$0.001/function (Gemini Flash) | ~200ms/batch of 20 |
| Embed (Gemini) | ~$0.0001/100 inputs | ~500ms/batch of 100 |
| Store (SurrealDB) | Free (local) | ~10ms/node |
| Vector search | Free (local) | ~50ms |
| BM25 search | Free (local) | ~30ms |
| Concept match | Free (local) | ~20ms |
| LLM rerank (optional) | ~$0.005/query | ~1-2s |
| LLM explain | ~$0.01/query | ~2-4s |

**Typical codebase (1000 functions):**
- Full index: ~2-3 minutes (dominated by LLM summarization)
- Incremental re-index: ~10-30 seconds (only changed files)
- Query latency: ~500ms without rerank, ~2s with rerank

---

## 7. Implementation Files

| File | Role |
|------|------|
| `internal/app/index_service.go` | Pipeline orchestration: Walk→Parse→Summarize→Embed→Store→Re-embed |
| `internal/app/summarizer.go` | LLM batch summarization with structured output |
| `internal/app/context_builder.go` | Embedding text construction with summary + graph context |
| `internal/app/embed_batcher.go` | Gemini embedding batching with deduplication |
| `internal/app/query_service.go` | Three-stage retrieval: Retrieve→Expand→Rank→Explain |
| `internal/app/fusion.go` | Reciprocal Rank Fusion with concept signal |
| `internal/adapters/treesitter/lang/*.go` | AST extraction with docstring capture |
| `internal/adapters/gemini/embedder.go` | Gemini Embedding 2 with task prefix alignment |
| `internal/adapters/gemini/explainer.go` | Structured LLM explanation |
| `internal/adapters/gemini/schema.go` | JSON schemas for structured output |
| `internal/adapters/surreal/graph_store.go` | SurrealDB CRUD + graph traversal |
| `internal/adapters/surreal/vector_index.go` | HNSW ANN search |
| `internal/adapters/surreal/text_index.go` | BM25 full-text search |
| `internal/domain/ports.go` | Port interfaces for all adapters |
| `pkg/types/ast.go` | CodeNode, CodeEdge types with Summary + Concepts |
| `pkg/types/result.go` | QueryResult, TraceResult, BlastResult with structured explanations |
| `assets/schema.surql` | SurrealDB DDL with summary/concepts fields + FTS indexes |
