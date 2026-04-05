---
name: commit0-surrealdb
description: SurrealDB 3.0 for commit0. TRIGGER when: writing SurrealQL, schema definitions, HNSW vector indexes, BM25 full-text search, RELATE edges, the SurrealDB Go adapter, transactions, DEFINE API, changefeeds, or assets/schema.surql. DO NOT TRIGGER for pure Go or Gemini code.
---

# commit0 SurrealDB 3.0 Skill

Use this skill when writing SurrealDB-related code for commit0: schema definitions, SurrealQL queries, the Go SurrealDB adapter, DEFINE API endpoints, vector/FTS indexes, transactions, and changefeeds.

---

## SurrealDB 3.0 Breaking Changes (from 2.x)

CRITICAL: commit0 targets SurrealDB 3.0. Always use the 3.0 syntax:

| 2.x (DO NOT USE) | 3.0 (USE THIS) |
|---|---|
| `MTREE DIMENSION 3072 DIST COSINE` | `HNSW DIMENSION 3072 DIST COSINE` |
| `SEARCH ANALYZER code_analyzer BM25` | `FULLTEXT ANALYZER code_analyzer BM25` |
| `<future> { ... }` | `COMPUTED expression` |
| Bare `$var = value` | `LET $var = value` |
| `rand::guid()` | `rand::id()` |
| `type::thing()` | `type::record()` |
| `GROUP` + `SPLIT` together | Refactor to subqueries |

---

## Namespace & Database

```sql
DEFINE NAMESPACE IF NOT EXISTS commit0;
USE NS commit0;
DEFINE DATABASE IF NOT EXISTS codebase STRICT;
USE DB codebase;
```

**Strict mode** enforces that every table and field must be explicitly defined. No accidental schemaless drift.

---

## Schema: Node Tables

### repo (Repository Registry)

```sql
DEFINE TABLE repo SCHEMAFULL;

DEFINE FIELD slug            ON repo TYPE string ASSERT string::len($value) > 0;
DEFINE FIELD path            ON repo TYPE string;
DEFINE FIELD remote_url      ON repo TYPE option<string>;
DEFINE FIELD default_branch  ON repo TYPE string VALUE "main";
DEFINE FIELD languages       ON repo TYPE set<string>;
DEFINE FIELD last_commit     ON repo TYPE option<string>;
DEFINE FIELD last_indexed_at ON repo TYPE option<datetime>;
DEFINE FIELD stats           ON repo TYPE object
    VALUE { files: 0, functions: 0, classes: 0, modules: 0, edges: 0 };

-- Computed: evaluated on read, never stored
DEFINE FIELD is_stale ON repo COMPUTED
    last_indexed_at IS NONE OR time::now() - last_indexed_at > 24h;

DEFINE INDEX repo_slug_idx ON repo FIELDS slug UNIQUE;
```

### file (Source File)

```sql
DEFINE TABLE file SCHEMAFULL;

DEFINE FIELD path         ON file TYPE string;
DEFINE FIELD repo         ON file TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD language     ON file TYPE string;
DEFINE FIELD content_hash ON file TYPE string;
DEFINE FIELD line_count   ON file TYPE int VALUE 0;
DEFINE FIELD size_bytes   ON file TYPE int VALUE 0;
DEFINE FIELD embedding    ON file TYPE option<array<float>>;
DEFINE FIELD indexed_at   ON file TYPE datetime VALUE time::now();

DEFINE FIELD symbol_count ON file COMPUTED
    count(->defines->function) + count(->defines->class);
```

### function (Function / Method)

