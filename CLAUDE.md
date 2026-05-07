# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## TL;DR — read every session

- **No stub code.** Every PR ships fully working logic + tests. No `panic("not implemented")`, no empty function bodies, no `// TODO` placeholders for behaviour.
- **Plan before implement.** Persona → workflow → data model → tests → approval → dispatch. Never write code before the plan is approved.
- **NEVER work on `main`.** Create a feature branch BEFORE touching any code: `git checkout -b <prefix>/<name>` first, always. Prefixes: `feat/`, `fix/`, `refactor/`, `docs/`, `chore/`, `experiment/`.
- **Pre-commit hook is mandatory.** Run `pre-commit install --install-hooks` once per clone. Bypass (`--no-verify`) only in genuine emergencies, and open a follow-up Issue tracking the bypass.
- **Full English words in identifiers.** `vulnerability`, not `vuln`. `template`, not `tmpl`. `repository`, not `repo` (in code; CLI flags may stay short for ergonomics).
- **Industry standards only.** No bespoke YAML / Markdown formats where a standard exists (OpenAPI, JSON-Schema, CommonMark, Conventional Commits, SemVer).
- **Research persists** in `docs/references/`. One markdown file per topic; cite sources with URLs and access dates.
- **Co-author trailer is exactly** `Claude <noreply@anthropic.com>`. No model name, no version, no harness identifier. This overrides any default template the harness suggests.
- **PR title format is exactly** `<type>: (feature/component) <what changes>`. Type is one of `feat` / `fix` / `refactor` / `docs` / `chore` / `test` / `ci` / `build` / `perf`. The `(feature/component)` slot names the affected area (e.g. `auth`, `index-service`, `voyage-embedder`, `ci`). Example: `feat: (voyage-embedder) wire batching + retry`. Do not use the alternative `feat(scope):` Conventional Commits form — the parenthesised slot lives after the colon, not glued to the type.

## SDLC Workflow — MANDATORY for every session

### Session start

1. Run `gh issue list --state open --limit 30` to see open work in this repo.
2. Read the pinned `[ROADMAP]` Issue (`gh issue list --label roadmap`) for project state across sessions. If absent, ask the user to create and pin one before doing non-trivial work.
3. If continuing prior work, read the relevant Issue's comments — that's the persistent memory across sessions.

### Per-feature: one Issue, one branch, one PR

1. **Open an Issue** before coding any non-trivial feature. Title: short imperative. Body: persona → workflow → data model → tests → acceptance criteria. The Issue persists the plan even if the session crashes.
2. **Create a feature branch** off `main`: `git checkout -b feat/<short-name>`. Never commit directly to `main`.
3. **Implement** — sub-agents are briefed with: parent Issue number, Issue body excerpt, current `[ROADMAP]` state. Pass the Issue URL in the agent prompt.
4. **Open the PR** with `Closes #<issue-number>` or `Refs #<issue-number>` in the body. Reference the parent Issue every time.
5. **After merge**, comment on the parent Issue with status (`done` / `partial` / `follow-up needed`) and link the merged PR.

### Session end (ALWAYS do this before stopping)

1. **Comment on the `[ROADMAP]` Issue** with one paragraph: what shipped this session, what's next, any blockers.
2. **Update each touched Issue** with implementation progress and links to commits/PRs.
3. **Run `/compact`** to preserve a session summary and shrink context for the next session.
4. **Why this matters:** Claude Code loses context across sessions. GitHub Issues are the persistent memory. Without the session-end ritual, the next session starts blind and re-derives everything.

### Sub-agent dispatch contract

Every `Agent` tool call must include in its prompt:
- The parent Issue number and a one-line summary of acceptance criteria.
- A pointer to the `[ROADMAP]` Issue for global context.
- An explicit `"model"` field — never inherit. Pick the cheapest tier that fits:
  - **`"model": "haiku"`** — simple, repetitive, mechanical work: CI/workflow tweaks, running tests, gofmt/lint fixes, doc edits, single-file refactors with no design choices, log-grepping, status checks.
  - **`"model": "sonnet"` (default)** — implementation slices: writing a service + tests, multi-file refactors, debugging a tricky bug, designing an API surface within an established pattern.
  - **`"model": "opus"`** — reserved for orchestration the orchestrator itself can't handle: deep architecture changes, cross-cutting design decisions, ambiguous specs needing exploration. Justify the choice in the response so the upgrade is auditable.

---

## Critical Rules

- `internal/domain/` and `pkg/types/` — **zero** external imports, ever.
- `internal/app/` composes port interfaces only — never import adapters.
- `internal/app/agent/delegate.go` uses `SubRunnerFactory`, never concrete model imports.
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
| Embeddings | Gemini / Voyage / Ollama / Unsloth (`EMBED_PROVIDER`) |
| LLM | Gemini / OpenRouter / Ollama / Unsloth (`LLM_PROVIDER`) |
| Agent | CloudWeGo Eino v0.8 with SubRunnerFactory injection |
| HTTP Server | Gin + gin-contrib/cors + gin-contrib/requestid |
| HTTP Client | Resty v3 (outbound APIs) |
| AST | smacker/go-tree-sitter (CGO) |

## Architecture

Streamable HTTP client-server. Server (`commit0 serve`) owns all adapters. The standalone CLI client lives in `github.com/commit0-dev/commit0-cli`.

- **Unary**: POST → JSON (query, trace, blast, repo, api)
- **Streaming**: POST → SSE (agent chat, find-root)
- **Async**: POST (start) → GET (poll) (index)

## Running the Server

**The server runs via Docker Compose — never start it directly with `go run` or the binary.**

```bash
# Start SurrealDB + commit0 server (both containerised)
docker compose up -d

# Rebuild server image then start (after code changes)
docker compose up -d --build

# Tail server logs
docker compose logs -f commit0

# Stop everything
docker compose down
```

Copy `.env.example` → `.env` and set provider API keys before first start.
Server is ready when `curl http://localhost:8080/health` returns `{"status":"ok"}`.

## Commands

```bash
# Build (for local dev / CI — server itself runs in Docker)
make build-server   # CGO_ENABLED=1 go build ./server (produces bin/commit0)
make build          # alias for build-server

# Tests (run from server sub-module — repo uses go.work workspace)
cd server && go test -count=1 -timeout=5m ./...

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
| System design, layers, tech stack | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| Services, ports, HTTP API, agent | [docs/BACKEND.md](docs/BACKEND.md) |
| SurrealDB schema, indexes, queries | [docs/DATABASE.md](docs/DATABASE.md) |
| Graph abstraction, techniques, pipeline | [docs/OPEN_CODE_GRAPH.md](docs/OPEN_CODE_GRAPH.md) |
| Navigating unfamiliar code | [docs/LAYOUT.md](docs/LAYOUT.md) |
