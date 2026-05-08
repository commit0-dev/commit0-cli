-- commit0 SurrealDB 3.0 Schema — OpenCodeGraph + RepositoryGraph
-- Applied via ApplySchema() on first run (embedded via go:embed)
--
-- Design: docs/OPEN_CODE_GRAPH.md + docs/REPOSITORY_GRAPH.md
-- Node tables: SCHEMAFULL (HNSW, COMPUTED, FTS)
-- Edge tables: SCHEMALESS (generic RELATE CONTENT $props)
-- Repo: graph node with cascade delete to all code entities

DEFINE NAMESPACE IF NOT EXISTS commit0;
USE NS commit0;
DEFINE DATABASE IF NOT EXISTS codebase;
USE DB codebase;

-- ─────────────────────────────────────────
-- 1. Full-Text Search Analyzers (MUST be defined before tables that use them)
-- ─────────────────────────────────────────

DEFINE ANALYZER OVERWRITE code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER OVERWRITE nl_analyzer   TOKENIZERS blank FILTERS lowercase, ascii, snowball(english);

-- ─────────────────────────────────────────
-- 2. Repository Node (root of the RepositoryGraph)
-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE repo SCHEMAFULL;

DEFINE FIELD OVERWRITE slug            ON repo TYPE string ASSERT string::len($value) > 0;
DEFINE FIELD OVERWRITE path            ON repo TYPE string;
DEFINE FIELD OVERWRITE remote_url      ON repo TYPE option<string>;
DEFINE FIELD OVERWRITE default_branch  ON repo TYPE string VALUE "main";
DEFINE FIELD OVERWRITE languages       ON repo TYPE array<string>;
DEFINE FIELD OVERWRITE last_commit     ON repo TYPE option<string>;
DEFINE FIELD OVERWRITE last_indexed_at ON repo TYPE option<datetime>;

DEFINE FIELD OVERWRITE is_stale ON repo COMPUTED
    last_indexed_at IS NONE OR time::now() - last_indexed_at > 24h;

DEFINE INDEX OVERWRITE repo_slug_idx ON repo FIELDS slug UNIQUE;

-- ─────────────────────────────────────────
-- 3. Code Node Tables (SCHEMAFULL — HNSW + COMPUTED + FTS)
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
DEFINE FIELD OVERWRITE embedding       ON file TYPE option<array<float>>;
DEFINE FIELD OVERWRITE embed_provider  ON file TYPE option<string>;
DEFINE FIELD OVERWRITE summary         ON file TYPE option<string>;
DEFINE FIELD OVERWRITE concepts     ON file TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at   ON file TYPE datetime VALUE time::now();
DEFINE FIELD IF NOT EXISTS confidence         ON file TYPE option<float> VALUE 1.0;
DEFINE FIELD IF NOT EXISTS provenance         ON file TYPE option<object>;

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
DEFINE FIELD OVERWRITE embedding       ON `function` TYPE option<array<float>>;
DEFINE FIELD OVERWRITE embed_provider  ON `function` TYPE option<string>;
DEFINE FIELD OVERWRITE summary         ON `function` TYPE option<string>;
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

-- Temporal fields
DEFINE FIELD IF NOT EXISTS introduced_commit    ON `function` TYPE option<string>;
DEFINE FIELD IF NOT EXISTS introduced_at        ON `function` TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS last_modified_commit ON `function` TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_modified_at     ON `function` TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS removed_commit       ON `function` TYPE option<string>;

-- Confidence and provenance fields
DEFINE FIELD IF NOT EXISTS confidence         ON `function` TYPE option<float> VALUE 1.0;
DEFINE FIELD IF NOT EXISTS provenance         ON `function` TYPE option<object>;

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
DEFINE FIELD OVERWRITE body         ON class TYPE option<string>;
DEFINE FIELD OVERWRITE language     ON class TYPE string;
DEFINE FIELD OVERWRITE kind         ON class TYPE string VALUE "class"
    ASSERT $value IN ["class", "struct", "interface", "enum", "trait", "protocol"];
