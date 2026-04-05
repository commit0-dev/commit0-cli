-- commit0 SurrealDB 3.0 Schema
-- Applied via ApplySchema() on first run (embedded via go:embed)

DEFINE NAMESPACE IF NOT EXISTS commit0;
USE NS commit0;
DEFINE DATABASE IF NOT EXISTS codebase STRICT;
USE DB codebase;

-- ─────────────────────────────────────────
-- Node Tables
-- ─────────────────────────────────────────

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

DEFINE FIELD is_stale ON repo COMPUTED
    last_indexed_at IS NONE OR time::now() - last_indexed_at > 24h;

DEFINE INDEX repo_slug_idx ON repo FIELDS slug UNIQUE;

-- ─────────────────────────────────────────

DEFINE TABLE file SCHEMAFULL CHANGEFEED 7d;

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

DEFINE INDEX file_repo_path_idx ON file FIELDS repo, path UNIQUE;
DEFINE INDEX file_repo_lang_idx ON file FIELDS repo, language;
DEFINE INDEX file_count_idx     ON file FIELDS repo COUNT;

-- ─────────────────────────────────────────

DEFINE TABLE function SCHEMAFULL CHANGEFEED 7d;

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

DEFINE FIELD call_count     ON function COMPUTED count(<-calls<-function);
DEFINE FIELD callee_count   ON function COMPUTED count(->calls->function);
DEFINE FIELD centrality     ON function COMPUTED
    count(<-calls<-function) + count(->calls->function);
DEFINE FIELD is_leaf        ON function COMPUTED count(->calls->function) == 0;
DEFINE FIELD is_entry_point ON function COMPUTED
    count(<-calls<-function) == 0 AND count(->calls->function) > 0;

DEFINE INDEX fn_qualified_idx   ON function FIELDS repo, qualified UNIQUE;
DEFINE INDEX fn_repo_lang_idx   ON function FIELDS repo, language;
DEFINE INDEX fn_count_idx       ON function FIELDS repo COUNT;

-- ─────────────────────────────────────────

DEFINE TABLE class SCHEMAFULL CHANGEFEED 7d;

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

DEFINE INDEX cls_qualified_idx  ON class FIELDS repo, qualified UNIQUE;
DEFINE INDEX cls_repo_lang_idx  ON class FIELDS repo, language;
DEFINE INDEX cls_count_idx      ON class FIELDS repo COUNT;

-- ─────────────────────────────────────────

DEFINE TABLE module SCHEMAFULL CHANGEFEED 7d;

DEFINE FIELD name       ON module TYPE string;
DEFINE FIELD path       ON module TYPE string;
DEFINE FIELD repo       ON module TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD language   ON module TYPE string;
DEFINE FIELD docstring  ON module TYPE option<string>;
DEFINE FIELD embedding  ON module TYPE option<array<float>>;
DEFINE FIELD indexed_at ON module TYPE datetime VALUE time::now();

DEFINE FIELD file_count ON module COMPUTED count(<-imports<-file);

DEFINE INDEX mod_repo_path_idx ON module FIELDS repo, path UNIQUE;

-- ─────────────────────────────────────────
-- Edge Tables
-- ─────────────────────────────────────────

DEFINE TABLE calls SCHEMAFULL;
DEFINE FIELD in         ON calls TYPE record<function>;
DEFINE FIELD out        ON calls TYPE record<function>;
DEFINE FIELD call_site  ON calls TYPE string;
DEFINE FIELD is_dynamic ON calls TYPE bool VALUE false;
DEFINE FIELD call_type  ON calls TYPE string VALUE "direct"
    ASSERT $value IN ["direct", "interface", "callback", "goroutine", "deferred"];
DEFINE FIELD repo       ON calls TYPE record<repo>;

DEFINE INDEX calls_in_idx   ON calls FIELDS in;
DEFINE INDEX calls_out_idx  ON calls FIELDS out;
DEFINE INDEX calls_repo_idx ON calls FIELDS repo;

-- ─────────────────────────────────────────

DEFINE TABLE imports SCHEMAFULL;
DEFINE FIELD in          ON imports TYPE record<file>;
DEFINE FIELD out         ON imports TYPE record<module>;
DEFINE FIELD alias       ON imports TYPE option<string>;
DEFINE FIELD is_wildcard ON imports TYPE bool VALUE false;

DEFINE INDEX imports_in_idx  ON imports FIELDS in;
DEFINE INDEX imports_out_idx ON imports FIELDS out;

-- ─────────────────────────────────────────

DEFINE TABLE defines SCHEMAFULL;
DEFINE FIELD in  ON defines TYPE record<file | module>;
DEFINE FIELD out ON defines TYPE record<function | class>;

DEFINE INDEX defines_in_idx  ON defines FIELDS in;
DEFINE INDEX defines_out_idx ON defines FIELDS out;

-- ─────────────────────────────────────────

DEFINE TABLE inherits SCHEMAFULL;
DEFINE FIELD in   ON inherits TYPE record<class>;
DEFINE FIELD out  ON inherits TYPE record<class>;
DEFINE FIELD kind ON inherits TYPE string VALUE "extends"
    ASSERT $value IN ["extends", "implements", "embeds", "mixes"];

DEFINE INDEX inherits_in_idx  ON inherits FIELDS in;
DEFINE INDEX inherits_out_idx ON inherits FIELDS out;

-- ─────────────────────────────────────────

DEFINE TABLE uses SCHEMAFULL;
DEFINE FIELD in         ON uses TYPE record<function>;
DEFINE FIELD out        ON uses TYPE record<class>;
DEFINE FIELD usage_type ON uses TYPE string VALUE "reference"
    ASSERT $value IN ["instantiation", "type_annotation", "struct_literal",
                       "cast", "reference", "return_type"];

DEFINE INDEX uses_in_idx  ON uses FIELDS in;
DEFINE INDEX uses_out_idx ON uses FIELDS out;

-- ─────────────────────────────────────────
-- Vector Indexes (HNSW — Gemini Embedding 2, 3072-dim)
-- ─────────────────────────────────────────

DEFINE INDEX fn_vec_idx   ON function FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;

DEFINE INDEX cls_vec_idx  ON class FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 200 M 16;

DEFINE INDEX file_vec_idx ON file FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 150 M 12;

DEFINE INDEX mod_vec_idx  ON module FIELDS embedding
    HNSW DIMENSION 3072 DIST COSINE TYPE F32 EFC 150 M 12;

-- ─────────────────────────────────────────
-- Full-Text Search Indexes (BM25)
-- ─────────────────────────────────────────

DEFINE ANALYZER code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER nl_analyzer   TOKENIZERS blank, punctuation FILTERS lowercase, ascii, snowball(english);

DEFINE INDEX fn_name_fts  ON function FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX fn_doc_fts   ON function FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

DEFINE INDEX cls_name_fts ON class FIELDS name, qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX cls_doc_fts  ON class FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

DEFINE INDEX file_path_fts ON file FIELDS path
    FULLTEXT ANALYZER code_analyzer BM25;

DEFINE INDEX mod_name_fts ON module FIELDS name, path
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX mod_doc_fts  ON module FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

-- ─────────────────────────────────────────
-- Schema Version (for migration tracking)
-- ─────────────────────────────────────────

DEFINE TABLE schema_version SCHEMAFULL;
DEFINE FIELD version    ON schema_version TYPE int;
DEFINE FIELD applied_at ON schema_version TYPE datetime VALUE time::now();

INSERT INTO schema_version (version) VALUES (1) ON DUPLICATE KEY UPDATE version = 1;
