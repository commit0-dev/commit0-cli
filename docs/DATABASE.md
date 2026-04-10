# commit0 — Database Design Document

> SurrealDB 3.0 schema, indexing strategy, query patterns, and API layer for the
> commit0 code knowledge graph.

**Target version:** SurrealDB 3.0.x (GA February 2026)
**Go SDK:** `surrealdb/surrealdb.go/v2` (1.0 stable, 3.0-compatible)
**Protocol:** WebSocket (`ws://`) for streaming; HTTP for DEFINE API endpoints

---

## 1. Why SurrealDB 3.0

commit0 chose SurrealDB because it unifies graph traversal, vector ANN search, and
full-text search in a single query language (SurrealQL). Version 3.0 deepens this
advantage with capabilities that map directly to commit0's needs:

| SurrealDB 3.0 Feature | commit0 Use Case |
|---|---|
| HNSW vector indexes (replaces MTREE) | Faster, memory-bounded ANN for code embeddings |
| Computed fields (`COMPUTED`) | Derived metrics (call count, centrality) evaluated on read |
| Record references (`REFERENCE`) | Bidirectional file-to-function navigation without manual joins |
| DEFINE API endpoints | Expose query/trace/blast as DB-native HTTP endpoints |
| Client-side transactions | Atomic batch upsert of nodes + edges during indexing |
| Changefeeds | Incremental re-indexing — detect what changed since last run |
| Full-text search (OR ops, compound indexes) | Hybrid symbol + natural language search |
| DEFINE BUCKET (file storage) | Cache embedding inputs, store parsed AST snapshots |
| Synced writes (default) | Durability guarantee — no silent data loss on crash |
| Strict mode per database | Schema enforcement at the DB level, not just per table |
| GraphQL (stable) | Alternative query interface for future web UI |
| Surrealism extensions (WASM) | Custom scoring functions, language-specific analyzers |

### 1.1 Migration from 2.x

The ARCHITECTURE.md references SurrealDB 2.x. The following breaking changes
require updates to `assets/schema.surql` and `internal/graph/`:

| 2.x Syntax | 3.0 Replacement | Affected File |
|---|---|---|
| `MTREE DIMENSION 3072 DIST COSINE` | `HNSW DIMENSION 3072 DIST COSINE` | `schema.surql` |
| `SEARCH ANALYZER code_analyzer BM25` | `FULLTEXT ANALYZER code_analyzer BM25` | `schema.surql` |
| `<future> { ... }` | `COMPUTED expression` | `schema.surql` |
| Bare `$var = value` | `LET $var = value` | all `.surql` queries |
| `rand::guid()` | `rand::id()` | `upsert.go` |
| `type::thing()` | `type::record()` | `client.go` |
| `GROUP` + `SPLIT` together | Refactor to subqueries | `search.go` |

---

## 2. Namespace & Database Topology

```
Namespace: commit0
  └─ Database: codebase          ← STRICT mode enabled
       ├── Node tables:  file, function, class, module
       ├── Edge tables:  calls, imports, defines, inherits, uses
       ├── System tables: repo, index_run, session
       ├── API endpoints: /query, /trace, /blast, /repos, /index
       └── Buckets:      embedding_cache
```

```sql
-- Bootstrap (run once by schema.go ApplySchema)
DEFINE NAMESPACE IF NOT EXISTS commit0;
USE NS commit0;
DEFINE DATABASE IF NOT EXISTS codebase STRICT;
USE DB codebase;
```

**Strict mode** (new in 3.0) enforces that every table and field must be explicitly
defined — no accidental schemaless drift. This is critical for commit0 because
the indexing pipeline must never silently create malformed records.

---

## 3. Node Tables

### 3.1 `repo` — Repository Registry

Tracks indexed repositories and their metadata. New table (not in the original
ARCHITECTURE.md) to support multi-repo management.

```sql
DEFINE TABLE repo SCHEMAFULL;

DEFINE FIELD slug            ON repo TYPE string
    ASSERT string::len($value) > 0;
DEFINE FIELD path            ON repo TYPE string;
DEFINE FIELD remote_url      ON repo TYPE option<string>;
DEFINE FIELD default_branch  ON repo TYPE string
    VALUE "main";
DEFINE FIELD languages       ON repo TYPE set<string>;           -- set: auto-dedup (3.0)
DEFINE FIELD last_commit     ON repo TYPE option<string>;        -- HEAD SHA at last index
DEFINE FIELD last_indexed_at ON repo TYPE option<datetime>;
DEFINE FIELD stats           ON repo TYPE object
    VALUE { files: 0, functions: 0, classes: 0, modules: 0, edges: 0 };

-- Computed fields: evaluated on read, never stored (3.0 COMPUTED)
DEFINE FIELD is_stale        ON repo COMPUTED
    last_indexed_at IS NONE OR time::now() - last_indexed_at > 24h;

DEFINE INDEX repo_slug_idx   ON repo FIELDS slug UNIQUE;
```