DEFINE FIELD OVERWRITE content_hash ON class TYPE string;
DEFINE FIELD OVERWRITE embedding       ON class TYPE option<array<float>>;
DEFINE FIELD OVERWRITE embed_provider  ON class TYPE option<string>;
DEFINE FIELD OVERWRITE summary         ON class TYPE option<string>;
DEFINE FIELD OVERWRITE concepts     ON class TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at   ON class TYPE datetime VALUE time::now();

DEFINE FIELD OVERWRITE method_count ON class COMPUTED count(->defines->`function`);
DEFINE FIELD OVERWRITE depth        ON class COMPUTED count(->inherits->class);

DEFINE INDEX OVERWRITE cls_qualified_idx  ON class FIELDS repo, qualified UNIQUE;
DEFINE INDEX OVERWRITE cls_repo_lang_idx  ON class FIELDS repo, language;

-- Temporal fields
DEFINE FIELD IF NOT EXISTS introduced_commit    ON class TYPE option<string>;
DEFINE FIELD IF NOT EXISTS introduced_at        ON class TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS last_modified_commit ON class TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_modified_at     ON class TYPE option<datetime>;

-- Confidence and provenance fields
DEFINE FIELD IF NOT EXISTS confidence         ON class TYPE option<float> VALUE 1.0;
DEFINE FIELD IF NOT EXISTS provenance         ON class TYPE option<object>;

-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE module SCHEMAFULL CHANGEFEED 7d;

DEFINE FIELD OVERWRITE name       ON module TYPE string;
DEFINE FIELD OVERWRITE path       ON module TYPE string;
DEFINE FIELD OVERWRITE file_path  ON module TYPE string;
DEFINE FIELD OVERWRITE repo_slug  ON module TYPE string;
DEFINE FIELD OVERWRITE repo       ON module TYPE record<repo> REFERENCE ON DELETE CASCADE;
DEFINE FIELD OVERWRITE language   ON module TYPE string;
DEFINE FIELD OVERWRITE docstring  ON module TYPE option<string>;
DEFINE FIELD OVERWRITE embedding       ON module TYPE option<array<float>>;
DEFINE FIELD OVERWRITE embed_provider  ON module TYPE option<string>;
DEFINE FIELD OVERWRITE summary         ON module TYPE option<string>;
DEFINE FIELD OVERWRITE concepts   ON module TYPE option<array<string>>;
DEFINE FIELD OVERWRITE indexed_at ON module TYPE datetime VALUE time::now();
DEFINE FIELD IF NOT EXISTS confidence         ON module TYPE option<float> VALUE 1.0;
DEFINE FIELD IF NOT EXISTS provenance         ON module TYPE option<object>;

DEFINE FIELD OVERWRITE file_count ON module COMPUTED count(<-imports<-file);

DEFINE INDEX OVERWRITE mod_repo_path_idx ON module FIELDS repo, path UNIQUE;

-- Temporal fields
DEFINE FIELD IF NOT EXISTS introduced_commit    ON file TYPE option<string>;
DEFINE FIELD IF NOT EXISTS introduced_at        ON file TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS last_modified_commit ON file TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_modified_at     ON file TYPE option<datetime>;

-- ─────────────────────────────────────────
-- 4. Vector Indexes (HNSW)
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
-- 5. Full-Text Search Indexes (BM25)
-- ─────────────────────────────────────────

DEFINE INDEX OVERWRITE fn_name_fts      ON `function` FIELDS name
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE fn_qualified_fts ON `function` FIELDS qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE fn_doc_fts       ON `function` FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;
DEFINE INDEX IF NOT EXISTS fn_summary_fts   ON `function` FIELDS summary
    FULLTEXT ANALYZER nl_analyzer BM25;

DEFINE INDEX OVERWRITE cls_name_fts      ON class FIELDS name
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE cls_qualified_fts ON class FIELDS qualified
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE cls_doc_fts       ON class FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;
DEFINE INDEX IF NOT EXISTS cls_summary_fts   ON class FIELDS summary
    FULLTEXT ANALYZER nl_analyzer BM25;