```sql
DEFINE TABLE function SCHEMAFULL;

DEFINE FIELD name         ON function TYPE string;
DEFINE FIELD qualified    ON function TYPE string;
DEFINE FIELD file         ON function TYPE record<file> REFERENCE ON DELETE CASCADE;
DEFINE FIELD repo         ON function TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD start_line   ON function TYPE int;
DEFINE FIELD end_line     ON function TYPE int;
DEFINE FIELD signature    ON function TYPE string;
DEFINE FIELD docstring    ON function TYPE option<string>;
DEFINE FIELD body         ON function TYPE string;
DEFINE FIELD language     ON function TYPE string;
DEFINE FIELD content_hash ON function TYPE string;
DEFINE FIELD embedding    ON function TYPE option<array<float>>;
DEFINE FIELD indexed_at   ON function TYPE datetime VALUE time::now();
DEFINE FIELD visibility   ON function TYPE string VALUE "public"
    ASSERT $value IN ["public", "private", "protected", "internal", "package"];

-- Computed fields (derived on read, zero storage cost)
DEFINE FIELD call_count     ON function COMPUTED count(<-calls<-function);
DEFINE FIELD callee_count   ON function COMPUTED count(->calls->function);
DEFINE FIELD centrality     ON function COMPUTED
    count(<-calls<-function) + count(->calls->function);
DEFINE FIELD is_leaf        ON function COMPUTED count(->calls->function) == 0;
DEFINE FIELD is_entry_point ON function COMPUTED
    count(<-calls<-function) == 0 AND count(->calls->function) > 0;
```

### class (Class / Struct / Interface)

```sql
DEFINE TABLE class SCHEMAFULL;

DEFINE FIELD name         ON class TYPE string;
DEFINE FIELD qualified    ON class TYPE string;
DEFINE FIELD file         ON class TYPE record<file> REFERENCE ON DELETE CASCADE;
DEFINE FIELD repo         ON class TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD start_line   ON class TYPE int;
DEFINE FIELD end_line     ON class TYPE int;
DEFINE FIELD docstring    ON class TYPE option<string>;
DEFINE FIELD language     ON class TYPE string;
DEFINE FIELD kind         ON class TYPE string VALUE "class"
    ASSERT $value IN ["class", "struct", "interface", "enum", "trait", "protocol"];
DEFINE FIELD content_hash ON class TYPE string;
DEFINE FIELD embedding    ON class TYPE option<array<float>>;
DEFINE FIELD indexed_at   ON class TYPE datetime VALUE time::now();

DEFINE FIELD method_count ON class COMPUTED count(->defines->function);
DEFINE FIELD depth        ON class COMPUTED count(->inherits->class);
```

### module (Package / Module)

```sql
DEFINE TABLE module SCHEMAFULL;

DEFINE FIELD name       ON module TYPE string;
DEFINE FIELD path       ON module TYPE string;
DEFINE FIELD repo       ON module TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD language   ON module TYPE string;
DEFINE FIELD embedding  ON module TYPE option<array<float>>;
DEFINE FIELD indexed_at ON module TYPE datetime VALUE time::now();

DEFINE FIELD file_count ON module COMPUTED count(<-defines<-file);
```

---

## Schema: Edge Tables

```sql
-- Function call graph
DEFINE TABLE calls SCHEMAFULL;
DEFINE FIELD in        ON calls TYPE record<function>;
DEFINE FIELD out       ON calls TYPE record<function>;
DEFINE FIELD call_site ON calls TYPE string;
DEFINE FIELD is_dynamic ON calls TYPE bool VALUE false;
DEFINE FIELD call_type ON calls TYPE string VALUE "direct"
    ASSERT $value IN ["direct", "interface", "callback", "goroutine", "deferred"];
DEFINE FIELD repo      ON calls TYPE record<repo>;
DEFINE INDEX calls_in_idx  ON calls FIELDS in;
DEFINE INDEX calls_out_idx ON calls FIELDS out;
DEFINE INDEX calls_repo_idx ON calls FIELDS repo;

-- File imports module
DEFINE TABLE imports SCHEMAFULL;
DEFINE FIELD in          ON imports TYPE record<file>;
DEFINE FIELD out         ON imports TYPE record<module>;
DEFINE FIELD alias       ON imports TYPE option<string>;
DEFINE FIELD is_wildcard ON imports TYPE bool VALUE false;
DEFINE INDEX imports_in_idx  ON imports FIELDS in;
DEFINE INDEX imports_out_idx ON imports FIELDS out;

-- File/Module defines Function/Class
DEFINE TABLE defines SCHEMAFULL;
DEFINE FIELD in  ON defines TYPE record<file | module>;
DEFINE FIELD out ON defines TYPE record<function | class>;
DEFINE INDEX defines_in_idx  ON defines FIELDS in;
DEFINE INDEX defines_out_idx ON defines FIELDS out;

-- Class hierarchy
DEFINE TABLE inherits SCHEMAFULL;
DEFINE FIELD in   ON inherits TYPE record<class>;
DEFINE FIELD out  ON inherits TYPE record<class>;
DEFINE FIELD kind ON inherits TYPE string VALUE "extends"
    ASSERT $value IN ["extends", "implements", "embeds", "mixes"];
DEFINE INDEX inherits_in_idx  ON inherits FIELDS in;
DEFINE INDEX inherits_out_idx ON inherits FIELDS out;

-- Function uses class
DEFINE TABLE uses SCHEMAFULL;
DEFINE FIELD in         ON uses TYPE record<function>;
DEFINE FIELD out        ON uses TYPE record<class>;
DEFINE FIELD usage_type ON uses TYPE string VALUE "reference"
    ASSERT $value IN ["instantiation", "type_annotation", "struct_literal",
                       "cast", "reference", "return_type"];
DEFINE INDEX uses_in_idx  ON uses FIELDS in;
DEFINE INDEX uses_out_idx ON uses FIELDS out;
```