### 3.2 `file` — Source File

```sql
DEFINE TABLE file SCHEMAFULL;

DEFINE FIELD path            ON file TYPE string;
DEFINE FIELD repo            ON file TYPE record<repo>
    REFERENCE ON DELETE CASCADE;                                 -- 3.0 referential integrity
DEFINE FIELD language        ON file TYPE string;
DEFINE FIELD content_hash    ON file TYPE string;
DEFINE FIELD line_count      ON file TYPE int
    VALUE 0;
DEFINE FIELD size_bytes      ON file TYPE int
    VALUE 0;
DEFINE FIELD embedding       ON file TYPE option<array<float>>;
DEFINE FIELD indexed_at      ON file TYPE datetime
    VALUE time::now();

-- Computed: count of functions/classes defined in this file
DEFINE FIELD symbol_count    ON file COMPUTED
    count(->defines->function) + count(->defines->class);
```

### 3.3 `function` — Function / Method

The core entity. Every function carries a fused embedding vector that captures
both its code semantics and its graph neighborhood.

```sql
DEFINE TABLE function SCHEMAFULL;

DEFINE FIELD name            ON function TYPE string;
DEFINE FIELD qualified       ON function TYPE string;
DEFINE FIELD file            ON function TYPE record<file>
    REFERENCE ON DELETE CASCADE;
DEFINE FIELD repo            ON function TYPE record<repo>
    REFERENCE ON DELETE CASCADE;
DEFINE FIELD start_line      ON function TYPE int;
DEFINE FIELD end_line        ON function TYPE int;
DEFINE FIELD signature       ON function TYPE string;
DEFINE FIELD docstring       ON function TYPE option<string>;
DEFINE FIELD body            ON function TYPE string;
DEFINE FIELD language        ON function TYPE string;
DEFINE FIELD content_hash    ON function TYPE string;
DEFINE FIELD embedding       ON function TYPE option<array<float>>;
DEFINE FIELD indexed_at      ON function TYPE datetime
    VALUE time::now();
DEFINE FIELD visibility      ON function TYPE string
    VALUE "public"
    ASSERT $value IN ["public", "private", "protected", "internal", "package"];

-- Computed fields (3.0) — derived on read, zero storage cost
DEFINE FIELD call_count      ON function COMPUTED
    count(<-calls<-function);
DEFINE FIELD callee_count    ON function COMPUTED
    count(->calls->function);
DEFINE FIELD centrality      ON function COMPUTED
    count(<-calls<-function) + count(->calls->function);
DEFINE FIELD is_leaf         ON function COMPUTED
    count(->calls->function) == 0;
DEFINE FIELD is_entry_point  ON function COMPUTED
    count(<-calls<-function) == 0 AND count(->calls->function) > 0;
```

### 3.4 `class` — Class / Struct / Interface

```sql
DEFINE TABLE class SCHEMAFULL;

DEFINE FIELD name            ON class TYPE string;
DEFINE FIELD qualified       ON class TYPE string;
DEFINE FIELD file            ON class TYPE record<file>
    REFERENCE ON DELETE CASCADE;
DEFINE FIELD repo            ON class TYPE record<repo>
    REFERENCE ON DELETE CASCADE;
DEFINE FIELD start_line      ON class TYPE int;
DEFINE FIELD end_line        ON class TYPE int;
DEFINE FIELD docstring       ON class TYPE option<string>;
DEFINE FIELD language        ON class TYPE string;
DEFINE FIELD kind            ON class TYPE string
    VALUE "class"
    ASSERT $value IN ["class", "struct", "interface", "enum", "trait", "protocol"];
DEFINE FIELD content_hash    ON class TYPE string;
DEFINE FIELD embedding       ON class TYPE option<array<float>>;
DEFINE FIELD indexed_at      ON class TYPE datetime
    VALUE time::now();

-- Computed
DEFINE FIELD method_count    ON class COMPUTED
    count(->defines->function);
DEFINE FIELD depth           ON class COMPUTED
    count(->inherits->class);
```

### 3.5 `module` — Package / Module

```sql
DEFINE TABLE module SCHEMAFULL;

DEFINE FIELD name            ON module TYPE string;
DEFINE FIELD path            ON module TYPE string;
DEFINE FIELD repo            ON module TYPE record<repo>
    REFERENCE ON DELETE CASCADE;
DEFINE FIELD language        ON module TYPE string;
DEFINE FIELD embedding       ON module TYPE option<array<float>>;
DEFINE FIELD indexed_at      ON module TYPE datetime
    VALUE time::now();

-- Computed
DEFINE FIELD file_count      ON module COMPUTED
    count(<-defines<-file);
```

---

## 4. Edge Tables

SurrealDB models graph edges as first-class records with their own fields.
Edge records use the `RELATE` statement: `RELATE nodeA->edge_table->nodeB`.

### 4.1 `calls` — Function Call Graph

