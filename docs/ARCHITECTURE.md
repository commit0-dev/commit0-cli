# commit0 — Architecture Document

> Graph-based source code analyzer with conversational query and blast radius analysis.
> Ships as a **single Go binary**. Powered by SurrealDB (graph + vector) and
> **Gemini Embedding 2** — the only embedding model that unifies text, code, and images
> in one vector space.

---

## 1. Vision

**commit0** indexes any codebase into a richly connected knowledge graph where every
function, class, and file is a node, every call/import/inheritance is an edge, and every
entity carries a dense multi-modal embedding placing code, comments, diagrams, and
natural-language queries into the **same vector space**.

Users can then:

- Ask in plain English — "where does auth token validation happen?"
- Trace flows end-to-end — "show the call chain from HTTP handler to DB write"
- Run blast-radius analysis — "if I change `UserService.create`, what else breaks?"

Inspired by **DeepWiki** (wiki-style code docs) and **GitNexus** (graph-first code nav).

### Single Binary Philosophy
One `commit0` binary, downloaded from GitHub Releases or installed via Homebrew, does
everything. No Python runtime, no `pip install`, no virtualenv. The only external
dependency is a running SurrealDB instance (which commit0 can manage locally).

```
curl -fsSL https://install.commit0.dev | sh
commit0 index ./my-project
commit0 query "where is the JWT middleware?"
```

---

## 2. High-Level Architecture

commit0 uses a **ports-and-adapters** (hexagonal) architecture. The core domain
logic operates on Go interfaces (ports) — no knowledge of SurrealDB, Gemini, or
tree-sitter. Each external system is a swappable adapter. See `BACKEND.md` for
full interface signatures and implementation details.

```
┌──────────────────────────────────────────────────────────────────────────┐
│                       commit0  (single Go binary)                        │
│                                                                          │
│  DRIVING ADAPTERS (input)                                                │
│  ┌──────────────┐   ┌──────────────┐   ┌────────────────────────────┐   │
│  │  CLI (Cobra) │   │ HTTP (Echo)  │   │ SurrealDB DEFINE API (3.0) │   │
│  │  cmd/*.go    │   │ server/*.go  │   │ DB-native endpoints        │   │
│  └──────┬───────┘   └──────┬───────┘   └────────────┬───────────────┘   │
│         └──────────────────┼────────────────────────┘                    │
│                            ▼                                             │
│  ┌───────────────────────────────────────────────────────────────────┐   │
│  │  APPLICATION SERVICES  (internal/app/)                             │   │
│  │                                                                    │   │
│  │  IndexService │ QueryService │ TraceService │ BlastService         │   │
│  │  RepoService  │ SessionService                                    │   │
│  │                                                                    │   │
│  │  Orchestrate: walk → parse → embed → store                        │   │
│  │               query → embed → search(vec+fts) → RRF → explain    │   │
│  │               symbol → graph traverse → explain                   │   │
│  └───────────────────────────────┬───────────────────────────────────┘   │
│                                  │                                       │
│  ┌───────────────────────────────┼───────────────────────────────────┐   │
│  │  DOMAIN CORE  (internal/domain/ + pkg/types/)                     │   │
│  │                                                                    │   │
│  │  Types:  CodeNode, CodeEdge, Repo, QueryResult, TraceResult, ...  │   │
│  │  Ports:  GraphStore │ VectorIndex │ TextIndex │ Embedder          │   │
│  │          LLMExplainer │ Parser │ FileWalker                       │   │
│  └───────────────────────────────┬───────────────────────────────────┘   │
│                                  │                                       │
│  DRIVEN ADAPTERS (output)        ▼                                       │
│  ┌────────────────┐  ┌─────────────────┐  ┌──────────────────────┐      │
│  │  SurrealDB 3.0 │  │  Gemini SDK     │  │  tree-sitter (CGO)  │      │
│  │  Adapter        │  │  Adapter        │  │  Adapter             │      │
│  │                 │  │                 │  │                      │      │
│  │  → GraphStore   │  │  → Embedder     │  │  → Parser            │      │
│  │  → VectorIndex  │  │  → LLMExplainer │  │                      │      │
│  │  → TextIndex    │  │                 │  │                      │      │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬───────────┘      │
│           │                    │                       │                  │
│           ▼                    ▼                       ▼                  │
│  ┌─────────────────┐  ┌──────────────┐  ┌────────────────────────┐      │
│  │  SurrealDB 3.0  │  │  Gemini API  │  │  tree-sitter C libs    │      │
│  │  Graph + Vector  │  │  Embed + LLM │  │  Go, Python, TS, JS   │      │
│  │  + FTS + API     │  │              │  │  grammars             │      │
│  └─────────────────┘  └──────────────┘  └────────────────────────┘      │
└──────────────────────────────────────────────────────────────────────────┘
```

### Port Interfaces

The domain core defines 7 port interfaces — the contracts between business logic
and infrastructure. Every external dependency is accessed through these ports:

