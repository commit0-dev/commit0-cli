# commit0 — Database Design

> SurrealDB 3.0 schema, indexing strategy, query patterns, and performance tuning.

**Source of truth:** `server/assets/schema.surql` (embedded in binary via `go:embed`)

---

## 1. Why SurrealDB 3.0

Unifies graph traversal, vector ANN search, and full-text search in one query language.

| Feature | commit0 Use Case |
|---|---|
| HNSW vector indexes | ANN search over code embeddings |
| Computed fields (`COMPUTED`) | Derived centrality metrics, entry point detection |
| Record references (`REFERENCE`) | Bidirectional navigation, cascading deletes |
| Client-side transactions | Atomic batch upsert of nodes + edges during indexing |
| Full-text search (BM25) | Hybrid symbol + natural language search |
| SCHEMALESS relations | Extensible edge types — no schema migration for new analysis techniques |
| Recursive traversal (`.{1..N}`) | Bounded graph walks with inline filtering |

---

## 2. Topology

```
Namespace: commit0
  Database: codebase
    Node tables (SCHEMAFULL):  repo, file, function, class, module
    Edge tables (SCHEMALESS):  calls, imports, defines, inherits, uses,
                               data_flow, reads, writes, route, control_flow, data_dep
    System tables:             chat_session, chat_message, memory,
                               commit_history, schema_version, peer, sync_scope
    Indexes:                   HNSW vector, BM25 full-text, compound
```

### Schema Strategy

- **Node tables** are SCHEMAFULL — they carry HNSW vector indexes and COMPUTED fields that require defined columns.
- **Edge tables** are SCHEMALESS — they store properties via `RELATE ... CONTENT $props`. New edge types are created automatically on first `RELATE` with no `DEFINE TABLE` needed.
- **Repo as graph node** — the `repo` table is a graph node with `REFERENCE ON DELETE CASCADE` on all other node tables. Deleting a repo cascades to all its nodes and edges.

---

## 3. Node Tables

| Table | Key Fields | Special Features |
|-------|-----------|-----------------|
| `repo` | slug, path, languages, last_commit, last_indexed_at | `is_stale` COMPUTED field, CASCADE delete |
| `file` | path, repo (REFERENCE), language, content_hash, embedding | Per-file content hashing |
| `function` | name, qualified, signature, body, embedding | `centrality` COMPUTED, `call_count` COMPUTED, `is_entry_point` COMPUTED |
| `class` | name, qualified, docstring, embedding | REFERENCE to file + repo |
| `module` | name, path, embedding | Package/module level |

### Computed Fields (evaluated on read)

```sql
centrality     = count(<-calls<-function) + count(->calls->function)
call_count     = count(<-calls<-function)
is_entry_point = count(<-calls<-function) == 0 AND count(->calls->function) > 0
```

---

## 4. Edge Tables (13 Types)

| Edge | From -> To | Key Properties |
|------|-----------|---------------|
| `calls` | function -> function | call_site, is_dynamic, call_type, repo |
| `imports` | file -> module | alias, repo |
| `defines` | file/module -> function/class | repo |
| `inherits` | class -> class | kind (extends/implements/embeds) |
| `uses` | function -> class | repo |
| `data_flow` | function -> function | field_path, mutation_kind, param_name, arg_expr |
| `reads` | function -> field | repo |
| `writes` | function -> field | repo |
| `route` | handler -> function | http_method, http_path, middleware |
| `control_flow` | node -> node | branch_type, condition, from_line, to_line |
| `data_dep` | node -> node | var_name, def_line, use_line, def_type |

All edge tables are SCHEMALESS with only `in`/`out` indexes. Properties are stored as arbitrary key-value pairs via `RELATE ... CONTENT`.

---

## 5. Vector Indexes (HNSW)

```sql
DEFINE INDEX fn_vec_idx ON function FIELDS embedding
    HNSW DIMENSION $dim DIST COSINE TYPE F32 EFC 200 M 16;
```

The dimension (`$dim`) is set by `EMBED_DIM` config (default 1024). All embedding providers normalize to this dimension.

| Parameter | Value | Rationale |
|---|---|---|
| DIST | COSINE | Standard for normalized embeddings |
| TYPE | F32 | 50% memory savings over F64 |
| EFC | 200 | Higher recall for code embedding clusters |
| M | 16 | Slightly above default for dense clusters |

### Vector Query

```sql
SELECT *, vector::distance::knn() AS dist
FROM function WHERE embedding <|10, 40|> $q_vec;
```

---

## 6. Full-Text Search (BM25)

```sql
DEFINE ANALYZER code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER nl_analyzer   TOKENIZERS blank, punctuation FILTERS lowercase, ascii, snowball(english);

DEFINE INDEX fn_name_fts ON function FIELDS name, qualified FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX fn_doc_fts  ON function FIELDS docstring FULLTEXT ANALYZER nl_analyzer BM25;
```

---

## 7. Graph Traversal

Label-parameterized traversal is the core query pattern. All analysis techniques use the same mechanism with different edge label sets.

**Forward trace** (call chain):
```sql
SELECT *.{1..5}(->calls->function) FROM type::record($start);
```

**Reverse trace** (blast radius):
```sql
SELECT *.{1..5}(<-calls<-function) FROM type::record($start);
```

**Data flow trace** (field tracking):
```sql
SELECT *.{1..10}(->data_flow(WHERE field_path = $field)->function) FROM type::record($start);
```

**Multi-label traversal** is done via parallel per-label queries merged client-side, since SurrealDB does not support `(->edge_a | ->edge_b)` in one recursive expression.

---

## 8. Batch Operations

Atomic batch upsert — all nodes and edges from a single file committed together:

```go
tx, _ := db.BeginTransaction(ctx)
defer tx.Cancel(ctx)
// Upsert file, functions, classes, edges...
tx.Commit(ctx)
```

Generic edge storage — one query template for all edge types:

```go
q := fmt.Sprintf("RELATE $from->%s->$to CONTENT $props;", edge.Label)
```

---

## 9. Session and Memory Storage

```sql
DEFINE TABLE chat_session SCHEMAFULL;
DEFINE TABLE chat_message SCHEMAFULL;
DEFINE TABLE memory SCHEMAFULL;
DEFINE INDEX memory_vec_idx ON memory FIELDS embedding HNSW DIMENSION $dim DIST COSINE TYPE F32;
```

---

## 10. Configuration

```bash
SURREAL_URL=ws://localhost:8000
SURREAL_USER=root
SURREAL_PASS=root
SURREAL_NAMESPACE=commit0
SURREAL_DATABASE=codebase
```

See `internal/config/config.go` for full env var bindings.