```sql
DEFINE TABLE calls SCHEMAFULL;

DEFINE FIELD in              ON calls TYPE record<function>;
DEFINE FIELD out             ON calls TYPE record<function>;
DEFINE FIELD call_site       ON calls TYPE string;     -- "file.go:42"
DEFINE FIELD is_dynamic      ON calls TYPE bool
    VALUE false;
DEFINE FIELD call_type       ON calls TYPE string
    VALUE "direct"
    ASSERT $value IN ["direct", "interface", "callback", "goroutine", "deferred"];
DEFINE FIELD repo            ON calls TYPE record<repo>;

DEFINE INDEX calls_in_idx    ON calls FIELDS in;
DEFINE INDEX calls_out_idx   ON calls FIELDS out;
DEFINE INDEX calls_repo_idx  ON calls FIELDS repo;
```

### 4.2 `imports` — File Imports Module

```sql
DEFINE TABLE imports SCHEMAFULL;

DEFINE FIELD in              ON imports TYPE record<file>;
DEFINE FIELD out             ON imports TYPE record<module>;
DEFINE FIELD alias           ON imports TYPE option<string>;
DEFINE FIELD is_wildcard     ON imports TYPE bool
    VALUE false;

DEFINE INDEX imports_in_idx  ON imports FIELDS in;
DEFINE INDEX imports_out_idx ON imports FIELDS out;
```

### 4.3 `defines` — Containment (File/Module defines Function/Class)

```sql
DEFINE TABLE defines SCHEMAFULL;

DEFINE FIELD in              ON defines TYPE record<file | module>;
DEFINE FIELD out             ON defines TYPE record<function | class>;

DEFINE INDEX defines_in_idx  ON defines FIELDS in;
DEFINE INDEX defines_out_idx ON defines FIELDS out;
```

### 4.4 `inherits` — Class Hierarchy

```sql
DEFINE TABLE inherits SCHEMAFULL;

DEFINE FIELD in              ON inherits TYPE record<class>;
DEFINE FIELD out             ON inherits TYPE record<class>;
DEFINE FIELD kind            ON inherits TYPE string
    VALUE "extends"
    ASSERT $value IN ["extends", "implements", "embeds", "mixes"];

DEFINE INDEX inherits_in_idx  ON inherits FIELDS in;
DEFINE INDEX inherits_out_idx ON inherits FIELDS out;
```

### 4.5 `uses` — Function Uses Class

```sql
DEFINE TABLE uses SCHEMAFULL;

DEFINE FIELD in              ON uses TYPE record<function>;
DEFINE FIELD out             ON uses TYPE record<class>;
DEFINE FIELD usage_type      ON uses TYPE string
    VALUE "reference"
    ASSERT $value IN ["instantiation", "type_annotation", "struct_literal",
                       "cast", "reference", "return_type"];

DEFINE INDEX uses_in_idx     ON uses FIELDS in;
DEFINE INDEX uses_out_idx    ON uses FIELDS out;
```

---

## 5. Vector Indexes (HNSW — SurrealDB 3.0)

SurrealDB 3.0 removes MTREE and replaces it with HNSW (Hierarchical Navigable
Small World). HNSW provides faster approximate nearest-neighbor search with
memory-bounded LRU caching and concurrent write support.

### 5.1 Index Definitions

```sql
-- Function embeddings (primary search target)
DEFINE INDEX fn_vec_idx ON function FIELDS embedding
    HNSW DIMENSION 3072
    DIST COSINE
    TYPE F32                -- 32-bit float (half the memory of F64, sufficient precision)
    EFC 200                 -- construction-time search breadth (higher = better recall)
    M 16;                   -- max connections per node (16 is good for high-dim vectors)

-- Class embeddings
DEFINE INDEX cls_vec_idx ON class FIELDS embedding
    HNSW DIMENSION 3072
    DIST COSINE
    TYPE F32
    EFC 200
    M 16;

-- File embeddings (coarser, used for file-level search)
DEFINE INDEX file_vec_idx ON file FIELDS embedding
    HNSW DIMENSION 3072
    DIST COSINE
    TYPE F32
    EFC 150
    M 12;

-- Module embeddings (coarsest level)
DEFINE INDEX mod_vec_idx ON module FIELDS embedding
    HNSW DIMENSION 3072
    DIST COSINE
    TYPE F32
    EFC 150
    M 12;
```

### 5.2 HNSW Parameter Tuning Rationale

| Parameter | Value | Rationale |
|---|---|---|
| DIMENSION | 3072 | Gemini Embedding 2 output at max fidelity |
| DIST | COSINE | Standard for normalized text/code embeddings |
| TYPE | F32 | 50% memory savings over F64; negligible recall loss at 3072-dim |
| EFC | 200 | Higher than default (150) for better recall on code search |
| M | 16 | Slightly above default (12) — code embeddings have dense clusters |

**Memory estimate per 100K functions:**
- Vector storage: 100,000 x 3,072 x 4 bytes (F32) = ~1.2 GB
- HNSW graph overhead: ~20% additional = ~1.4 GB total
- Well within single-node capacity for most codebases

