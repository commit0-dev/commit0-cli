# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Critical Rules

- `internal/domain/` and `pkg/types/` — **zero** external imports, ever.
- `internal/app/` composes port interfaces only — never import adapters.
- `internal/app/agent/delegate.go` uses `ModelFactory`, never concrete model imports.
- Domain errors from `internal/domain/errors.go`, not raw `fmt.Errorf`.
- Bounded goroutines only — `errgroup.WithContext` + `SetLimit(N)`.
- HTTP clients: Resty v3 (`resty.dev/v3`). Never raw `net/http` for outbound.
- HTTP server: Gin (`github.com/gin-gonic/gin`). Never Echo.
- New features: add Gin handler first, then `internal/adapters/client/` method.
- CLI commands call the server via HTTP — never `wireDeps()` or `config.Load()`.

## Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26+ (CGO for tree-sitter) |
| Database | SurrealDB 3.0 (graph + HNSW vector + BM25 FTS) |
| Embeddings | Gemini / Voyage / Ollama (`EMBED_PROVIDER`) |
| LLM | Gemini / OpenRouter / Ollama (`LLM_PROVIDER`) |
| Agent | Google ADK v1.0.0 with ModelFactory injection |
| HTTP Server | Gin + gin-contrib/cors + gin-contrib/requestid |
| HTTP Client | Resty v3 (outbound APIs + CLI→server) |
| CLI | Cobra + Viper |
| AST | smacker/go-tree-sitter (CGO) |

## Architecture

Streamable HTTP client-server. Server (`commit0 serve`) owns all adapters. CLI is a thin HTTP client via `internal/adapters/client/`.

- **Unary**: POST → JSON (query, trace, blast, repo, api)
- **Streaming**: POST → SSE (agent chat, find-root)
- **Async**: POST (start) → GET (poll) (index)

## Commands

```bash
make build          # CGO_ENABLED=1 go build -o commit0 .
make test           # go test -count=1 -timeout=5m ./...
make test-race      # + race detector
make test-cover     # 98% threshold on internal/app/...
make lint           # golangci-lint
```

## commit0 Development Tools

This project uses its own code intelligence for self-development. When the commit0 server is running, use these tools alongside Grep/Glob/Read:

- **Search** — `commit0-cli query "question" --repo commit0-dev/commit0 --no-agent` for conceptual questions. PREFER over Grep for "how does X work?", "where is Y implemented?". Falls back to Grep if server is not running.
- **Impact** — `commit0-cli blast <FunctionName> --repo commit0-dev/commit0` BEFORE modifying any function. Check blast radius. If > 20 affected nodes, proceed with extra caution.
- **Trace** — `commit0-cli trace <symbol> --repo commit0-dev/commit0 --direction forward` to follow call chains. Use `--direction reverse` to find callers. Better than reading file-by-file.
- **Analyze** — `commit0-cli analyze --repo commit0-dev/commit0 --focus all` to self-analyze for architecture violations, dead code, consistency gaps, and hotspots. Run before starting work and after finishing to catch regressions.
- **Re-index** — `commit0-cli index .` after multi-file changes so search/trace/blast reflect current code. Incremental, fast.
- **Check server** — `commit0-cli repo list 2>/dev/null` to verify server is running before using tools.

## When to Reference Docs

| You're working on... | Read this doc |
|---|---|
| Understanding the project mission | [docs/VISION.md](docs/VISION.md) |
| System design, layers, tech stack | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| Services, ports, HTTP API, agent | [docs/BACKEND.md](docs/BACKEND.md) |
| SurrealDB schema, indexes, queries | [docs/DATABASE.md](docs/DATABASE.md) |
| Indexing or retrieval pipeline | [docs/PIPELINE.md](docs/PIPELINE.md) |
| Security analysis, taint, CPG | [docs/SECURITY_ROADMAP.md](docs/SECURITY_ROADMAP.md) |
| Navigating unfamiliar code | [docs/LAYOUT.md](docs/LAYOUT.md) |
| Embedding model selection | [docs/EMBEDDING_RESEARCH.md](docs/EMBEDDING_RESEARCH.md) |
| Local-first / offline strategy | [docs/LOCAL_MODEL.md](docs/LOCAL_MODEL.md) |
| Commit zero feature design | [docs/DESIGN.md](docs/DESIGN.md) |