**Rule:** Use `RELATE nodeA->edge_table->nodeB` to create edges, not INSERT.

---

## Vector Indexes (HNSW)

```sql
-- Function embeddings (primary search target)
DEFINE INDEX fn_vec_idx ON function FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;

-- Class embeddings
DEFINE INDEX cls_vec_idx ON class FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;

-- File embeddings (coarser)
DEFINE INDEX file_vec_idx ON file FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 150 M 12;

-- Module embeddings (coarsest)
DEFINE INDEX mod_vec_idx ON module FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 150 M 12;
```

### HNSW Parameters

| Param | Value | Meaning |
|---|---|---|
| DIMENSION | 3072 | Gemini Embedding 2 output dimension |
| DIST | COSINE | Standard for normalized embeddings |
| TYPE | F32 | 50% memory savings vs F64, negligible recall loss |
| EFC | 200 | Construction search breadth (higher = better recall) |
| M | 16 | Max connections per node in HNSW graph |

### Vector Query Syntax

```sql
-- Basic ANN: find 10 nearest functions
LET $q = <embedding>;
SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10|> $q;

-- With effort parameter (precision/speed tradeoff)
SELECT *, vector::distance::knn() AS dist
FROM function
WHERE embedding <|10, 40|> $q;

-- Explicit cosine similarity (bypasses index, use for reranking)
SELECT *, vector::similarity::cosine(embedding, $q) AS score
FROM function WHERE id IN $candidates
ORDER BY score DESC;
```

---

## Full-Text Search Indexes

```sql
-- Analyzers
DEFINE ANALYZER code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER nl_analyzer TOKENIZERS blank, punctuation FILTERS lowercase, ascii, snowball(english);

-- Function name/qualified search
DEFINE INDEX fn_name_fts ON function FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;
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

### FTS Query Syntax

```sql
-- BM25 scored search
SELECT *, search::score(1) AS score
FROM function
WHERE name @1@ "validate" OR qualified @1@ "validate"
ORDER BY score DESC
LIMIT 10;
```

---

## Compound & Count Indexes

```sql
DEFINE INDEX fn_repo_lang_idx   ON function FIELDS repo, language;
DEFINE INDEX cls_repo_lang_idx  ON class FIELDS repo, language;
DEFINE INDEX file_repo_lang_idx ON file FIELDS repo, language;

DEFINE INDEX fn_qualified_idx   ON function FIELDS repo, qualified UNIQUE;
DEFINE INDEX cls_qualified_idx  ON class FIELDS repo, qualified UNIQUE;

-- Count index: instant count() without table scan
DEFINE INDEX fn_count_idx   ON function FIELDS repo COUNT;
DEFINE INDEX cls_count_idx  ON class FIELDS repo COUNT;
DEFINE INDEX file_count_idx ON file FIELDS repo COUNT;
```

---

## Hybrid Search Pattern (Vector + FTS + Graph)

The signature query pattern for commit0:

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

## Record References (3.0)

Use `REFERENCE` for 1:N containment relationships (no edge metadata needed):

```sql
DEFINE FIELD repo ON file TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD file ON function TYPE record<file> REFERENCE ON DELETE CASCADE;

-- Traverse: all functions in a repo (via references)
SELECT <~function AS functions FROM repo:my_repo;

