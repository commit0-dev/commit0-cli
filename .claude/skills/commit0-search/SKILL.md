---
name: commit0-search
description: Semantic code search powered by commit0. TRIGGER when you need to understand how a feature works, find implementations of a concept, or explore unfamiliar code. Use INSTEAD of Grep for conceptual questions. Grep is better for exact strings; commit0-search is better for semantic understanding. Supports --kind, --file, --no-explain filters.
---

# commit0 Semantic Search

## Basic usage
```bash
commit0-cli query "your question" --repo <slug> --no-agent --no-explain
```

## Filters
```bash
--no-explain              # skip LLM explanation (1.5s vs 10s)
--no-agent                # direct search, no multi-step agent
--kind function           # only functions (also: class, file, module)
--kind class              # only structs/interfaces
--file server/internal/   # only results in this directory
--top-k 20                # number of results (default 10)
```

## Examples
```bash
# Find port interfaces
commit0-cli query "port interfaces" --repo <slug> --no-agent --no-explain --kind class

# Functions in a specific package
commit0-cli query "sync" --repo <slug> --no-agent --no-explain --file server/internal/app/sync

# Find HTTP handlers only
commit0-cli query "HTTP handler" --repo <slug> --no-agent --no-explain --kind function --file server/internal/adapters/http/

# Fast broad search
commit0-cli query "authentication" --repo <slug> --no-agent --no-explain --top-k 20
```

## When to use this vs Grep

| commit0-search | Grep |
|---|---|
| "How does auth work?" | `domain.ErrAuthFailed` |
| "Functions related to sync" | `func.*Sync` |
| "Port interfaces in domain" | `type.*interface` |
| Conceptual, architectural | Exact string, symbol name |