### 5.3 Vector Query Syntax (3.0)

```sql
-- Basic ANN search: find 10 nearest functions
LET $q = <embedding vector from Gemini>;

SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10|> $q;

-- With effort parameter for precision/speed tradeoff
-- Higher effort = more accurate but slower
SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10, 40|> $q;       -- effort=40 (search breadth at query time)

-- Cosine similarity (explicit, bypasses index — use for reranking)
SELECT *, vector::similarity::cosine(embedding, $q) AS score
FROM function
WHERE id IN $candidate_ids
ORDER BY score DESC;
```

### 5.4 Hybrid Search (Vector + Full-Text + Graph)

The signature query pattern for commit0 — combine semantic similarity with
keyword matching and graph structure in a single SurrealQL statement:

```sql
-- Hybrid: vector ANN + full-text + graph centrality boost
LET $q_vec = <embedding>;
LET $q_text = "JWT validation";

SELECT
    *,
    vector::distance::knn() AS vec_dist,
    search::score(1) AS fts_score,
    centrality AS graph_weight,

    -- Reciprocal Rank Fusion (RRF) for combining signals
    (1.0 / (60 + vec_dist)) AS rrf_vec,
    (1.0 / (60 + (1.0 - search::score(1)))) AS rrf_fts

FROM function
WHERE embedding <|20|> $q_vec
   OR (name @1@ $q_text OR qualified @1@ $q_text OR docstring @1@ $q_text)
ORDER BY (rrf_vec + rrf_fts) * math::log(1 + centrality) DESC
LIMIT 10;
```

---

## 6. Full-Text Search Indexes

### 6.1 Analyzer Definitions

```sql
-- Code-aware analyzer: splits on camelCase/snake_case boundaries
DEFINE ANALYZER code_analyzer
    TOKENIZERS class, blank
    FILTERS lowercase, ascii;

-- Natural language analyzer: for docstrings and queries
DEFINE ANALYZER nl_analyzer
    TOKENIZERS blank, punctuation
    FILTERS lowercase, ascii, snowball(english);
```

### 6.2 Full-Text Index Definitions

```sql
-- Function search (code identifiers)
DEFINE INDEX fn_name_fts ON function FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;

-- Function docstring search (natural language)
DEFINE INDEX fn_doc_fts ON function FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

-- Class search
DEFINE INDEX cls_name_fts ON class FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;

DEFINE INDEX cls_doc_fts ON class FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

-- File path search
DEFINE INDEX file_path_fts ON file FIELDS path
    FULLTEXT ANALYZER code_analyzer BM25;
```

### 6.3 Compound Indexes (3.0)

```sql
-- Compound: repo + language (fast filtering before vector search)
DEFINE INDEX fn_repo_lang_idx ON function FIELDS repo, language;
DEFINE INDEX cls_repo_lang_idx ON class FIELDS repo, language;
DEFINE INDEX file_repo_lang_idx ON file FIELDS repo, language;

-- Compound: repo + qualified name (exact symbol lookup)
DEFINE INDEX fn_qualified_idx ON function FIELDS repo, qualified UNIQUE;
DEFINE INDEX cls_qualified_idx ON class FIELDS repo, qualified UNIQUE;

-- Count index (3.0): instant count() without table scan
DEFINE INDEX fn_count_idx ON function FIELDS repo COUNT;
DEFINE INDEX cls_count_idx ON class FIELDS repo COUNT;
DEFINE INDEX file_count_idx ON file FIELDS repo COUNT;
```

---

## 7. Record References (3.0)

SurrealDB 3.0's `REFERENCE` keyword enables bidirectional navigation without
explicit `RELATE` edges. commit0 uses this for containment relationships where
the parent-child mapping is 1:N and doesn't need edge metadata.

```sql
-- file.repo is a reference: traverse with <~ from repo side
DEFINE FIELD repo ON file TYPE record<repo> REFERENCE ON DELETE CASCADE;

-- function.file is a reference
DEFINE FIELD file ON function TYPE record<file> REFERENCE ON DELETE CASCADE;

-- Traverse: all functions in a repo (via references, no edge table)
SELECT <~function AS functions FROM repo:my_repo;

-- Traverse: all files referencing a specific repo
SELECT <~file AS files FROM repo:my_repo;

-- Delete cascade: removing a repo deletes all its files, functions, classes
DELETE repo:my_repo;    -- cascades through REFERENCE ON DELETE CASCADE
```

**When to use references vs. edges:**

| Relationship | Mechanism | Reason |
|---|---|---|
| repo -> file, file -> function | `REFERENCE` | 1:N containment, no edge metadata needed |
| function -> function (calls) | `RELATE` edge | Edge carries call_site, is_dynamic metadata |
| file -> module (imports) | `RELATE` edge | Edge carries alias, is_wildcard metadata |
| class -> class (inherits) | `RELATE` edge | Edge carries kind (extends/implements) |

---

