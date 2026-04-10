# commit0 — Database Design

> SurrealDB 3.0 schema, indexing strategy, query patterns, and performance tuning.

**Source of truth:** `assets/schema.surql` (embedded in binary via `go:embed`)
**Go SDK:** `surrealdb/surrealdb.go` · **Protocol:** WebSocket for streaming, HTTP for DEFINE API

---

## 1. Why SurrealDB 3.0

Unifies graph traversal, vector ANN search, and full-text search in one query language.

| Feature | commit0 Use Case |
|---|---|
| HNSW vector indexes | Memory-bounded ANN for code embeddings |
| Computed fields (`COMPUTED`) | Derived centrality metrics, evaluated on read |
| Record references (`REFERENCE`) | Bidirectional file→function navigation, cascading deletes |
| Client-side transactions | Atomic batch upsert of nodes + edges during indexing |
| Changefeeds | Incremental re-indexing — detect what changed since last run |
| Full-text search (BM25) | Hybrid symbol + natural language search |
| Strict mode | Schema enforcement — no accidental schemaless drift |
| DEFINE API | DB-native HTTP endpoints (query, trace, blast) |

---

## 2. Topology

```
Namespace: commit0
  └─ Database: codebase (STRICT mode)
       ├── Node tables:  repo, file, function, class, module
       ├── Edge tables:  calls, imports, defines, inherits, uses,
       │                 data_flow, reads, writes, route, control_flow, data_dep
       ├── System:       chat_session, chat_message, memory, schema_version
       └── Indexes:      HNSW vector, BM25 full-text, compound
```

---

## 3. Node Tables

| Table | Key Fields | Special Features |
|-------|-----------|-----------------|
| `repo` | slug (UNIQUE), path, languages, last_commit, last_indexed_at | `is_stale` COMPUTED field |
| `file` | path, repo (REFERENCE CASCADE), language, content_hash, embedding | Per-file content hashing |
| `function` | name, qualified (UNIQUE per repo), signature, body, embedding | `centrality` COMPUTED, `is_entry_point` COMPUTED |
| `class` | name, qualified, docstring, embedding | REFERENCE to file + repo |
| `module` | name, path, embedding | Package/module level |

**Computed fields** (evaluated on read, zero storage):
- `call_count` = `count(<-calls<-function)`
- `centrality` = `count(<-calls<-function) + count(->calls->function)`
- `is_entry_point` = no callers AND has callees

---

## 4. Edge Tables (13 Types)

| Edge | From → To | Key Fields |
|------|-----------|-----------|
| `calls` | function → function | call_site, is_dynamic, call_type |
| `imports` | file → module | alias |
| `defines` | file/module → function/class | — |
| `inherits` | class → class | kind (extends/implements/embeds) |
| `uses` | function → class | — |
| `data_flow` | function → function | field_path, mutation_kind, metadata |
| `reads` | function → field | — |
| `writes` | function → field | — |
| `route` | handler → function | method, path, middleware |
| `control_flow` | node → node | kind (if/else/loop/return) |
| `data_dep` | node → node | kind (def/use), variable |

---

## 5. Vector Indexes (HNSW)

```sql
DEFINE INDEX fn_vec_idx ON function FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;
```

| Parameter | Value | Rationale |
|---|---|---|
| DIMENSION | 3072 | Gemini Embedding 2 max fidelity |
| DIST | COSINE | Standard for normalized embeddings |
| TYPE | F32 | 50% memory savings over F64, negligible recall loss |
| EFC | 200 | Higher than default for better recall on code |
| M | 16 | Slightly above default — code embeddings have dense clusters |

**Memory estimate:** 100K functions × 3072 × 4 bytes = ~1.2 GB + ~20% HNSW overhead ≈ 1.4 GB

### Vector Query

```sql
-- ANN search with effort parameter
SELECT *, vector::distance::knn() AS dist
FROM function WHERE embedding <|10, 40|> $q_vec;
```

### Hybrid Search (Vector + FTS + Graph)