| Port | Responsibility | Adapter |
|------|---------------|---------|
| `GraphStore` | CRUD nodes/edges, graph traversal, transactions | SurrealDB 3.0 |
| `VectorIndex` | ANN search over embeddings (HNSW) | SurrealDB 3.0 |
| `TextIndex` | BM25 full-text search | SurrealDB 3.0 |
| `Embedder` | Text/code/image → vector (batch, cache) | Gemini Embedding 2 |
| `LLMExplainer` | Code context → streaming NL explanation | Gemini 2.0 Flash |
| `Parser` | Source file → AST nodes + edges | tree-sitter (CGO) |
| `FileWalker` | Repo path → file entries (.gitignore-aware) | OS filesystem |

This isolation means: replace Gemini with OpenAI by implementing `Embedder` —
zero changes to IndexService, QueryService, or any business logic.

---

## 3. Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Language | **Go 1.22+** | Single static binary, fast compilation, strong concurrency |
| CLI | **Cobra + Viper** | Industry-standard Go CLI framework |
| HTTP server | **Echo v4** | Minimal, fast, idiomatic Go |
| AST Parsing | **go-tree-sitter** (`smacker/go-tree-sitter`) | Multi-language, incremental, CGO-linked |
| Graph + Vector DB | **SurrealDB 3.0** | Native graph traversal + HNSW vector ANN + FTS in one query; DEFINE API, computed fields, client-side transactions |
| Embeddings | **Gemini Embedding 2** | Only model embedding text+code+images in a single vector space |
| LLM (explain) | **Gemini 2.0 Flash** | Streaming, long context, tight SDK integration |
| Gemini SDK | `google.golang.org/genai` | Official unified Google AI Go SDK |
| SurrealDB SDK | `surrealdb/surrealdb.go/v2` | Official Go driver (WebSocket + HTTP) |
| Concurrency | `golang.org/x/sync/errgroup` | Bounded worker pool for parallel indexing |
| Progress | `schollz/progressbar/v3` | Terminal progress during indexing |
| Logging | `log/slog` (stdlib) | Structured logging, zero dependencies |
| Config | Viper + `.env` | Env-var driven, flag override |
| Distribution | GitHub Releases + Homebrew | `brew install commit0-dev/tap/commit0` |

---

## 4. Why Gemini Embedding 2

Gemini Embedding 2 (`gemini-embedding-2-preview`) is Google's **first multi-modal
embedding model** — and currently the only production embedding model placing text,
code, images, audio, video, and PDFs into **one unified vector space**.

| Input modality | Limit per request | commit0 use case |
|---|---|---|
| Text / code | 8,192 tokens | Function bodies, docstrings, NL queries |
| Images (PNG, JPEG) | max 6 per request | Architecture diagrams in READMEs |
| PDFs | max 6 pages | Design docs, specs linked from code |
| Audio (MP3, WAV) | max 80 seconds | (future: voice queries) |
| Video (MP4, MOV) | max 120 seconds | (future: recorded walkthroughs) |

Because all modalities share one vector space, a query like "JWT validation flow" surfaces
a matching function *even if the function has no text comments* — the code structure
itself is semantically comparable to the natural-language query.

**Model specs:**
- Model ID: `gemini-embedding-2-preview`
- Output dimensions: 128–3,072 (recommended: 768 / 1,536 / 3,072 via Matryoshka)
- Parameter: `output_dimensionality` controls dimension at call time

**Task instruction format (instruction-based, not enum):**

Unlike the older `gemini-embedding-001` model that used a `task_type` enum
(`RETRIEVAL_DOCUMENT`, `CODE_RETRIEVAL_QUERY`, etc.), Gemini Embedding 2 encodes
the task as a **natural language instruction prefix** in the content itself:

```
# At index time (document side):
"task: search result | query: {embedding_input_text}"

# At query time (query side):
"task: search query | query: {user_question}"

# For symmetric tasks (clustering, classification):
"task: classification | query: {content}"
```

This means the context builder must prepend the appropriate task prefix to every
embedding input — both when indexing and when handling user queries.

**Go SDK call (multimodal example):**
```go
// google.golang.org/genai
result, err := client.Models.EmbedContent(ctx, "gemini-embedding-2-preview",
    &genai.EmbedContentRequest{
        Contents: []*genai.Content{{
            Parts: []genai.Part{
                genai.Text("task: search result | query: func ValidateJWT(token string) error {...}"),
                genai.Blob{MIMEType: "image/png", Data: diagramBytes},
            },
        }},
        OutputDimensionality: ptr(3072),
    },
)
```

---

## 5. Directory Layout

Organized as **ports-and-adapters** (hexagonal architecture). Domain logic in
`internal/domain/` and `internal/app/` has no dependency on SurrealDB, Gemini,
or tree-sitter — those live in `internal/adapters/`. See `BACKEND.md` for full
implementation details of each package.

