-- commit0 SurrealDB 3.0 Schema
-- Applied via ApplySchema() on first run (embedded via go:embed)

DEFINE NAMESPACE IF NOT EXISTS commit0;
USE NS commit0;
DEFINE DATABASE IF NOT EXISTS codebase STRICT;
USE DB codebase;

-- ─────────────────────────────────────────
-- Node Tables
-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE repo SCHEMAFULL;

DEFINE FIELD OVERWRITE slug            ON repo TYPE string ASSERT string::len($value) > 0;
DEFINE FIELD OVERWRITE path            ON repo TYPE string;
DEFINE FIELD OVERWRITE remote_url      ON repo TYPE option<string>;
DEFINE FIELD OVERWRITE default_branch  ON repo TYPE string VALUE "main";
DEFINE FIELD OVERWRITE languages       ON repo TYPE array<string>;
DEFINE FIELD OVERWRITE last_commit     ON repo TYPE option<string>;
DEFINE FIELD OVERWRITE last_indexed_at ON repo TYPE option<datetime>;

-- Remove legacy stats object (OVERWRITE does not drop old field definitions).
REMOVE FIELD IF EXISTS stats ON repo;
UPDATE repo UNSET stats;

DEFINE FIELD OVERWRITE is_stale ON repo COMPUTED
    last_indexed_at IS NONE OR time::now() - last_indexed_at > 24h;

DEFINE INDEX OVERWRITE repo_slug_idx ON repo FIELDS slug UNIQUE;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE file SCHEMAFULL CHANGEFEED 7d;

DEFINE FIELD OVERWRITE path         ON file TYPE string;
DEFINE FIELD OVERWRITE file_path    ON file TYPE string;
DEFINE FIELD OVERWRITE repo_slug    ON file TYPE string;
DEFINE FIELD OVERWRITE repo         ON file TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE language     ON file TYPE string;
DEFINE FIELD OVERWRITE content_hash ON file TYPE string;
DEFINE FIELD OVERWRITE line_count   ON file TYPE int VALUE 0;
DEFINE FIELD OVERWRITE size_bytes   ON file TYPE int VALUE 0;
DEFINE FIELD OVERWRITE embedding    ON file TYPE option<array<float>>;
DEFINE FIELD OVERWRITE summary      ON file TYPE option<string>;
DEFINE FIELD OVERWRITE concepts     ON file TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at   ON file TYPE datetime VALUE time::now();

DEFINE FIELD OVERWRITE symbol_count ON file COMPUTED
    count(->defines->`function`) + count(->defines->class);

DEFINE INDEX OVERWRITE file_repo_path_idx ON file FIELDS repo, path UNIQUE;
DEFINE INDEX OVERWRITE file_repo_lang_idx ON file FIELDS repo, language;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE `function` SCHEMAFULL CHANGEFEED 7d;

DEFINE FIELD OVERWRITE name         ON `function` TYPE string;
DEFINE FIELD OVERWRITE qualified    ON `function` TYPE string;
DEFINE FIELD OVERWRITE file_path    ON `function` TYPE string;
DEFINE FIELD OVERWRITE repo_slug    ON `function` TYPE string;
DEFINE FIELD OVERWRITE file         ON `function` TYPE record<file> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE repo         ON `function` TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE start_line   ON `function` TYPE int;
DEFINE FIELD OVERWRITE end_line     ON `function` TYPE int;
DEFINE FIELD OVERWRITE signature    ON `function` TYPE string;
DEFINE FIELD OVERWRITE docstring    ON `function` TYPE option<string>;
DEFINE FIELD OVERWRITE body         ON `function` TYPE string;
DEFINE FIELD OVERWRITE language     ON `function` TYPE string;
DEFINE FIELD OVERWRITE content_hash ON `function` TYPE string;
DEFINE FIELD OVERWRITE embedding    ON `function` TYPE option<array<float>>;
DEFINE FIELD OVERWRITE summary      ON `function` TYPE option<string>;
DEFINE FIELD OVERWRITE concepts     ON `function` TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at   ON `function` TYPE datetime VALUE time::now();
DEFINE FIELD OVERWRITE visibility   ON `function` TYPE string VALUE "public"
    ASSERT $value IN ["public", "private", "protected", "internal", "package"];

DEFINE FIELD OVERWRITE call_count     ON `function` COMPUTED count(<-calls<-`function`);
DEFINE FIELD OVERWRITE callee_count   ON `function` COMPUTED count(->calls->`function`);
DEFINE FIELD OVERWRITE centrality     ON `function` COMPUTED
    count(<-calls<-`function`) + count(->calls->`function`);
DEFINE FIELD OVERWRITE is_leaf        ON `function` COMPUTED count(->calls->`function`) == 0;
DEFINE FIELD OVERWRITE is_entry_point ON `function` COMPUTED
    count(<-calls<-`function`) == 0 AND count(->calls->`function`) > 0;

