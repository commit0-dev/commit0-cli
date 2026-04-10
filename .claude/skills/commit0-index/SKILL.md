---
name: commit0-index
description: Index and re-index the codebase with commit0. TRIGGER when: starting a new session on a commit0-indexed project, after making multi-file changes, before using commit0-search/trace/blast. Use this skill proactively to keep the code graph fresh.
---

# commit0 Index Management

## Before first use — check if repo is indexed

Run this at the start of every session:

```bash
commit0-cli repo list 2>/dev/null
```

If the repo is not listed or the server is not running:

```bash
# Start server in background (if not running)
commit0 serve &

# Index the current project
commit0-cli index .
```

## After significant changes — re-index

After modifying more than 3 files, re-index so that commit0-search and commit0-trace reflect the current code:

```bash
commit0-cli index .
```

Incremental indexing only processes changed files (via ContentHash comparison), so re-indexing is fast (~seconds for small changes).

## When to re-index

- After adding new functions, classes, or files
- After changing function signatures or call patterns
- After refactoring (moving/renaming functions)
- Before running commit0-impact on recently changed code