```
commit0/
├── main.go                        # package main — wires Cobra root command
├── go.mod / go.sum
├── .env.example
│
├── docs/                          # Design documents
│   ├── ARCHITECTURE.md            # This document — high-level vision
│   ├── DATABASE.md                # SurrealDB 3.0 schema, indexes, queries
│   └── BACKEND.md                 # Core backend architecture, services, adapters
│
├── cmd/                           # CLI — driving adapter (thin layer)
│   ├── root.go                    # Global flags, config init
│   ├── wire.go                    # Dependency injection — wires all adapters + services
│   ├── index.go                   # `commit0 index <path|url>`  → IndexService
│   ├── query.go                   # `commit0 query "<question>"` → QueryService
│   ├── trace.go                   # `commit0 trace <symbol>`    → TraceService
│   ├── blast.go                   # `commit0 blast <symbol>`    → BlastService
│   ├── serve.go                   # `commit0 serve`             → HTTP Server
│   └── db.go                      # `commit0 db start|stop`     → DBManager
│
├── internal/                      # Private packages
│   │
│   ├── domain/                    # PORT INTERFACES + domain errors
│   │   ├── ports.go               # GraphStore, VectorIndex, TextIndex,
│   │   │                          # Embedder, LLMExplainer, Parser, FileWalker
│   │   └── errors.go              # DomainError (NotFound, RateLimit, Timeout, ...)
│   │
│   ├── app/                       # APPLICATION SERVICES (orchestration)
│   │   ├── index_service.go       # Walk → parse → embed → store pipeline
│   │   ├── query_service.go       # Embed → parallel search → RRF → explain
│   │   ├── trace_service.go       # Symbol resolve → graph traverse → explain
│   │   ├── blast_service.go       # Reverse transitive traversal → explain
│   │   ├── repo_service.go        # Repository CRUD + lifecycle
│   │   ├── session_service.go     # Multi-turn conversation context (Phase 3)
│   │   ├── context_builder.go     # Code + graph neighborhood → embedding text
│   │   ├── embed_batcher.go       # Accumulate → batch 100/request → Gemini API
│   │   └── fusion.go              # Reciprocal Rank Fusion (vector + FTS + centrality)
│   │
│   ├── adapters/                  # DRIVEN ADAPTERS (infrastructure)
│   │   │
│   │   ├── surreal/               # SurrealDB 3.0 adapter
│   │   │   ├── client.go          # WebSocket conn, auth, reconnect
│   │   │   ├── schema.go          # ApplySchema() — embedded schema.surql
│   │   │   ├── graph_store.go     # → implements GraphStore (upsert, traverse)
│   │   │   ├── vector_index.go    # → implements VectorIndex (HNSW search)
│   │   │   ├── text_index.go      # → implements TextIndex (BM25 search)
│   │   │   └── lifecycle.go       # Start/stop local SurrealDB process
│   │   │
│   │   ├── gemini/                # Gemini API adapter
│   │   │   ├── embedder.go        # → implements Embedder (batch, retry, cache)
│   │   │   └── explainer.go       # → implements LLMExplainer (streaming)
│   │   │
│   │   ├── treesitter/            # tree-sitter adapter (CGO)
│   │   │   ├── parser.go          # → implements Parser
│   │   │   ├── resolver.go        # Type resolution pass (methods, interfaces)
│   │   │   └── lang/              # Per-language extractors
│   │   │       ├── golang.go      # func_decl, method_decl, type_spec, call_expr
│   │   │       ├── python.go      # function_def, class_def, import, call
│   │   │       ├── typescript.go  # function_decl, method_def, class_decl, call_expr
│   │   │       └── javascript.go  # same as TS
│   │   │
│   │   ├── http/                  # Echo HTTP server — driving adapter
│   │   │   ├── server.go          # App factory, route registration, middleware
│   │   │   ├── middleware.go      # CORS, request ID, logging, recovery
│   │   │   └── handlers.go        # Request → Service → SSE/JSON response
│   │   │
│   │   └── walker/                # Filesystem walker
│   │       └── fs_walker.go       # → implements FileWalker (.gitignore, filters)
│   │
│   ├── infra/                     # Cross-cutting infrastructure
│   │   └── retry/
│   │       └── retry.go           # Exponential backoff + jitter
│   │
│   └── config/
│       └── config.go              # Typed config struct, Viper binding
│
├── pkg/                           # Exported types (for potential SDK use)
│   └── types/
│       ├── ast.go                 # CodeNode, CodeEdge, NodeKind, EdgeKind
│       └── result.go              # QueryResult, TraceResult, BlastResult, TimingInfo
│
└── assets/                        # Embedded into binary via go:embed
    └── schema.surql               # SurrealDB 3.0 DDL (HNSW, COMPUTED, REFERENCE)
```

### CLI → Service Mapping