```sql
LET $q_vec = <embedding>;
SELECT *, vector::distance::knn() AS vec_dist, search::score(1) AS fts_score, centrality
FROM function
WHERE embedding <|20|> $q_vec
   OR (name @1@ $q_text OR qualified @1@ $q_text OR docstring @1@ $q_text)
ORDER BY (1.0 / (60 + vec_dist)) * math::log(1 + centrality) DESC
LIMIT 10;
```

---

## 6. Full-Text Search

```sql
DEFINE ANALYZER code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER nl_analyzer   TOKENIZERS blank, punctuation FILTERS lowercase, ascii, snowball(english);

DEFINE INDEX fn_name_fts ON function FIELDS name, qualified FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX fn_doc_fts  ON function FIELDS docstring FULLTEXT ANALYZER nl_analyzer BM25;
```

---

## 7. Graph Traversal Patterns

**Forward trace** (call chain):
```sql
SELECT ->calls->(function AS hop1), ->calls->calls->(function AS hop2) ... FROM function:$start_id;
```

**Reverse trace** (blast radius):
```sql
SELECT <-calls<-(function AS hop1), <-calls<-calls<-(function AS hop2) ... FROM function:$target_id;
```

**Cross-entity** (classes used by callers):
```sql
SELECT <-calls<-(function)->uses->(class) AS affected_types FROM function:$target_id;
```

**Semantic + structural** (similar AND in call neighborhood):
```sql
LET $neighbors = SELECT ->calls->function.id, <-calls<-function.id FROM function:$fn_id;
SELECT *, vector::similarity::cosine(embedding, $fn.embedding) AS sim
FROM function WHERE id IN $neighbors.ids AND sim > 0.75 ORDER BY sim DESC;
```

---

## 8. Transactions

Atomic batch upsert — all nodes and edges from a single file committed together via client-side transactions:

```go
tx, _ := db.BeginTransaction(ctx)
defer tx.Cancel(ctx)
// Upsert file, functions, classes, edges...
tx.Commit(ctx)
```

Snapshot isolation: concurrent workers on different files don't conflict.

---

## 9. Changefeeds (Incremental Re-indexing)

```sql
DEFINE TABLE function SCHEMAFULL CHANGEFEED 7d;
SHOW CHANGES FOR TABLE function SINCE $last_run_timestamp LIMIT 1000;
```

Flow: `git diff` → changed file paths → compare content_hash → re-parse → re-embed → upsert.

---

## 10. Session & Memory Storage

```sql
-- Chat sessions (multi-turn conversations)
DEFINE TABLE chat_session SCHEMAFULL;
-- Chat messages per session
DEFINE TABLE chat_message SCHEMAFULL;
-- Persistent memory with vector retrieval
DEFINE TABLE memory SCHEMAFULL;
DEFINE INDEX memory_vec_idx ON memory FIELDS embedding HNSW DIMENSION 3072 DIST COSINE TYPE F32;
```

---

## 11. Performance

### Capacity Planning

| Codebase | Functions | Edges | Vector Storage | Total DB |
|---|---|---|---|---|
| Small (10K LOC) | ~500 | ~2,000 | 6 MB | ~50 MB |
| Medium (100K LOC) | ~5,000 | ~20,000 | 60 MB | ~500 MB |
| Large (1M LOC) | ~50,000 | ~200,000 | 600 MB | ~5 GB |
| Monorepo (10M LOC) | ~500,000 | ~2,000,000 | 6 GB | ~50 GB |

### Optimization

- `REBUILD INDEX fn_vec_idx ON function;` — after bulk imports
- `EXPLAIN FULL SELECT ...` — verify index utilization
- Schema versioning via `schema_version` table with `ApplySchema()` in Go

---

## 12. Configuration

```bash
SURREAL_URL=ws://localhost:8000          # WebSocket
SURREAL_USER=root
SURREAL_PASS=root
SURREAL_NAMESPACE=commit0
SURREAL_DATABASE=codebase
```

See `internal/config/config.go` for full env var bindings.
