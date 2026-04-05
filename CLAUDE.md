# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# commit0 — Claude Coding Guide

> Graph-based source code analyzer. Single Go binary. SurrealDB 3.0 + Gemini Embedding 2 + tree-sitter.

## Critical Rules

- `internal/domain/` and `pkg/types/` must **never** import external packages.
- All external access goes through port interfaces in `internal/domain/ports.go`.
- `internal/app/` services compose ports only — no direct adapter imports.
- Use domain error types from `internal/domain/errors.go`, not raw `fmt.Errorf`.
- Never unbounded goroutines — use `errgroup.WithContext` + `SetLimit(N)`.

## Stack

- **Module:** `github.com/commit0-dev/commit0` | **Go:** 1.26+ (CGO required for tree-sitter)
- **DB:** SurrealDB 3.0 | **Embeddings:** `gemini-embedding-2-preview` (3072-dim) | **LLM:** Gemini 2.0 Flash
- **AST:** `smacker/go-tree-sitter` | **CLI:** Cobra + Viper | **HTTP:** Echo v4

## Commands

```bash
make build          # CGO_ENABLED=1 go build -trimpath … -o commit0 .
make test           # go test -count=1 -timeout=5m ./...
make test-race      # + race detector
make test-cover     # internal/app coverage — enforces 98% threshold
make lint           # golangci-lint (mirrors pre-push hook and CI)
make lint-fix       # golangci-lint --fix
make install-hooks  # pre-commit (fmt/vet) + pre-push (golangci-lint)

go test -count=1 -run TestQueryService ./internal/app/...
```

## Architecture

See [docs/LAYOUT.md](docs/LAYOUT.md) for the full annotated directory tree.

`SurrealAdapter` is a unified adapter implementing `GraphStore`, `VectorIndex`, and `TextIndex`. `AsVectorIndex()` / `AsTextIndex()` return typed interface wrappers over the same connection. `wireServeServices()` opens one SurrealDB connection shared across all services.

**Index pipeline:** Walk → Parse → Embed → Store via buffered channels. Non-fatal errors skip + log + continue.
**Query pipeline:** Vector ANN + BM25 FTS in parallel → Reciprocal Rank Fusion → LLM stream (`internal/app/fusion.go`).
**Embed batching:** Up to 100 inputs per `EmbedBatch` call; SHA-256 content hash for deduplication.

## Testing

`internal/app/` unit tests use in-memory stubs for all ports (`stubs_test.go`). Coverage: **98%** threshold on `internal/app/...`. Adapter integration tests require live SurrealDB + `GEMINI_API_KEY`. Compile-time check pattern: `_ domain.GraphStore = (*SurrealAdapter)(nil)`.

## SurrealDB 3.0 Specifics

Use `HNSW` (not `MTREE`), `COMPUTED` (not `<future>`), `LET $var`, `type::record()`, `rand::id()`, `FULLTEXT ANALYZER`, `REFERENCE ON DELETE CASCADE`. See [docs/DATABASE.md](docs/DATABASE.md).

## Gemini Embedding 2

- Index prefix: `"task: search result | query: {content}"`
- Query prefix: `"task: search query | query: {user_question}"`
- SDK: `google.golang.org/genai` → `client.Models.EmbedContent(ctx, "gemini-embedding-2-preview", ...)`

## Custom Skills

`.claude/skills/`: `commit0-go`, `commit0-surrealdb`, `commit0-treesitter`, `commit0-gemini`

## Further Reading

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) · [docs/BACKEND.md](docs/BACKEND.md) · [docs/DATABASE.md](docs/DATABASE.md)