DEFINE INDEX OVERWRITE fn_qualified_idx   ON `function` FIELDS repo, qualified UNIQUE;
DEFINE INDEX OVERWRITE fn_repo_lang_idx   ON `function` FIELDS repo, language;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE class SCHEMAFULL CHANGEFEED 7d;

DEFINE FIELD OVERWRITE name         ON class TYPE string;
DEFINE FIELD OVERWRITE qualified    ON class TYPE string;
DEFINE FIELD OVERWRITE file_path    ON class TYPE string;
DEFINE FIELD OVERWRITE repo_slug    ON class TYPE string;
DEFINE FIELD OVERWRITE file         ON class TYPE record<file> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE repo         ON class TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE start_line   ON class TYPE int;
DEFINE FIELD OVERWRITE end_line     ON class TYPE int;
DEFINE FIELD OVERWRITE docstring    ON class TYPE option<string>;
DEFINE FIELD OVERWRITE language     ON class TYPE string;
DEFINE FIELD OVERWRITE kind         ON class TYPE string VALUE "class"
    ASSERT $value IN ["class", "struct", "interface", "enum", "trait", "protocol"];
DEFINE FIELD OVERWRITE content_hash ON class TYPE string;
DEFINE FIELD OVERWRITE embedding    ON class TYPE option<array<float>>;
DEFINE FIELD OVERWRITE summary      ON class TYPE option<string>;
DEFINE FIELD OVERWRITE concepts     ON class TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at   ON class TYPE datetime VALUE time::now();

DEFINE FIELD OVERWRITE method_count ON class COMPUTED count(->defines->`function`);
DEFINE FIELD OVERWRITE depth        ON class COMPUTED count(->inherits->class);

DEFINE INDEX OVERWRITE cls_qualified_idx  ON class FIELDS repo, qualified UNIQUE;
DEFINE INDEX OVERWRITE cls_repo_lang_idx  ON class FIELDS repo, language;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE module SCHEMAFULL CHANGEFEED 7d;

DEFINE FIELD OVERWRITE name       ON module TYPE string;
DEFINE FIELD OVERWRITE path       ON module TYPE string;
DEFINE FIELD OVERWRITE file_path  ON module TYPE string;
DEFINE FIELD OVERWRITE repo_slug  ON module TYPE string;
DEFINE FIELD OVERWRITE repo       ON module TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE language   ON module TYPE string;
DEFINE FIELD OVERWRITE docstring  ON module TYPE option<string>;
DEFINE FIELD OVERWRITE embedding  ON module TYPE option<array<float>>;
DEFINE FIELD OVERWRITE summary    ON module TYPE option<string>;
DEFINE FIELD OVERWRITE concepts   ON module TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at ON module TYPE datetime VALUE time::now();

DEFINE FIELD OVERWRITE file_count ON module COMPUTED count(<-imports<-file);

DEFINE INDEX OVERWRITE mod_repo_path_idx ON module FIELDS repo, path UNIQUE;

-- ─────────────────────────────────────────
-- Edge Tables
-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE calls TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE call_site  ON calls TYPE string;
DEFINE FIELD OVERWRITE is_dynamic ON calls TYPE bool VALUE false;
DEFINE FIELD OVERWRITE call_type  ON calls TYPE string VALUE "direct"
    ASSERT $value IN ["direct", "interface", "callback", "goroutine", "deferred"];
DEFINE FIELD OVERWRITE repo       ON calls TYPE record<repo>;

DEFINE INDEX OVERWRITE calls_in_idx   ON calls FIELDS in;
DEFINE INDEX OVERWRITE calls_out_idx  ON calls FIELDS out;
DEFINE INDEX OVERWRITE calls_repo_idx ON calls FIELDS repo;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE imports TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE alias       ON imports TYPE option<string>;
DEFINE FIELD OVERWRITE is_wildcard ON imports TYPE bool VALUE false;

DEFINE INDEX OVERWRITE imports_in_idx  ON imports FIELDS in;
DEFINE INDEX OVERWRITE imports_out_idx ON imports FIELDS out;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE defines TYPE RELATION SCHEMAFULL;

DEFINE INDEX OVERWRITE defines_in_idx  ON defines FIELDS in;
DEFINE INDEX OVERWRITE defines_out_idx ON defines FIELDS out;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE inherits TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE kind ON inherits TYPE string VALUE "extends"
    ASSERT $value IN ["extends", "implements", "embeds", "mixes"];

DEFINE INDEX OVERWRITE inherits_in_idx  ON inherits FIELDS in;
DEFINE INDEX OVERWRITE inherits_out_idx ON inherits FIELDS out;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE uses TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE usage_type ON uses TYPE string VALUE "reference"
    ASSERT $value IN ["instantiation", "type_annotation", "struct_literal",
                       "cast", "reference", "return_type"];

DEFINE INDEX OVERWRITE uses_in_idx  ON uses FIELDS in;
DEFINE INDEX OVERWRITE uses_out_idx ON uses FIELDS out;