| CLI Command | Application Service | Adapter(s) Used |
|---|---|---|
| `commit0 index` | IndexService | FileWalker → Parser → Embedder → GraphStore |
| `commit0 query` | QueryService | Embedder → VectorIndex + TextIndex → LLMExplainer |
| `commit0 trace` | TraceService | GraphStore (traverse) → LLMExplainer |
| `commit0 blast` | BlastService | GraphStore (reverse traverse) → LLMExplainer |
| `commit0 serve` | HTTP Server | All services exposed via Echo REST + SSE |
| `commit0 db` | DBManager | SurrealDB lifecycle (start/stop/status) |

---

## 6. Graph Data Model (SurrealDB 3.0 Schema)

> Full schema with all fields, computed fields, references, DEFINE API, and
> capacity planning is documented in `DATABASE.md`. This section provides the
> essential overview.

### 6.1 Node Tables

```sql
-- Repository (multi-repo support)
DEFINE TABLE repo SCHEMAFULL;
DEFINE FIELD slug            ON repo TYPE string ASSERT string::len($value) > 0;
DEFINE FIELD path            ON repo TYPE string;
DEFINE FIELD languages       ON repo TYPE set<string>;
DEFINE FIELD last_commit     ON repo TYPE option<string>;
DEFINE FIELD last_indexed_at ON repo TYPE option<datetime>;
DEFINE FIELD is_stale        ON repo COMPUTED                       -- 3.0: evaluated on read
    last_indexed_at IS NONE OR time::now() - last_indexed_at > 24h;
DEFINE INDEX repo_slug_idx   ON repo FIELDS slug UNIQUE;

-- Source file
DEFINE TABLE file SCHEMAFULL;
DEFINE FIELD path            ON file TYPE string;
DEFINE FIELD repo            ON file TYPE record<repo>
    REFERENCE ON DELETE CASCADE;                                    -- 3.0: referential integrity
DEFINE FIELD language        ON file TYPE string;
DEFINE FIELD content_hash    ON file TYPE string;
DEFINE FIELD embedding       ON file TYPE option<array<float>>;

-- Function / Method
DEFINE TABLE function SCHEMAFULL;
DEFINE FIELD name            ON function TYPE string;
DEFINE FIELD qualified       ON function TYPE string;               -- pkg.Receiver.Method
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
-- 3.0 computed fields: derived on read, zero storage cost
DEFINE FIELD call_count      ON function COMPUTED count(<-calls<-function);
DEFINE FIELD centrality      ON function COMPUTED
    count(<-calls<-function) + count(->calls->function);
DEFINE FIELD is_entry_point  ON function COMPUTED
    count(<-calls<-function) == 0 AND count(->calls->function) > 0;

-- Class / Struct / Interface
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
DEFINE FIELD content_hash    ON class TYPE string;
DEFINE FIELD embedding       ON class TYPE option<array<float>>;

-- Module / Package
DEFINE TABLE module SCHEMAFULL;
DEFINE FIELD name            ON module TYPE string;
DEFINE FIELD path            ON module TYPE string;
DEFINE FIELD repo            ON module TYPE record<repo>
    REFERENCE ON DELETE CASCADE;
DEFINE FIELD language        ON module TYPE string;
DEFINE FIELD embedding       ON module TYPE option<array<float>>;
```

### 6.2 Edge Tables

```sql
-- function A calls function B
DEFINE TABLE calls SCHEMAFULL;
DEFINE FIELD in              ON calls TYPE record<function>;
DEFINE FIELD out             ON calls TYPE record<function>;
DEFINE FIELD call_site       ON calls TYPE string;    -- "file.go:42"
DEFINE FIELD is_dynamic      ON calls TYPE bool;      -- interface dispatch
DEFINE FIELD call_type       ON calls TYPE string      -- direct | interface | callback
    VALUE "direct";

-- file imports module
DEFINE TABLE imports SCHEMAFULL;
DEFINE FIELD in              ON imports TYPE record<file>;
DEFINE FIELD out             ON imports TYPE record<module>;
DEFINE FIELD alias           ON imports TYPE option<string>;

-- file/module defines function or class
DEFINE TABLE defines SCHEMAFULL;
DEFINE FIELD in              ON defines TYPE record<file | module>;
DEFINE FIELD out             ON defines TYPE record<function | class>;

-- class B inherits / implements class A
DEFINE TABLE inherits SCHEMAFULL;
DEFINE FIELD in              ON inherits TYPE record<class>;
DEFINE FIELD out             ON inherits TYPE record<class>;
DEFINE FIELD kind            ON inherits TYPE string   -- extends | implements | embeds
    VALUE "extends";

-- function uses class (instantiation, type annotation, struct literal)
DEFINE TABLE uses SCHEMAFULL;
DEFINE FIELD in              ON uses TYPE record<function>;
DEFINE FIELD out             ON uses TYPE record<class>;
```

### 6.3 Vector Indexes (HNSW — SurrealDB 3.0)

SurrealDB 3.0 replaces MTREE with HNSW (Hierarchical Navigable Small World)
for faster ANN search with memory-bounded LRU caching and concurrent writes.

