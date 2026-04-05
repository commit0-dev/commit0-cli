---
name: commit0-surrealdb
description: SurrealDB 3.0 for commit0. TRIGGER when: writing SurrealQL, schema definitions, HNSW vector indexes, BM25 full-text search, RELATE edges, the SurrealDB Go adapter, transactions, DEFINE API, changefeeds, or assets/schema.surql. DO NOT TRIGGER for pure Go or Gemini code.
---

# commit0 SurrealDB 3.0 Skill

---

## 3.0 Breaking Changes (from 2.x)

| 2.x — DO NOT USE | 3.0 — USE THIS |
|---|---|
| `MTREE DIMENSION 3072 DIST COSINE` | `HNSW DIMENSION 3072 DIST COSINE` |
| `SEARCH ANALYZER name BM25` | `FULLTEXT ANALYZER name BM25` |
| `<future> { expr }` | `COMPUTED expr` |
| `$var = value` (bare) | `LET $var = value` |
| `rand::guid()` | `rand::id()` |
| `type::thing()` | `type::record()` |
| `GROUP` + `SPLIT` together | Refactor to subqueries |
| `set` displays as `[]` | `set` deduplicates+orders, displays as `{}` |
| Empty array on missing table query | Error returned — handle in Go |
| Depth on edge: `->calls{1..N}->fn` | Depth on record: `$r.{1..N}(->calls->fn)` |

---

## Graph Traversal

### Single-hop (verified working in SurrealDB 3.0)

```sql
-- Forward: functions that $fn calls
SELECT id, name FROM $fn->calls->function;

-- Reverse: functions that call $fn
SELECT id, name FROM $fn<-calls<-function;

-- Bidirectional
SELECT id, name FROM $fn<->calls<->function;

-- Optional (no error if edge missing)
SELECT ->? AS edges FROM $fn;
```

### Multi-hop recursive — CRITICAL SYNTAX

**Depth goes on the RECORD using dot notation, NOT on the edge.**

```sql
-- Exactly N hops
SELECT id, name, qualified FROM $start.{3}(->calls->function);

-- Between N and M hops (most common)
SELECT id, name, qualified, language, file_path, repo_slug
FROM $target.{1..10}(<-calls<-function);

-- Unlimited depth (use carefully — can be slow)
SELECT id, name FROM $start.{..}(->calls->function);
```

### Path algorithms

```sql
-- Shortest path to a specific record
SELECT * FROM $start.{..+shortest=function:target}(->calls->function);

-- Collect all unique nodes reachable
SELECT * FROM $start.{..+collect}(->calls->function);

-- Collect all possible paths (returns path arrays)
SELECT * FROM $start.{..+path}(->calls->function);

-- Include the origin record in results
SELECT * FROM $start.{..+inclusive}(->calls->function);
```

### Reverse record references (3.0 only)

```sql
-- Get all functions belonging to a repo (via REFERENCE field)
SELECT <~function AS functions FROM repo:my_repo;
```

### Edge metadata in traversal

`->edge.field` / `<-edge.field` always returns an **array** — one value per matching edge on that node — even in single-hop queries. Do not unmarshal into a plain `string` field in Go; use `[]string` or omit from the query entirely.

```sql
-- Returns []string (array), not string — caller may call $fn from multiple sites
SELECT id, name, ->calls.call_site AS call_sites
FROM $fn<-calls<-function;

-- For multi-hop traversal: omit edge fields entirely
SELECT id, name, qualified, language, file_path, repo_slug
FROM $fn.{1..10}(<-calls<-function);
```

---

## Schema Patterns

### Key conventions

```sql
-- Computed fields: derived on read, zero storage
DEFINE FIELD centrality ON function COMPUTED
    count(<-calls<-function) + count(->calls->function);

-- REFERENCE: 1:N containment, cascades on delete
DEFINE FIELD repo ON file TYPE record<repo> REFERENCE ON DELETE CASCADE;

-- RELATE: edges with metadata (call_site, is_dynamic, etc.)
RELATE function:a->calls->function:b CONTENT { call_site: "file.go:42", ... };
```

**REFERENCE vs RELATE:**
- `REFERENCE`: containment, no edge metadata, auto-cascade (repo→file, file→function)
- `RELATE`: graph edge with metadata (calls, imports, inherits, uses)

### Edge tables used in commit0

| Edge | in → out | Key fields |
|---|---|---|
| `calls` | function → function | `call_site`, `call_type`, `is_dynamic`, `repo` |
| `imports` | file → module | `alias`, `is_wildcard` |
| `defines` | file/module → function/class | — |
| `inherits` | class → class | `kind` |
| `uses` | function → class | `usage_type` |
| `data_flow` | function → function | `call_site`, `param_name` |

---

## Vector ANN (HNSW)

```sql
-- Indexes: HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16
-- Tables: function, class, file, module

LET $q = <3072-dim float array>;

-- Basic ANN (10 nearest)
SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10|> $q;

-- With precision/speed tradeoff
SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10, 40|> $q;
```

---

## Full-Text Search (BM25)

```sql
-- Indexes: FULLTEXT ANALYZER code_analyzer BM25 on name, qualified, docstring

SELECT *, search::score(1) AS score
FROM function
WHERE name @1@ "validate" OR qualified @1@ "validate" OR docstring @1@ "validate"
ORDER BY score DESC
LIMIT 10;
```

---

## Hybrid Search (Vector + FTS + Graph Centrality)

Signature pattern — Reciprocal Rank Fusion:

```sql
LET $q_vec = <embedding>;
LET $q_text = "JWT validation";

SELECT
    *,
    vector::distance::knn() AS vec_dist,
    search::score(1) AS fts_score,
    centrality AS graph_weight,
    (1.0 / (60 + vec_dist)) AS rrf_vec,
    (1.0 / (60 + (1.0 - search::score(1)))) AS rrf_fts
FROM function
WHERE embedding <|20|> $q_vec
   OR (name @1@ $q_text OR qualified @1@ $q_text OR docstring @1@ $q_text)
ORDER BY (rrf_vec + rrf_fts) * math::log(1 + centrality) DESC
LIMIT 10;
```

---

## Namespace / Database

```sql
DEFINE NAMESPACE IF NOT EXISTS commit0;
USE NS commit0;
DEFINE DATABASE IF NOT EXISTS codebase STRICT;
USE DB codebase;
```

**Strict mode**: every table and field must be explicitly defined.

Full schema: `assets/schema.surql` (embedded in binary via `go:embed`).