DEFINE INDEX OVERWRITE file_path_fts ON file FIELDS path
    FULLTEXT ANALYZER code_analyzer BM25;

DEFINE INDEX OVERWRITE mod_name_fts ON module FIELDS name
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE mod_path_fts ON module FIELDS path
    FULLTEXT ANALYZER code_analyzer BM25;
DEFINE INDEX OVERWRITE mod_doc_fts  ON module FIELDS docstring
    FULLTEXT ANALYZER nl_analyzer BM25;

-- Temporal indexes on nodes
DEFINE INDEX IF NOT EXISTS fn_introduced_idx ON `function` FIELDS introduced_commit;
DEFINE INDEX IF NOT EXISTS fn_modified_idx   ON `function` FIELDS last_modified_commit;

-- ─────────────────────────────────────────
-- 6. Edge Tables (SCHEMALESS — OpenCodeGraph generic RELATE)
--
-- NO DEFINE FIELD. Properties stored via CONTENT $props.
-- New edge labels auto-create tables via RELATE (SurrealDB 3.0).
-- Only in/out indexes for traversal performance.
-- ─────────────────────────────────────────

DEFINE TABLE calls TYPE RELATION SCHEMALESS;
DEFINE INDEX calls_in_idx   ON calls FIELDS in;
DEFINE INDEX calls_out_idx  ON calls FIELDS out;

DEFINE TABLE imports TYPE RELATION SCHEMALESS;
DEFINE INDEX imports_in_idx  ON imports FIELDS in;
DEFINE INDEX imports_out_idx ON imports FIELDS out;

DEFINE TABLE defines TYPE RELATION SCHEMALESS;
DEFINE INDEX defines_in_idx  ON defines FIELDS in;
DEFINE INDEX defines_out_idx ON defines FIELDS out;

DEFINE TABLE inherits TYPE RELATION SCHEMALESS;
DEFINE INDEX inherits_in_idx  ON inherits FIELDS in;
DEFINE INDEX inherits_out_idx ON inherits FIELDS out;

DEFINE TABLE uses TYPE RELATION SCHEMALESS;
DEFINE INDEX uses_in_idx  ON uses FIELDS in;
DEFINE INDEX uses_out_idx ON uses FIELDS out;

DEFINE TABLE data_flow TYPE RELATION SCHEMALESS;
DEFINE INDEX df_in_idx   ON data_flow FIELDS in;
DEFINE INDEX df_out_idx  ON data_flow FIELDS out;

DEFINE TABLE reads TYPE RELATION SCHEMALESS;
DEFINE INDEX reads_in_idx  ON reads FIELDS in;
DEFINE INDEX reads_out_idx ON reads FIELDS out;

DEFINE TABLE writes TYPE RELATION SCHEMALESS;
DEFINE INDEX writes_in_idx  ON writes FIELDS in;
DEFINE INDEX writes_out_idx ON writes FIELDS out;

DEFINE TABLE route TYPE RELATION SCHEMALESS;
DEFINE INDEX route_in_idx   ON route FIELDS in;
DEFINE INDEX route_out_idx  ON route FIELDS out;

DEFINE TABLE control_flow TYPE RELATION SCHEMALESS;
DEFINE INDEX cf_in_idx  ON control_flow FIELDS in;
DEFINE INDEX cf_out_idx ON control_flow FIELDS out;

DEFINE TABLE data_dep TYPE RELATION SCHEMALESS;
DEFINE INDEX dd_in_idx  ON data_dep FIELDS in;
DEFINE INDEX dd_out_idx ON data_dep FIELDS out;

-- ─────────────────────────────────────────
-- 7. System Tables
-- ─────────────────────────────────────────

DEFINE TABLE IF NOT EXISTS memory SCHEMAFULL;

DEFINE FIELD OVERWRITE tier        ON memory TYPE string
    ASSERT $value IN ["working", "session", "persistent"];