-- Delete cascade: removing a repo deletes all its files, functions, classes
DELETE repo:my_repo;
```

**When to use REFERENCE vs RELATE:**
- REFERENCE: 1:N containment, no edge metadata (repo→file, file→function)
- RELATE edge: relationships with metadata (calls, imports, inherits, uses)

---

## Client-Side Transactions (3.0)

Atomic per-file batch upsert pattern:

```go
func (a *SurrealAdapter) UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
    tx, err := a.db.BeginTransaction(ctx)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Cancel(ctx) // no-op if already committed

    for _, node := range nodes {
        if _, err := tx.Query(ctx, upsertNodeQuery(node.Kind), nodeParams(node)); err != nil {
            return fmt.Errorf("upsert node %s: %w", node.ID, err)
        }
    }

    for _, edge := range edges {
        if _, err := tx.Query(ctx, relateEdgeQuery(edge.Kind), edgeParams(edge)); err != nil {
            return fmt.Errorf("relate edge %s->%s: %w", edge.FromID, edge.ToID, err)
        }
    }

    return tx.Commit(ctx)
}
```

Retry pattern for conflict resolution (concurrent workers on same functions):

```go
for attempt := 0; attempt < 3; attempt++ {
    err := c.UpsertFile(ctx, pf)
    if err == nil { return nil }
    if !isRetryable(err) { return err }
    time.Sleep(time.Duration(attempt*50) * time.Millisecond)
}
```

---

## DEFINE API Endpoints

SurrealDB 3.0 native HTTP endpoints:

```sql
-- Middleware functions
DEFINE FUNCTION fn::require_repo($req: object, $next: function) -> object {
    LET $repo_slug = $req.params.repo;
    LET $repo = SELECT * FROM repo WHERE slug = $repo_slug LIMIT 1;
    IF $repo IS NONE {
        RETURN { status: 404, body: { error: "Repository not found", slug: $repo_slug } };
    };
    LET $req = $req + { context: { repo: $repo[0] } };
    RETURN $next($req);
};

-- Endpoints
DEFINE API OVERWRITE "/query/:repo" FOR post
    MIDDLEWARE fn::require_repo(), api::timeout(30s) THEN { ... } PERMISSIONS FULL;

DEFINE API OVERWRITE "/trace/:repo/:symbol" FOR get
    MIDDLEWARE fn::require_repo(), api::timeout(60s) THEN { ... } PERMISSIONS FULL;

DEFINE API OVERWRITE "/blast/:repo/:symbol" FOR get
    MIDDLEWARE fn::require_repo(), api::timeout(120s) THEN { ... } PERMISSIONS FULL;

DEFINE API OVERWRITE "/repos" FOR get
    MIDDLEWARE api::timeout(5s) THEN { ... } PERMISSIONS FULL;
```

Endpoint paths: `/api/commit0/codebase/<endpoint_path>`

---

## Changefeeds for Incremental Indexing

```sql
-- Enable 7-day retention
DEFINE TABLE file     SCHEMAFULL CHANGEFEED 7d;
DEFINE TABLE function SCHEMAFULL CHANGEFEED 7d;
DEFINE TABLE class    SCHEMAFULL CHANGEFEED 7d;
DEFINE TABLE module   SCHEMAFULL CHANGEFEED 7d;

-- Query changes since last run
SHOW CHANGES FOR TABLE function SINCE $last_run_timestamp LIMIT 1000;
```

---

## Go SDK Connection Pattern

```go
import surrealdb "surrealdb/surrealdb.go/v2"

func NewSurrealAdapter(cfg *config.SurrealConfig) (*SurrealAdapter, error) {
    db, err := surrealdb.New(cfg.URL)  // ws://localhost:8000
    if err != nil { return nil, err }

    if _, err := db.Signin(map[string]interface{}{
        "user": cfg.User, "pass": cfg.Pass,
    }); err != nil { return nil, err }

    if _, err := db.Use(cfg.Namespace, cfg.Database); err != nil {
        return nil, err
    }

    return &SurrealAdapter{db: db}, nil
}
```

---

## Schema Application (assets/schema.surql)

The schema file is embedded into the binary via `go:embed` and applied on first run:

```go
//go:embed assets/schema.surql
var schemaSurQL string

func (a *SurrealAdapter) ApplySchema(ctx context.Context) error {
    _, err := a.db.Query(ctx, schemaSurQL, nil)
    return err
}
```