```sql
DEFINE INDEX fn_vec_idx   ON function FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;
DEFINE INDEX cls_vec_idx  ON class    FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;
DEFINE INDEX file_vec_idx ON file     FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 150 M 12;
```

### 6.4 Full-Text Indexes

```sql
DEFINE ANALYZER code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER nl_analyzer   TOKENIZERS blank, punctuation FILTERS lowercase, ascii, snowball(english);

DEFINE INDEX fn_name_fts  ON function FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX fn_doc_fts   ON function FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;
DEFINE INDEX cls_name_fts ON class FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;
```

### 6.5 Compound & Count Indexes

```sql
DEFINE INDEX fn_repo_lang_idx   ON function FIELDS repo, language;
DEFINE INDEX fn_qualified_idx   ON function FIELDS repo, qualified UNIQUE;
DEFINE INDEX fn_count_idx       ON function FIELDS repo COUNT;
```

---

## 7. Indexing Pipeline

The pipeline uses **4 concurrent stages** connected by buffered channels.
Each stage runs as a bounded `errgroup` worker pool. Individual file failures
are non-fatal — the pipeline logs and continues. See `BACKEND.md` Section 4.1
for the full Go implementation.

```
Stage 1: Walk          Stage 2: Parse         Stage 3: Embed         Stage 4: Store
(1 goroutine)          (N workers)            (M workers)            (P workers)
FileWalker             errgroup               errgroup               errgroup
                       N = GOMAXPROCS         M = 4 (API bound)     P = 8 (DB bound)

  repoPath             fileCh                 parsedCh               embedCh
     │                 chan FileEntry          chan ParsedFile         chan EmbeddedFile
     ▼                    │                      │                       │
  ┌────────┐             ▼                      ▼                       ▼
  │ Walker │──────▶ ┌──────────┐ ────────▶ ┌───────────┐ ────────▶ ┌──────────┐
  │        │       │  Parser  │           │ Context   │           │ Upsert   │
  │ .git-  │       │ (tree-   │           │ Builder + │           │ (client- │
  │ ignore │       │  sitter) │           │ EmbedBatch│           │  side tx)│
  │ filter │       │ + type   │           │ (100/req) │           │ atomic   │
  │ >500KB │       │ resolver │           │ SHA cache │           │ per file │
  └────────┘       └──────────┘           └───────────┘           └──────────┘
                                                                       │
                       Per file:                                       ▼
                       - AST → nodes + edges          SurrealDB 3.0 (transactional)
                       - call site extraction         UPSERT nodes → RELATE edges
                       - method/interface resolve     idempotent by qualified+repo
```

**Graceful degradation:** Parse errors skip the file. Embed errors skip the
batch. Store errors log and continue. The final report shows:
```
✓ Indexed 1,838 / 1,842 files (4 parse errors, 0 embed errors)
```

---

## 8. Embedding Strategy

### 8.1 Context Text Templates

Every embedding input is wrapped with the task instruction prefix required by
`gemini-embedding-2-preview`. The model encodes task intent via natural language
prefix rather than an enum parameter.

**Function (index time):**
```
task: search result | query: [FUNCTION] {qualified_name}
Language: {language}  File: {file_path}:{start_line}-{end_line}
Signature: {signature}
Calls: {callee_1}, {callee_2}, ...
Called by: {caller_1}, {caller_2}, ...
Doc: {docstring}
---
{code_body}
```

**Class / Struct (index time):**
```
task: search result | query: [CLASS] {qualified_name}
Language: {language}  File: {file_path}
Inherits: {base_1}, {base_2}
Methods: {method_1}, {method_2}, ...
Doc: {docstring}
---
{class_body_first_512_tokens}
```

**File (index time):**
```
task: search result | query: [FILE] {relative_path}
Language: {language}
Exports: {top_level_symbols}
Imports: {imported_modules}
---
{file_first_1024_tokens}
```

**User query (query time):**
```
task: search query | query: {user_natural_language_question}
```

The asymmetry between `search result` (documents) and `search query` (queries) is
how Gemini Embedding 2 achieves high retrieval precision — equivalent to the
`RETRIEVAL_DOCUMENT` / `CODE_RETRIEVAL_QUERY` split in `gemini-embedding-001`.

### 8.2 Multi-Modal Assets (Gemini Embedding 2 advantage)

When a README or doc file contains images (architecture diagrams, screenshots), the
context builder extracts them and sends a multi-part embedding request — the image
and its surrounding text are fused into one vector:

```go
// Up to 6 images per request; combine with surrounding text
parts := []genai.Part{
    genai.Text("task: search result | query: " + surroundingText),
    genai.Blob{MIMEType: "image/png", Data: diagramBytes},
}
```

No separate image pipeline — one call, one vector, same space as code.

### 8.4 Cache & Incremental Indexing

```go
hash := sha256(embeddingInputText)
if existing.ContentHash == hash {
    continue  // skip re-embedding
}
```

Re-indexing a repo after a small diff only re-embeds changed functions.