DEFINE FIELD OVERWRITE session_id  ON memory TYPE option<string>;
DEFINE FIELD OVERWRITE repo_slug   ON memory TYPE string;
DEFINE FIELD OVERWRITE content     ON memory TYPE string;
DEFINE FIELD OVERWRITE concepts    ON memory TYPE option<array<string>>;
DEFINE FIELD OVERWRITE embedding   ON memory TYPE option<array<float>>;
DEFINE FIELD OVERWRITE token_count ON memory TYPE int VALUE 0;
DEFINE FIELD OVERWRITE created_at  ON memory TYPE datetime VALUE time::now();

DEFINE INDEX IF NOT EXISTS mem_session_idx ON memory FIELDS session_id;
DEFINE INDEX IF NOT EXISTS mem_tier_idx    ON memory FIELDS tier;

DEFINE TABLE IF NOT EXISTS commit_history SCHEMAFULL;

DEFINE FIELD OVERWRITE hash       ON commit_history TYPE string;
DEFINE FIELD OVERWRITE repo_slug  ON commit_history TYPE string;
DEFINE FIELD OVERWRITE message    ON commit_history TYPE string;
DEFINE FIELD OVERWRITE author     ON commit_history TYPE string;
DEFINE FIELD OVERWRITE timestamp  ON commit_history TYPE datetime;
DEFINE FIELD OVERWRITE files      ON commit_history TYPE array<string>;
DEFINE FIELD OVERWRITE indexed    ON commit_history TYPE bool VALUE false;

DEFINE INDEX IF NOT EXISTS ch_hash_idx ON commit_history FIELDS repo_slug, hash UNIQUE;
DEFINE INDEX IF NOT EXISTS ch_ts_idx   ON commit_history FIELDS repo_slug, timestamp;

DEFINE TABLE OVERWRITE chat_session SCHEMAFULL;
DEFINE FIELD OVERWRITE repo_slug  ON chat_session TYPE string;
DEFINE FIELD OVERWRITE created_at ON chat_session TYPE datetime VALUE time::now();
DEFINE FIELD OVERWRITE updated_at ON chat_session TYPE datetime VALUE time::now();

DEFINE INDEX IF NOT EXISTS chat_sess_repo_idx ON chat_session FIELDS repo_slug;

DEFINE TABLE OVERWRITE chat_message SCHEMAFULL;
DEFINE FIELD OVERWRITE session_id ON chat_message TYPE string;
DEFINE FIELD OVERWRITE role       ON chat_message TYPE string
    ASSERT $value IN ["user", "assistant", "system"];
DEFINE FIELD OVERWRITE content    ON chat_message TYPE string;
DEFINE FIELD OVERWRITE created_at ON chat_message TYPE datetime VALUE time::now();

DEFINE INDEX IF NOT EXISTS chat_msg_sess_idx ON chat_message FIELDS session_id;

-- P2P sync tables
DEFINE TABLE IF NOT EXISTS peer SCHEMAFULL;
DEFINE FIELD OVERWRITE name       ON peer TYPE string;
DEFINE FIELD OVERWRITE address    ON peer TYPE string;
DEFINE FIELD OVERWRITE last_seen  ON peer TYPE option<datetime>;
DEFINE INDEX IF NOT EXISTS peer_name_idx ON peer FIELDS name UNIQUE;

DEFINE TABLE IF NOT EXISTS sync_scope SCHEMAFULL;
DEFINE FIELD OVERWRITE repo_slug  ON sync_scope TYPE string;
DEFINE FIELD OVERWRITE peer_name  ON sync_scope TYPE string;
DEFINE INDEX IF NOT EXISTS scope_repo_idx ON sync_scope FIELDS repo_slug;

-- ─────────────────────────────────────────
-- 8. Schema Version
-- ─────────────────────────────────────────

DEFINE TABLE OVERWRITE schema_version SCHEMAFULL;
DEFINE FIELD OVERWRITE version    ON schema_version TYPE int;
DEFINE FIELD OVERWRITE applied_at ON schema_version TYPE datetime VALUE time::now();

-- Initial version stamp (actual version set by Go ApplySchema after DDL success)
INSERT INTO schema_version (version) VALUES (1) ON DUPLICATE KEY UPDATE version = 1;