## 8. DEFINE API Endpoints (3.0)

SurrealDB 3.0's `DEFINE API` moves HTTP endpoint logic into the database itself.
commit0 uses this to expose its core query operations as DB-native endpoints,
reducing the Gin HTTP server to a thin proxy (or eliminating it for simple
deployments).

### 8.1 Middleware

```sql
-- Rate limiter middleware
DEFINE FUNCTION fn::rate_limit($req: object, $next: function) -> object {
    -- Built-in api::timeout for request-level timeout
    LET $res = $next($req);
    RETURN $res;
};

-- Repo validation middleware
DEFINE FUNCTION fn::require_repo($req: object, $next: function) -> object {
    LET $repo_slug = $req.params.repo;
    LET $repo = SELECT * FROM repo WHERE slug = $repo_slug LIMIT 1;
    IF $repo IS NONE {
        RETURN { status: 404, body: { error: "Repository not found", slug: $repo_slug } };
    };
    LET $req = $req + { context: { repo: $repo[0] } };
    RETURN $next($req);
};
```

### 8.2 Query Endpoint

```sql
DEFINE API OVERWRITE "/query/:repo" FOR post
    MIDDLEWARE fn::require_repo(), api::timeout(30s)
    THEN {
        LET $repo = $request.context.repo;
        LET $q = $request.body.q;
        LET $top_k = $request.body.top_k OR 10;
        LET $min_score = $request.body.min_score OR 0.70;

        -- Embed the query (delegated to external service via fn::embed)
        LET $q_vec = fn::embed_query($q);

        -- Hybrid search: vector + full-text
        LET $results = SELECT
            qualified, name, file.path AS file_path,
            start_line, end_line, signature, docstring,
            vector::distance::knn() AS distance,
            search::score(1) AS fts_score,
            centrality
        FROM function
        WHERE repo = $repo.id
          AND (
              embedding <|$top_k * 2|> $q_vec
              OR name @1@ $q
              OR qualified @1@ $q
          )
        ORDER BY (1.0 / (60 + vector::distance::knn()))
               * math::log(1 + centrality) DESC
        LIMIT $top_k;

        RETURN {
            status: 200,
            body: {
                query: $q,
                repo: $repo.slug,
                results: $results
            }
        };
    }
    PERMISSIONS FULL;
```

### 8.3 Trace Endpoint

```sql
DEFINE API OVERWRITE "/trace/:repo/:symbol" FOR get
    MIDDLEWARE fn::require_repo(), api::timeout(60s)
    THEN {
        LET $repo = $request.context.repo;
        LET $symbol = $request.params.symbol;
        LET $depth = $request.query.depth OR 6;
        LET $reverse = $request.query.reverse OR false;

        -- Resolve symbol
        LET $fn = SELECT * FROM function
            WHERE repo = $repo.id AND qualified = $symbol
            LIMIT 1;

        IF $fn IS NONE {
            RETURN { status: 404, body: { error: "Symbol not found" } };
        };

        -- Forward or reverse traversal
        LET $chain = IF $reverse {
            SELECT <-calls<-(function AS caller) FROM $fn[0].id LIMIT $depth
        } ELSE {
            SELECT ->calls->(function AS callee) FROM $fn[0].id LIMIT $depth
        };

        RETURN {
            status: 200,
            body: {
                symbol: $symbol,
                direction: IF $reverse { "callers" } ELSE { "callees" },
                chain: $chain
            }
        };
    }
    PERMISSIONS FULL;
```

### 8.4 Blast Radius Endpoint

```sql
DEFINE API OVERWRITE "/blast/:repo/:symbol" FOR get
    MIDDLEWARE fn::require_repo(), api::timeout(120s)
    THEN {
        LET $repo = $request.context.repo;
        LET $symbol = $request.params.symbol;

        LET $fn = SELECT * FROM function
            WHERE repo = $repo.id AND qualified = $symbol
            LIMIT 1;

        IF $fn IS NONE {
            RETURN { status: 404, body: { error: "Symbol not found" } };
        };

        -- Recursive reverse traversal: all transitive callers
        LET $affected = SELECT
            <-calls<-(function WHERE repo = $repo.id) AS callers
        FROM $fn[0].id;

        RETURN {
            status: 200,
            body: {
                target: $fn[0],
                affected: $affected,
                count: count($affected)
            }
        };
    }
    PERMISSIONS FULL;
```

### 8.5 Repos Listing Endpoint

```sql
DEFINE API OVERWRITE "/repos" FOR get
    MIDDLEWARE api::timeout(5s)
    THEN {
        LET $repos = SELECT
            slug, path, remote_url, languages,
            last_commit, last_indexed_at, is_stale, stats
        FROM repo
        ORDER BY last_indexed_at DESC;

        RETURN { status: 200, body: $repos };
    }
    PERMISSIONS FULL;
```

### 8.6 Endpoint Routing Summary

All DEFINE API endpoints are accessible at:
```
/api/commit0/codebase/<endpoint_path>
```