-- ─────────────────────────────────────────
-- Data-Flow Edge Tables
-- ─────────────────────────────────────────

-- data_flow: function A passes a specific argument to a named parameter of function B.
-- Richer than `calls` — carries what data moves, not just that a call occurred.
DEFINE TABLE OVERWRITE data_flow TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE call_site  ON data_flow TYPE string;
DEFINE FIELD OVERWRITE param_name ON data_flow TYPE option<string>; -- callee parameter name
DEFINE FIELD OVERWRITE arg_expr   ON data_flow TYPE option<string>; -- argument expression at call site
DEFINE FIELD OVERWRITE arg_type   ON data_flow TYPE option<string>; -- resolved type (if available)
DEFINE FIELD OVERWRITE repo       ON data_flow TYPE record<repo>;

DEFINE INDEX OVERWRITE df_in_idx   ON data_flow FIELDS in;
DEFINE INDEX OVERWRITE df_out_idx  ON data_flow FIELDS out;
DEFINE INDEX OVERWRITE df_repo_idx ON data_flow FIELDS repo;

-- ─────────────────────────────────────────

-- reads: function reads a field or global variable of a class/struct.
DEFINE TABLE OVERWRITE reads TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE field_name ON reads TYPE option<string>; -- qualified field, e.g. "User.Email"
DEFINE FIELD OVERWRITE repo       ON reads TYPE record<repo>;

DEFINE INDEX OVERWRITE reads_in_idx  ON reads FIELDS in;
DEFINE INDEX OVERWRITE reads_out_idx ON reads FIELDS out;

-- ─────────────────────────────────────────

-- writes: function writes a field or global variable of a class/struct.
DEFINE TABLE OVERWRITE writes TYPE RELATION SCHEMAFULL;
DEFINE FIELD OVERWRITE field_name ON writes TYPE option<string>;
DEFINE FIELD OVERWRITE repo       ON writes TYPE record<repo>;

DEFINE INDEX OVERWRITE writes_in_idx  ON writes FIELDS in;
DEFINE INDEX OVERWRITE writes_out_idx ON writes FIELDS out;

-- ─────────────────────────────────────────
-- Vector Indexes (HNSW — dimension set by EMBED_DIM config)
-- ─────────────────────────────────────────

DEFINE INDEX OVERWRITE fn_vec_idx   ON `function` FIELDS embedding
    HNSW DIMENSION {{EMBED_DIM}} DIST COSINE TYPE F32 EFC 200 M 16;

DEFINE INDEX OVERWRITE cls_vec_idx  ON class FIELDS embedding
    HNSW DIMENSION {{EMBED_DIM}} DIST COSINE TYPE F32 EFC 200 M 16;

DEFINE INDEX OVERWRITE file_vec_idx ON file FIELDS embedding
    HNSW DIMENSION {{EMBED_DIM}} DIST COSINE TYPE F32 EFC 150 M 12;

DEFINE INDEX OVERWRITE mod_vec_idx  ON module FIELDS embedding
    HNSW DIMENSION {{EMBED_DIM}} DIST COSINE TYPE F32 EFC 150 M 12;

-- ─────────────────────────────────────────
-- Full-Text Search Indexes (BM25)
-- ─────────────────────────────────────────

DEFINE ANALYZER OVERWRITE code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER OVERWRITE nl_analyzer   TOKENIZERS blank FILTERS lowercase, ascii, snowball(english);

DEFINE INDEX OVERWRITE fn_name_fts      ON `function` FIELDS name
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE fn_qualified_fts ON `function` FIELDS qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE fn_doc_fts       ON `function` FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;
DEFINE INDEX OVERWRITE fn_summary_fts   ON `function` FIELDS summary
    FULLTEXT ANALYZER nl_analyzer BM25;

DEFINE INDEX OVERWRITE cls_name_fts      ON class FIELDS name
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE cls_qualified_fts ON class FIELDS qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE cls_doc_fts       ON class FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;
DEFINE INDEX OVERWRITE cls_summary_fts   ON class FIELDS summary
    FULLTEXT ANALYZER nl_analyzer BM25;

DEFINE INDEX OVERWRITE file_path_fts ON file FIELDS path
    FULLTEXT ANALYZER code_analyzer BM25;

DEFINE INDEX OVERWRITE mod_name_fts ON module FIELDS name
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE mod_path_fts ON module FIELDS path
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE mod_doc_fts  ON module FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

-- ─────────────────────────────────────────
-- Schema Version (for migration tracking)
-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE schema_version SCHEMAFULL;
DEFINE FIELD OVERWRITE version    ON schema_version TYPE int;
DEFINE FIELD OVERWRITE applied_at ON schema_version TYPE datetime VALUE time::now();

INSERT INTO schema_version (version) VALUES (1) ON DUPLICATE KEY UPDATE version = 1;