---

## 9. Query Flows

### 9.1 Natural Language → Code

The query pipeline runs **vector ANN search and full-text search in parallel**,
then fuses results via **Reciprocal Rank Fusion (RRF)** with a graph centrality
boost. See `BACKEND.md` Section 4.2 for full implementation.

```
User: "where does JWT token validation happen?"
          │
          ▼
    QueryService.Query()
          │
          ▼
    1. Embed: gemini.EmbedQuery("task: search query | query: " + question)
          │  → vector[3072]
          ▼
    2. Parallel search (errgroup):
       ┌──────────────────────────────┬──────────────────────────────┐
       │  VectorIndex.Search()        │  TextIndex.Search()          │
       │  HNSW ANN (cosine, top 20)   │  BM25 full-text (top 20)    │
       │                              │  name, qualified, docstring  │
       └──────────────┬───────────────┴──────────────┬───────────────┘
                      │                              │
                      ▼                              ▼
    3. Reciprocal Rank Fusion:
       score = (1/(60+vec_rank) + 1/(60+fts_rank)) × log(1 + centrality)
          │
          ▼
    4. Top K results (default 10)
          │
          ▼
    5. LLMExplainer.Explain() — streaming via Gemini 2.0 Flash:
       "Given these code excerpts, explain where JWT validation occurs
        and describe the code flow. Include file:line references."
          │
          ▼
    Streamed response → terminal (CLI) / SSE (HTTP) / JSON (API)
```

### 9.2 Call Chain Trace

```
commit0 trace "UserController.createUser" --depth 6
          │
          ▼
    Symbol lookup (exact or vector search)
          │
          ▼
    SurrealDB graph traversal (forward):
      SELECT ->calls->(function) AS callee,
             ->calls->calls->(function) AS callee2, ...
      FROM function:UserController⋅createUser
      FETCH 6;
          │
          ▼
    Build ordered call tree { level, node, file, line }
          │
          ▼
    Gemini: step-by-step prose explanation with file:line anchors
```

### 9.3 Blast Radius

```
commit0 blast "UserRepository.save"
          │
          ▼
    Symbol lookup
          │
          ▼
    SurrealDB reverse traversal (who calls this, transitively):
      SELECT <-calls<-(function) AS caller,
             <-calls<-calls<-(function) AS caller2, ...
      FROM function:UserRepository⋅save;
          │
          ▼
    Deduplicate, annotate with hop distance
    Group by module/package
          │
          ▼
    Output table:
      hop | function              | file               | module
      1   | OrderService.create   | order/service.go:88| order
      2   | OrderHandler.post     | api/orders.go:34   | api
      ...
```

---

## 10. CLI Reference

```bash
# Manage local SurrealDB
commit0 db start          # start embedded SurrealDB (file mode, ~/.commit0/db)
commit0 db stop
commit0 db status

# Index
commit0 index ./my-repo                          # index current language set
commit0 index ./my-repo --lang go,python         # specific languages
commit0 index https://github.com/org/repo        # clone + index
commit0 index ./my-repo --workers 8              # parallelism

# Query
commit0 query "where is the auth middleware?"    # NL search + explain
commit0 query "UserService" --exact              # symbol lookup

# Trace call chain
commit0 trace "pkg.Handler.ServeHTTP" --depth 8  # forward call chain
commit0 trace "pkg.Handler.ServeHTTP" --reverse  # who calls this

# Blast radius
commit0 blast "pkg.DB.Save"                      # all transitive callers
commit0 blast "pkg.DB.Save" --json               # JSON output

# HTTP API server (for IDE integrations)
commit0 serve --port 8080 --cors "*"
```

---

## 11. HTTP API Endpoints

```
POST /api/v1/index
  { "repo_path": "...", "languages": ["go","python"], "exclude": ["vendor"] }
  → { "job_id": "...", "status": "indexing", "progress": 0 }

GET  /api/v1/index/:job_id
  → { "status": "done", "stats": { "files": 142, "functions": 1840, "edges": 4200 } }

POST /api/v1/query
  { "q": "JWT validation", "top_k": 10, "repo": "my-repo" }
  → { "results": [{ "qualified_name", "file_path", "start_line", "end_line",
                    "score", "snippet", "explanation" }] }

POST /api/v1/trace
  { "symbol": "UserController.createUser", "depth": 6, "repo": "my-repo" }
  → SSE stream: call tree nodes + final prose explanation

POST /api/v1/blast-radius
  { "symbol": "UserRepository.save", "repo": "my-repo" }
  → { "target": {...}, "affected": [{ "hop", "qualified_name", "file_path" }],
      "summary": "..." }

GET  /api/v1/repos
  → [{ "id", "path", "stats", "last_indexed_at" }]
```

---

## 12. Supported Languages — Phase 1