| Endpoint | Method | Path |
|---|---|---|
| Query | POST | `/api/commit0/codebase/query/:repo` |
| Trace | GET | `/api/commit0/codebase/trace/:repo/:symbol` |
| Blast | GET | `/api/commit0/codebase/blast/:repo/:symbol` |
| Repos | GET | `/api/commit0/codebase/repos` |

---

## 9. Client-Side Transactions (3.0)

SurrealDB 3.0 introduces client-side transactions — multi-request ACID workflows
managed from the Go SDK. commit0 uses this for atomic batch indexing: all nodes
and edges from a single file are committed together, or not at all.

### 9.1 Indexing Transaction Pattern

```go
// internal/graph/upsert.go

func (c *Client) UpsertFile(ctx context.Context, pf *parser.ParsedFile) error {
    tx, err := c.db.BeginTransaction(ctx)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Cancel(ctx) // no-op if already committed

    // 1. Upsert the file node
    if _, err := tx.Query(ctx, `
        UPSERT file SET
            path = $path, repo = $repo, language = $lang,
            content_hash = $hash, embedding = $emb,
            line_count = $lines, size_bytes = $size,
            indexed_at = time::now()
        WHERE path = $path AND repo = $repo
    `, map[string]interface{}{
        "path": pf.Path, "repo": pf.RepoID, "lang": pf.Language,
        "hash": pf.ContentHash, "emb": pf.Embedding,
        "lines": pf.LineCount, "size": pf.SizeBytes,
    }); err != nil {
        return err
    }

    // 2. Upsert function/class nodes
    for _, node := range pf.Nodes {
        if _, err := tx.Query(ctx, nodeUpsertQuery(node), nodeParams(node)); err != nil {
            return err
        }
    }

    // 3. Create edges (calls, imports, defines, uses, inherits)
    for _, edge := range pf.Edges {
        if _, err := tx.Query(ctx, `
            RELATE $from->$table->$to SET
                call_site = $site, is_dynamic = $dyn, repo = $repo
        `, edgeParams(edge)); err != nil {
            return err
        }
    }

    // 4. Atomic commit — all or nothing
    return tx.Commit(ctx)
}
```

### 9.2 Transaction Isolation

SurrealDB uses snapshot isolation: each transaction sees a consistent point-in-time
view. Concurrent indexing workers operating on different files will not conflict.
Workers touching the same function (cross-file call edges) may require retry logic:

```go
func (c *Client) UpsertFileWithRetry(ctx context.Context, pf *parser.ParsedFile) error {
    for attempt := 0; attempt < 3; attempt++ {
        err := c.UpsertFile(ctx, pf)
        if err == nil {
            return nil
        }
        if !isRetryable(err) {
            return err
        }
        time.Sleep(time.Duration(attempt*50) * time.Millisecond)
    }
    return fmt.Errorf("upsert failed after 3 retries: %s", pf.Path)
}
```

---

## 10. Changefeeds for Incremental Indexing

SurrealDB 3.0 changefeeds track every mutation to a table with configurable
retention. commit0 uses this to detect which records changed since the last
indexing run, enabling efficient incremental re-indexing.

### 10.1 Enable Changefeeds

```sql
-- 7-day retention on node tables
DEFINE TABLE file    SCHEMAFULL CHANGEFEED 7d;
DEFINE TABLE function SCHEMAFULL CHANGEFEED 7d;
DEFINE TABLE class   SCHEMAFULL CHANGEFEED 7d;
DEFINE TABLE module  SCHEMAFULL CHANGEFEED 7d;
```

### 10.2 Query Changes Since Last Run

```sql
-- Get all function changes since last index run
SHOW CHANGES FOR TABLE function SINCE $last_run_timestamp LIMIT 1000;
```

### 10.3 Incremental Re-Index Flow

```
git diff HEAD~1 → changed file paths
        │
        ▼
SELECT id, path, content_hash FROM file
    WHERE repo = $repo AND path IN $changed_paths;
        │
        ▼
For each file where SHA-256(new content) != content_hash:
    Re-parse → Re-embed → Upsert (transactional)
        │
        ▼
Delete edges for removed functions:
    DELETE calls WHERE in IN $deleted_fn_ids OR out IN $deleted_fn_ids;
        │
        ▼
Update repo.last_indexed_at, repo.last_commit
```

---

## 11. Session & Conversation Storage

Phase 3 introduces multi-turn conversational queries. Sessions are stored in
SurrealDB alongside the code graph for co-located access.

```sql
DEFINE TABLE session SCHEMAFULL;

DEFINE FIELD repo            ON session TYPE record<repo>;
DEFINE FIELD created_at      ON session TYPE datetime
    VALUE time::now();
DEFINE FIELD updated_at      ON session TYPE datetime
    VALUE time::now();
DEFINE FIELD turns           ON session TYPE array<object>;
    -- Each turn: { role: "user"|"assistant", content: string, timestamp: datetime }
DEFINE FIELD context_ids     ON session TYPE array<record>;
    -- function/class/file IDs referenced in conversation

DEFINE INDEX session_repo_idx ON session FIELDS repo;
DEFINE INDEX session_time_idx ON session FIELDS updated_at;
```

