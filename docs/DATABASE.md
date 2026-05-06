# Database Design

SurrealDB 3.0 schema, indexes, and query patterns.

**Schema source of truth:** `server/assets/schema.surql` (embedded in the binary via `go:embed`)

---

## 1. Why SurrealDB

SurrealDB provides graph traversal, HNSW vector search, and BM25 full-text search in a single query language. This avoids running separate graph, vector, and search databases.

| Feature | Usage |
|---------|-------|
| HNSW vector indexes | Approximate nearest-neighbor search over code embeddings |
| COMPUTED fields | Derived metrics (centrality, entry point detection) evaluated on read |
| REFERENCE constraints | Cascading deletes when a repository is removed |
| Client-side transactions | Atomic batch upsert during indexing |
| BM25 full-text search | Symbol name and docstring search |
| SCHEMALESS relations | New edge types created without schema migration |
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
```

**Node tables** are SCHEMAFULL to support HNSW vector indexes and COMPUTED fields.

**Edge tables** are SCHEMALESS. Properties are stored via `RELATE ... CONTENT $props`. New edge types are created automatically on the first `RELATE` statement.

**The `repo` table** uses `REFERENCE ON DELETE CASCADE` so that deleting a repository removes all associated nodes and edges.

---

## 3. Node Tables

| Table | Key columns | Notes |
|-------|------------|-------|
| `repo` | slug, path, languages, last_indexed_at | `is_stale` COMPUTED field |
| `file` | path, repo (REFERENCE), language, content_hash, embedding | Content hash for deduplication |
| `function` | name, qualified, signature, body, embedding | `centrality` and `call_count` COMPUTED fields |
| `class` | name, qualified, docstring, embedding | References file and repo |
| `module` | name, path, embedding | Package-level node |

COMPUTED fields are evaluated on read and do not consume storage:

```sql
centrality     = count(<-calls<-function) + count(->calls->function)
call_count     = count(<-calls<-function)
is_entry_point = count(<-calls<-function) == 0 AND count(->calls->function) > 0
```

---

## 4. Edge Tables

| Edge | Direction | Key properties |
|------|-----------|---------------|
| `calls` | function to function | call_site, is_dynamic, call_type |
| `imports` | file to module | alias |
| `defines` | file/module to function/class | |
| `inherits` | class to class | kind (extends, implements, embeds) |
| `uses` | function to class | |
| `data_flow` | function to function | field_path, mutation_kind, param_name, arg_expr |
| `reads` | function to field | |
| `writes` | function to field | |
| `route` | handler to function | http_method, http_path, middleware |
| `control_flow` | node to node | branch_type, condition, from_line, to_line |
| `data_dep` | node to node | var_name, def_line, use_line |

All edge tables have `in`/`out` indexes only. Properties are stored as key-value pairs.

---

## 5. Vector Indexes

```sql
DEFINE INDEX fn_vec_idx ON function FIELDS embedding
    HNSW DIMENSION 1024 DIST COSINE TYPE F32 EFC 200 M 16;
```

The dimension is set by the `EMBED_DIM` configuration value (default 1024). All embedding providers normalize to this dimension.

| Parameter | Value | Notes |
|-----------|-------|-------|
| DIST | COSINE | Standard for normalized embeddings |
| TYPE | F32 | Reduces memory by half compared to F64 |
| EFC | 200 | Increases recall at the cost of build time |
| M | 16 | Controls graph connectivity during index construction |

Query example:

```sql
SELECT *, vector::distance::knn() AS dist
FROM function WHERE embedding <|10, 40|> $query_vec;
```

---

## 6. Full-Text Search

Two analyzers are defined for different field types:

```sql
DEFINE ANALYZER code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER nl_analyzer   TOKENIZERS blank, punctuation FILTERS lowercase, ascii, snowball(english);

DEFINE INDEX fn_name_fts ON function FIELDS name, qualified FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX fn_doc_fts  ON function FIELDS docstring FULLTEXT ANALYZER nl_analyzer BM25;
```

`code_analyzer` splits on camelCase boundaries and whitespace. `nl_analyzer` applies English stemming for natural-language fields.

---

## 7. Graph Traversal

All analysis techniques use label-parameterized traversal with different edge label sets.

Forward trace (call chain):
```sql
SELECT *.{1..5}(->calls->function) FROM type::record($start);
```

Reverse trace (callers):
```sql
SELECT *.{1..5}(<-calls<-function) FROM type::record($start);
```

Data flow with field filter:
```sql
SELECT *.{1..10}(->data_flow(WHERE field_path = $field)->function) FROM type::record($start);
```

SurrealDB does not support multi-table traversal (`->a | ->b`) in one recursive expression. The adapter runs parallel per-label queries and merges results on the client side.

---

## 8. Batch Operations

Nodes and edges from a single file are committed atomically:

```go
tx, _ := db.BeginTransaction(ctx)
defer tx.Cancel(ctx)
// upsert file node, function nodes, class nodes, edges
tx.Commit(ctx)
```

Edge storage uses a single query template for all edge types:

```go
q := fmt.Sprintf("RELATE $from->%s->$to CONTENT $props;", edge.Label)
```

---

## 9. System Tables

```sql
DEFINE TABLE chat_session SCHEMAFULL;
DEFINE TABLE chat_message SCHEMAFULL;
DEFINE TABLE memory SCHEMAFULL;
DEFINE INDEX memory_vec_idx ON memory FIELDS embedding HNSW DIMENSION 1024 DIST COSINE TYPE F32;
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

See `internal/config/config.go` for the full set of environment variables.