| Language | tree-sitter grammar | Extracted nodes |
|----------|---------------------|-----------------|
| Go | `tree-sitter-go` | `func_decl`, `method_decl`, `type_spec`, `import_decl`, `call_expr` |
| Python | `tree-sitter-python` | `function_def`, `class_def`, `import`, `call` |
| TypeScript | `tree-sitter-typescript` | `function_decl`, `method_def`, `class_decl`, `import`, `call_expr` |
| JavaScript | `tree-sitter-javascript` | same as TS |

Phase 2: Java, Rust, Ruby, C/C++ — same `lang.Extractor` interface, no core changes.

---

## 13. Configuration

**Precedence** (highest wins): CLI flags → env vars → config file → defaults.

See `BACKEND.md` Section 11 for the full typed Go config struct.

```bash
# ~/.commit0/config.yaml  or  .env in project root

# SurrealDB 3.0
COMMIT0_SURREAL_URL=ws://localhost:8000/rpc       # WebSocket for streaming queries
COMMIT0_SURREAL_HTTP=http://localhost:8000          # HTTP for DEFINE API endpoints
COMMIT0_SURREAL_NS=commit0
COMMIT0_SURREAL_DB=codebase
COMMIT0_SURREAL_USER=root
COMMIT0_SURREAL_PASS=root
COMMIT0_SURREAL_STRICT=true                        # 3.0: enforce schema on all tables

# Google Gemini
COMMIT0_GEMINI_API_KEY=AIza...
COMMIT0_GEMINI_EMBED_MODEL=gemini-embedding-2-preview
COMMIT0_GEMINI_LLM_MODEL=gemini-2.0-flash
COMMIT0_GEMINI_EMBED_DIMS=3072        # 128-3072; recommended: 768 | 1536 | 3072
COMMIT0_GEMINI_EMBED_BATCH=100        # max 100 inputs per batch call

# HNSW Vector Index Tuning
COMMIT0_HNSW_EFC=200                  # Construction-time search breadth
COMMIT0_HNSW_M=16                     # Max connections per HNSW node
COMMIT0_HNSW_TYPE=F32                 # Vector storage: F32 (half mem of F64)

# Indexer
COMMIT0_MAX_FILE_KB=500
COMMIT0_WORKERS=0                     # 0 = GOMAXPROCS
COMMIT0_EXCLUDE=vendor,.git,node_modules,dist,__pycache__
COMMIT0_LANGUAGES=go,python,typescript,javascript

# Query
COMMIT0_TOP_K=10
COMMIT0_TRACE_DEPTH=8
COMMIT0_MIN_SCORE=0.70
COMMIT0_HNSW_EFFORT=40               # Query-time search breadth (higher = more precise)

# HTTP Server
COMMIT0_PORT=8080
COMMIT0_CORS=*
```

---

## 14. Go Module Dependencies

```go
// go.mod (key dependencies)
require (
    github.com/spf13/cobra                  v1.8.x
    github.com/spf13/viper                  v1.19.x
    github.com/labstack/echo/v4             v4.12.x
    github.com/surrealdb/surrealdb.go/v2    v2.x.x    // Go SDK 1.0 (SurrealDB 3.0 compatible)
    google.golang.org/genai                 v0.x.x    // Gemini SDK (embed + LLM)
    github.com/smacker/go-tree-sitter       v0.0.x
    golang.org/x/sync                       v0.x.x    // errgroup bounded worker pools
    github.com/schollz/progressbar/v3       v3.x.x
    github.com/fatih/color                  v1.x.x
)
```

---

## 15. Build & Distribution

```bash
# Local development
go build -o commit0 .

# Release build (static, stripped)
CGO_ENABLED=1 go build -ldflags="-s -w" -o commit0 .

# Cross-compile (Linux amd64)
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-linux-musl-gcc \
  go build -ldflags="-s -w" -o commit0-linux-amd64 .

# macOS universal binary
lipo -create commit0-darwin-amd64 commit0-darwin-arm64 -output commit0-darwin
```

> **Note on CGO:** tree-sitter requires CGO (`CGO_ENABLED=1`) for its C grammar
> libraries. Cross-compilation requires a musl/cross toolchain. For pure-Go targets
> (disabling tree-sitter grammars), a CGO-free build path will be provided in Phase 2.

**GitHub Actions release workflow** publishes:
- `commit0-darwin-arm64` (Apple Silicon)
- `commit0-darwin-amd64`
- `commit0-linux-amd64`
- `commit0-linux-arm64`
- `commit0-windows-amd64.exe`

---

## 16. Phased Delivery Plan