---

## 12. File Storage (DEFINE BUCKET — 3.0)

SurrealDB 3.0 introduces native file storage via buckets. commit0 uses this to
cache embedding input text and parsed AST snapshots, avoiding redundant Gemini API
calls during re-indexing.

```sql
-- Embedding input cache
DEFINE BUCKET embedding_cache
    BACKEND "file"                  -- filesystem-backed
    PERMISSIONS FULL;

-- Store embedding input for cache validation
-- Key: content_hash, Value: the text sent to Gemini
-- On re-index: if content_hash matches, skip embedding API call
```

```go
// internal/graph/cache.go
func (c *Client) CacheEmbeddingInput(ctx context.Context, hash, text string) error {
    return c.bucket.Put(ctx, "embedding_cache", hash, []byte(text))
}

func (c *Client) GetCachedInput(ctx context.Context, hash string) (string, error) {
    data, err := c.bucket.Get(ctx, "embedding_cache", hash)
    if err != nil {
        return "", err
    }
    return string(data), nil
}
```

---

## 13. Permissions & Access Control

commit0 is a local-first CLI tool, so the default deployment uses root
credentials. For multi-user deployments (Phase 4), SurrealDB's scope-based
auth provides per-repo access control.

```sql
-- Read-only scope for query users (no indexing)
DEFINE ACCESS reader ON DATABASE TYPE RECORD
    SIGNUP NONE
    SIGNIN (
        SELECT * FROM user WHERE email = $email AND
            crypto::argon2::compare(password, $password)
    )
    DURATION FOR SESSION 24h;

-- Per-table permissions
DEFINE FIELD repo ON function TYPE record<repo>;

-- Functions: readers can SELECT; only indexer can CREATE/UPDATE/DELETE
ALTER TABLE function
    PERMISSIONS
        FOR select FULL
        FOR create, update, delete WHERE $auth.role = "indexer";
```

---

## 14. Observability

### 14.1 Index Run Tracking

```sql
DEFINE TABLE index_run SCHEMAFULL;

DEFINE FIELD repo            ON index_run TYPE record<repo>;
DEFINE FIELD started_at      ON index_run TYPE datetime
    VALUE time::now();
DEFINE FIELD completed_at    ON index_run TYPE option<datetime>;
DEFINE FIELD status          ON index_run TYPE string
    VALUE "running"
    ASSERT $value IN ["running", "completed", "failed"];
DEFINE FIELD stats           ON index_run TYPE object
    VALUE {
        files_scanned: 0, files_changed: 0,
        nodes_upserted: 0, edges_upserted: 0,
        embeddings_computed: 0, embeddings_cached: 0,
        errors: 0
    };
DEFINE FIELD error_message   ON index_run TYPE option<string>;
DEFINE FIELD duration_ms     ON index_run COMPUTED
    IF completed_at IS NOT NONE {
        duration::millis(completed_at - started_at)
    } ELSE {
        NONE
    };

DEFINE INDEX run_repo_idx    ON index_run FIELDS repo, started_at;
```

### 14.2 Live Queries for Progress Monitoring

```sql
-- Subscribe to index_run changes (used by CLI progress bar / web UI)
LIVE SELECT * FROM index_run WHERE status = "running";
```

---

## 15. Graph Traversal Patterns

### 15.1 Forward Call Chain (Trace)

```sql
-- Depth-limited forward traversal from a function
-- Uses SurrealDB's recursive graph syntax
SELECT
    ->calls->(function AS hop1),
    ->calls->calls->(function AS hop2),
    ->calls->calls->calls->(function AS hop3),
    ->calls->calls->calls->calls->(function AS hop4),
    ->calls->calls->calls->calls->calls->(function AS hop5),
    ->calls->calls->calls->calls->calls->calls->(function AS hop6)
FROM function:$start_id;
```

### 15.2 Reverse Call Chain (Blast Radius)

```sql
-- Who calls this function, transitively?
SELECT
    <-calls<-(function AS hop1),
    <-calls<-calls<-(function AS hop2),
    <-calls<-calls<-calls<-(function AS hop3),
    <-calls<-calls<-calls<-calls<-(function AS hop4)
FROM function:$target_id;
```

### 15.3 Cross-Entity Traversal

```sql
-- Find all classes used by functions that call a given function
SELECT
    <-calls<-(function)->uses->(class) AS affected_types
FROM function:$target_id;

-- Find the module hierarchy for a given function
SELECT
    file.path,
    <-defines<-(file)<-defines<-(module) AS parent_modules
FROM function:$fn_id;
```

### 15.4 Semantic + Structural Combined Query

