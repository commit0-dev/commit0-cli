# commit0 — Code Intelligence Pipeline

> How commit0 indexes, understands, and searches code. Source of truth for the indexing and retrieval architecture.

---

## 1. Why Not Just Vector Search?

| Problem | Example |
|---------|---------|
| Syntactic ≠ semantic | "TestAgentMemory" and "AgentMemoryStore" score identically for "agent memory management" |
| No concept reasoning | Can't distinguish "implements caching" from "mentions caching" |
| No structural awareness | Finds `Save()` but misses `Load()` and `Clear()` in the same class |
| Test pollution | Test fixtures and implementation score the same |

commit0's advantage: a **full code graph** (callers, callees, data flow, blast radius) used as a first-class retrieval signal alongside vector and keyword search.

---

## 2. Indexing Pipeline

```
WALK → PARSE → SUMMARIZE → EMBED → STORE → RE-EMBED
```

### Stage 1: Walk
Filesystem traversal with `.gitignore` awareness, language filtering, max file size. Yields `FileEntry` (path, content, language).

### Stage 2: Parse (tree-sitter)
AST extraction: functions, classes, modules → `CodeNode[]`. Call sites, imports, inheritance → `CodeEdge[]`. Docstring capture from adjacent comments. Per-language extractors: Go, Python, TypeScript, JavaScript.

### Stage 3: Summarize (LLM)
Batched LLM calls generate for each node:
- **Summary**: 1-paragraph semantic description of what the code does
- **Concepts**: 3-5 concept tags (e.g., `["authentication", "jwt", "middleware"]`)

Cached by SHA-256 content hash — only re-summarized when code changes. Cost: ~$0.001/function.

### Stage 4: Embed
Embedding text constructed with priority ordering (model weights earlier tokens more):

```
1. Task prefix + Kind + Qualified name
2. LLM-generated summary (MOST IMPORTANT)
3. Concept tags
4. Signature
5. Graph context (callers, callees)
6. --- separator ---
7. Code body (truncated if needed)
```

**Task prefix** (Gemini Embedding 2): `"task: code retrieval | document: ..."` at index time, `"task: code retrieval | query: ..."` at search time.

Batched: ≤100 inputs per API call. SHA-256 dedup skips unchanged nodes.

### Stage 5: Store
Transactional batch upsert to SurrealDB — all nodes + edges for a file committed atomically.

### Stage 6: Re-embed (Graph Context)
After initial store, re-embed with 1-hop neighborhood context (callers, callees, module). The resulting vector captures both *what the code does* and *how it fits in the codebase*.

### Concurrency Model

```
Walk (1 goroutine) → Parse (N=GOMAXPROCS) → Embed (M=4 API workers) → Store (P=4 DB workers)
```

Non-fatal errors skip + log + continue. Individual file failures don't abort the run.

---

## 3. Retrieval Pipeline

```
RETRIEVE (parallel) → EXPAND (graph) → RANK (RRF + boosts) → EXPLAIN (LLM)
```

### Stage 1: Retrieve (Parallel)

Three signals via `errgroup`:

| Signal | Source | Returns |
|--------|--------|---------|
| **Vector ANN** | HNSW index on embeddings | Semantic similarity scores |
| **BM25 FTS** | Full-text on name, qualified, summary, docstring | Keyword relevance scores |
| **Concept Match** | `WHERE concepts CONTAINS ANY $concepts` | Exact tag matches |

### Stage 2: Expand (Graph-Augmented)

For each top-K candidate:
- Fetch 1-hop callers + callees via `GetNeighborhood()`
- Add same-file sibling nodes (if `Save()` matches, include `Load()` and `Clear()`)
- Deduplicate expanded pool

### Stage 3: Rank (Multi-Signal Scoring)

**Reciprocal Rank Fusion:**
```
score = w_vector/(K + rank_vector) + w_fts/(K + rank_fts) + w_concept/(K + rank_concept)
```
Default weights: `w_vector=1.0, w_fts=0.8, w_concept=1.2` (concept is most precise signal).

**Post-fusion boosts:**
- Concept overlap: 2× if query concepts match node concepts
- Centrality: `1 + log(centrality+1) × 0.1`

**Optional LLM reranking** (`--rerank` flag): Top-30 candidates scored 0-10 by LLM for relevance.

### Stage 4: Explain (Structured Output)

Top-K results → Gemini structured JSON output. Per query type:

| Query Type | JSON Structure |
|------------|---------------|
| search | `{overview, evidence[{function, file, description}], insights[]}` |
| trace | `{overview, flow_steps[{hop, function, action, data_changes}], key_insights[]}` |
| blast | `{overview, severity, risk_areas[{function, risk, mitigation}], migration_steps[]}` |

---

## 4. Cost and Performance

| Operation | Cost | Latency |
|-----------|------|---------|
| Parse (tree-sitter) | Free | ~50ms/file |
| Summarize (LLM) | ~$0.001/function | ~200ms/batch |
| Embed (Gemini) | ~$0.0001/100 inputs | ~500ms/batch |
| Store (SurrealDB) | Free | ~10ms/node |
| Vector search | Free | ~50ms |
| BM25 search | Free | ~30ms |
| LLM explain | ~$0.01/query | ~2-4s |

**Typical 1000-function codebase:** Full index ~2-3 min. Incremental ~10-30s. Query ~500ms.

---

## 5. Implementation Files

| File | Role |
|------|------|
| `internal/app/index_service.go` | Pipeline orchestration |
| `internal/app/summarizer.go` | LLM batch summarization |
| `internal/app/context_builder.go` | Embedding text construction |
| `internal/app/embed_batcher.go` | Embedding batching + dedup |
| `internal/app/query_service.go` | Three-stage retrieval |
| `internal/app/fusion.go` | Reciprocal Rank Fusion |
| `internal/adapters/treesitter/lang/*.go` | AST extraction |
| `internal/adapters/gemini/embedder.go` | Gemini embedding with task prefix |
| `assets/schema.surql` | SurrealDB DDL |