### Phase 1 — Core Indexer (MVP)
- [ ] Go module scaffold + Cobra CLI skeleton + `cmd/wire.go` dependency injection
- [ ] Domain core: port interfaces (`GraphStore`, `Embedder`, `Parser`, `FileWalker`) in `internal/domain/`
- [ ] Application services: `IndexService`, `QueryService` in `internal/app/`
- [ ] SurrealDB 3.0 adapter: connection, auth, `ApplySchema()` from embedded `schema.surql`
- [ ] SurrealDB schema: HNSW indexes, COMPUTED fields, REFERENCE constraints, STRICT mode
- [ ] FileWalker adapter: filesystem walker with .gitignore support
- [ ] tree-sitter adapter: Parser for Go + Python languages with type resolution
- [ ] Gemini adapter: Embedder (batch 100/request, retry with backoff, SHA-256 cache)
- [ ] Context builder: code body + graph neighborhood → embedding input text
- [ ] `EmbedBatcher`: accumulate → batch → flush to Gemini API
- [ ] GraphStore: transactional upsert (SurrealDB 3.0 client-side transactions)
- [ ] `commit0 index` command with 4-stage pipeline (walk→parse→embed→store)
- [ ] `commit0 query` command (parallel vector+FTS → RRF → Gemini explanation)
- [ ] `commit0 db start/stop` (manage local SurrealDB process)
- [ ] Unit tests with mock port implementations

### Phase 2 — Graph Traversal + Blast Radius
- [ ] Call graph edge extraction from tree-sitter call sites
- [ ] `TraceService` + `BlastService` in `internal/app/`
- [ ] GraphStore: forward/reverse graph traversal via SurrealQL
- [ ] `commit0 trace` — forward call chain with streaming prose explanation
- [ ] `commit0 blast` — reverse transitive traversal with module grouping
- [ ] TypeScript + JavaScript language extractors
- [ ] Graph-context re-embedding (neighborhood augmentation, Phase 1.5)
- [ ] `commit0 serve` — Echo HTTP server with SSE streaming + REST handlers
- [ ] SurrealDB DEFINE API endpoints (DB-native HTTP, parallel to Echo server)
- [ ] Integration tests with testcontainers (SurrealDB)

### Phase 3 — Conversational Interface
- [ ] `SessionService` — multi-turn conversation context stored in SurrealDB
- [ ] Streaming SSE responses for long explanations (trace, blast, query)
- [ ] Incremental re-indexing via SurrealDB changefeeds + git diff
- [ ] Web UI (embedded in binary via `go:embed`, served by Echo)
- [ ] SurrealDB DEFINE BUCKET for embedding input cache

### Phase 4 — Scale + Ecosystem
- [ ] Java, Rust, Ruby language extractors (same `Parser` interface)
- [ ] Multi-repo management + scope-based access control
- [ ] VS Code extension using the HTTP API
- [ ] `commit0 index` watch mode (inotify/FSEvents)
- [ ] Pure-Go CGO-free build path (tree-sitter WASM)
- [ ] Surrealism WASM extensions (custom scoring, language-specific analyzers)

---

## 17. Key Design Decisions

### Why Go + single binary?
A developer tool that requires `pip install`, a virtualenv, or a Node.js runtime creates
friction that kills adoption. A single compiled binary with no runtime dependencies means:
- `curl | sh` or `brew install` installs it in seconds
- Works offline after install
- No version conflicts between projects
- Easy to ship in Docker FROM scratch, CI runners, dev containers

### Why Ports and Adapters?
commit0 orchestrates three complex external systems (SurrealDB, Gemini API,
tree-sitter C libraries) that each have their own failure modes, version changes,
and rate limits. Isolating domain logic behind port interfaces means: every
service is unit-testable without infrastructure, adapters are independently
replaceable, and the same business logic serves CLI, HTTP, and SurrealDB DEFINE
API endpoints. See `BACKEND.md` Section 2 for the full rationale.

### Why SurrealDB 3.0 over Neo4j + Pinecone?
SurrealDB executes **hybrid queries** — graph traversal + HNSW vector ANN +
full-text BM25 in a single SurrealQL statement. Version 3.0 adds COMPUTED
fields (derived centrality metrics), REFERENCE constraints (cascading deletes),
client-side transactions (atomic batch upsert), DEFINE API (DB-native HTTP
endpoints), and changefeeds (incremental re-indexing). This eliminates the need
for separate graph, vector, and search databases. See `DATABASE.md` for the
full schema.

### Why Gemini Embedding 2 specifically?
`gemini-embedding-2-preview` is Google's **first multi-modal embedding model** and
currently the only production embedding model placing text, code, images, audio, video,
and PDFs into one unified vector space. Competing models (OpenAI `text-embedding-3`,
Cohere `embed-v3`) are text/code only.

Key mechanic: the model uses **instruction prefixes** (`task: search result | query: …`
vs `task: search query | query: …`) rather than a `task_type` enum — this is specific
to `gemini-embedding-2-preview` and must be applied correctly at both index and query
time for asymmetric retrieval to work. The older `gemini-embedding-001` used enums;
those do not apply here.

### Single Vector Space = Unified Retrieval
The context builder fuses each function's code body with its graph neighborhood
(callers, callees, module, docstring) into a single embedding input text. The resulting
vector captures both *what the code does* (semantic) and *how it fits in the codebase*
(structural). A user asking "what processes payment?" gets ranked results that balance
semantic relevance with architectural centrality — no separate reranking model needed.