```sql
-- "What functions are semantically similar to X AND in its call neighborhood?"
LET $fn = SELECT * FROM function:$fn_id;
LET $neighbors = SELECT ->calls->function.id AS ids,
                        <-calls<-function.id AS ids
                 FROM function:$fn_id;

SELECT *, vector::similarity::cosine(embedding, $fn.embedding) AS sim
FROM function
WHERE id IN $neighbors.ids
  AND vector::similarity::cosine(embedding, $fn.embedding) > 0.75
ORDER BY sim DESC
LIMIT 10;
```

---

## 16. Performance Considerations

### 16.1 Index Rebuild Strategy

HNSW indexes are in-memory structures. After large bulk imports, rebuild for
optimal graph connectivity:

```sql
REBUILD INDEX fn_vec_idx ON function;
REBUILD INDEX cls_vec_idx ON class;
REBUILD INDEX file_vec_idx ON file;
```

### 16.2 Query Optimization

Use `EXPLAIN FULL` to verify index utilization:

```sql
EXPLAIN FULL
SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10|> $q_vec AND repo = $repo_id;
```

### 16.3 Connection Pooling

The Go SDK supports connection pooling over WebSocket. Configure based on
concurrency needs:

```go
// internal/graph/client.go
func NewClient(cfg *config.Config) (*Client, error) {
    db, err := surrealdb.New(cfg.SurrealURL)
    if err != nil {
        return nil, err
    }
    // Authenticate
    if _, err := db.Signin(map[string]interface{}{
        "user": cfg.SurrealUser,
        "pass": cfg.SurrealPass,
    }); err != nil {
        return nil, err
    }
    // Select namespace and database
    if _, err := db.Use(cfg.SurrealNS, cfg.SurrealDB); err != nil {
        return nil, err
    }
    return &Client{db: db}, nil
}
```

### 16.4 Capacity Planning

| Codebase Size | Functions | Edges | Vector Storage (F32) | Total DB Size (est.) |
|---|---|---|---|---|
| Small (10K LOC) | ~500 | ~2,000 | 6 MB | ~50 MB |
| Medium (100K LOC) | ~5,000 | ~20,000 | 60 MB | ~500 MB |
| Large (1M LOC) | ~50,000 | ~200,000 | 600 MB | ~5 GB |
| Monorepo (10M LOC) | ~500,000 | ~2,000,000 | 6 GB | ~50 GB |

---

## 17. Schema Versioning

The schema is embedded in the Go binary via `go:embed` and applied by
`internal/graph/schema.go`. A version table tracks migrations:

```sql
DEFINE TABLE schema_version SCHEMAFULL;

DEFINE FIELD version         ON schema_version TYPE int;
DEFINE FIELD applied_at      ON schema_version TYPE datetime
    VALUE time::now();
DEFINE FIELD description     ON schema_version TYPE string;
```

```go
// internal/graph/schema.go

//go:embed ../../assets/schema.surql
var schemaSQL string

func (c *Client) ApplySchema(ctx context.Context) error {
    // Check current version
    current, _ := c.getCurrentSchemaVersion(ctx)

    // Apply migrations sequentially
    for _, m := range migrations {
        if m.Version > current {
            if _, err := c.db.Query(ctx, m.SQL, nil); err != nil {
                return fmt.Errorf("migration v%d failed: %w", m.Version, err)
            }
            c.recordVersion(ctx, m.Version, m.Description)
        }
    }
    return nil
}
```

---

## 18. Configuration Reference

Updated from ARCHITECTURE.md section 13, reflecting SurrealDB 3.0:

```bash
# SurrealDB 3.0
COMMIT0_SURREAL_URL=ws://localhost:8000/rpc      # WebSocket for streaming
COMMIT0_SURREAL_HTTP=http://localhost:8000         # HTTP for DEFINE API endpoints
COMMIT0_SURREAL_NS=commit0
COMMIT0_SURREAL_DB=codebase
COMMIT0_SURREAL_USER=root
COMMIT0_SURREAL_PASS=root
COMMIT0_SURREAL_STRICT=true                        # Enable strict mode (3.0)

# Vector Index Tuning
COMMIT0_HNSW_EFC=200                               # Construction-time search breadth
COMMIT0_HNSW_M=16                                  # Max connections per node
COMMIT0_HNSW_TYPE=F32                              # Vector storage type

# Embedding
COMMIT0_GEMINI_EMBED_DIMS=3072                     # Must match HNSW DIMENSION
```

---

## 19. Complete Schema File

The full `assets/schema.surql` combining all definitions above is maintained as
the source of truth. The file follows this order:

1. Namespace and database setup (with STRICT)
2. Analyzers
3. Node tables (repo, file, function, class, module)
4. Edge tables (calls, imports, defines, inherits, uses)
5. System tables (session, index_run, schema_version)
6. Vector indexes (HNSW)
7. Full-text indexes
8. Compound indexes and count indexes
9. Buckets
10. Functions (middleware, helpers)
11. API endpoints

This ordering ensures that all dependencies (tables, fields) exist before
indexes and API endpoints reference them.
