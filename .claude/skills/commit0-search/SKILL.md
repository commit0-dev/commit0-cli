---
name: commit0-search
description: Semantic code search powered by commit0. TRIGGER when: you need to understand how a feature works, find implementations of a concept, or explore unfamiliar code. Use INSTEAD of Grep for conceptual questions like "how does X work?" or "where is Y implemented?". Grep is better for exact string matches; commit0-search is better for semantic understanding.
---

# commit0 Semantic Search

## How to use

```bash
commit0-cli query "your question" --repo <slug> --no-agent
```

The `--no-agent` flag gives direct results (faster). Without it, an agent does multi-step analysis with tool use (deeper but slower).

## Examples

```bash
# Understanding a feature
commit0-cli query "how does the embedding pipeline work?" --repo commit0-dev/commit0 --no-agent

# Finding implementations
commit0-cli query "where is HMAC authentication implemented?" --repo commit0-dev/commit0 --no-agent

# Finding related code
commit0-cli query "all functions that interact with SurrealDB" --repo commit0-dev/commit0 --no-agent --top-k 20
```

## Output

Returns a table of functions/classes ranked by semantic relevance:
- **Score**: combined vector similarity + full-text match + graph centrality
- **Location**: file:line for each result
- **Explanation**: LLM-generated summary of findings

## When to use this instead of Grep

| Use commit0-search | Use Grep |
|---|---|
| "How does auth work?" | `domain.ErrAuthFailed` |
| "Where is rate limiting?" | `SetRetryCount` |
| "Functions related to sync" | `func.*Sync` |
| Conceptual, architectural | Exact string, symbol name |

## Getting the repo slug

```bash
commit0-cli repo list
```
