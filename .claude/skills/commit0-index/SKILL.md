---
name: commit0-index
description: Index and re-index the codebase with commit0. TRIGGER when starting a new session on a commit0-indexed project, after making multi-file changes, before using commit0-search/trace/blast, or after resolver/extractor changes. Supports --reparse for full re-parse without crash.
---

# commit0 Index Management

## Check server status
```bash
commit0-cli status                    # shows: idle or indexing (N jobs)
commit0-cli repo list                 # list indexed repos with last commit
```

## Index the project
```bash
commit0-cli index .                   # incremental (skips unchanged files, fast)
commit0-cli index . --reparse         # re-parse ALL files (after resolver changes)
commit0-cli index . --force           # delete + re-index (caution: slow on large repos)
```

## When to re-index

| Situation | Command |
|-----------|---------|
| After editing a few files | `commit0-cli index .` (incremental, seconds) |
| After adding/deleting files | `commit0-cli index .` (cleanup removes stale nodes) |
| After resolver/extractor code changes | `commit0-cli index . --reparse` |
| Corrupted index | `commit0-cli index . --force` |

## Concurrent access
Dual connection pool (8 read + 4 write) means queries work DURING indexing.
No need to wait for index to finish before searching/tracing/blasting.

## What the indexer does (6 techniques)
1. **Parse** (tree-sitter) → functions, classes, files, modules + 13 edge types
2. **Summarize** (LLM) → semantic description + concept tags per node
3. **Embed** (Gemini/Voyage/Ollama) → dense vector per node
4. **Store** (SurrealDB) → graph + HNSW vector index + BM25 FTS
5. **Re-embed** → with graph neighborhood context (callers, callees)
6. **Temporal** → stamp introduced_commit/last_modified from git history
7. **Cleanup** → remove nodes for deleted files
