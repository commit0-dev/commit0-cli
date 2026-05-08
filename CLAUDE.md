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
- **Dogfood commit0 — non-negotiable.** When working in this repo, `commit0-cli` (and the MCP tools once reloaded) are the **default** code-intelligence path. Grep/Read are the fallback when commit0 can't answer. A maintainer who can't use the tool for daily work has shipped trash. See **Code-Intelligence Workflow** below for the exact tool routing.

## SDLC Workflow — MANDATORY for every session

### ROADMAP — IMMUTABLE RULES (HARD REQUIREMENT, do not violate)

The `[ROADMAP]` Issue is **persistent cross-session memory**. Treat it like a database, not a sprint board. These rules are HARD — not guidelines, not best practices. Violating any of them means corrective action (reopen, fold, relabel, comment) before proceeding with any other work.

1. **Exactly ONE `[ROADMAP]` issue per repository, ever.** Created once, lives forever. The `roadmap` label is the canonical marker. Verify count with:
   ```bash
   gh issue list --label roadmap --json number,state | jq 'length'   # must be 1
   ```
2. **NEVER close the ROADMAP.** Not when a milestone ships. Not when direction pivots. Not when scope changes. Closing it deletes the cross-session memory.
3. **NEVER write `Closes #<roadmap-id>` in a PR body.** Use `Refs #<roadmap-id>`. The `Closes` directive is reserved for scope-bound items (features, bugs, milestones) — never for the ROADMAP itself. Pre-flight check before every `gh pr create`:
   ```bash
   ROADMAP=$(gh issue list --label roadmap --state open --json number --jq '.[0].number')
   echo "$PR_BODY" | grep -E "^Closes #${ROADMAP}\b" && { echo "VIOLATION — change to Refs"; exit 1; }
   ```
4. **NEVER open a second roadmap.** No `[ROADMAP-2]`, no `[ROADMAP] phase 2`, no parallel tracker for a "new direction." If the existing ROADMAP doesn't fit, **update its body** and post a comment.
5. **Milestones land as comments on the ROADMAP, not as state transitions.** When a milestone ships: the milestone Issue (scope-bound) gets `Closes #<milestone>`; the ROADMAP gets `Refs #<roadmap>` plus a comment summarising what shipped, what's next, and any blockers.
6. **Pre-flight before every `gh issue close`:** verify the issue does NOT carry the `roadmap` label.
   ```bash
   gh issue view "$N" --json labels --jq '.labels[].name' | grep -qx roadmap && { echo "VIOLATION — do not close ROADMAP"; exit 1; }
   ```
7. **If you discover the rule was violated** (closed ROADMAP, second roadmap-labelled issue, `Closes #<roadmap>` in a merged PR): reopen the canonical ROADMAP, remove the `roadmap` label from any imposter, fold the imposter's content into a comment on the canonical one, post a corrective explanation. Do not silently continue.
8. **Lesson learned 2026-05-08.** Closed canonical ROADMAP #15 by letting `Closes #15` ride in PR #59's body, then opened a forbidden `[ROADMAP-2]` #62. Corrective action shipped the same session: reopened #15, folded #62's content as a comment, removed the `roadmap` label from #62, closed #62 with explanation. The rule above exists because the rule was insufficiently loud before. Don't break it again.

### Session start

1. Run `gh issue list --state open --limit 30` to see open work in this repo.
2. Read the pinned `[ROADMAP]` Issue (`gh issue list --label roadmap`) for project state across sessions. If absent, ask the user to create and pin one before doing non-trivial work. Verify there is **exactly one** open issue with the `roadmap` label — more than one is a violation requiring corrective action before any other work begins.
3. If continuing prior work, read the relevant Issue's comments — that's the persistent memory across sessions.

### Per-feature: one Issue, one branch, one PR

1. **Open an Issue** before coding any non-trivial feature. Title: short imperative. Body: persona → workflow → data model → tests → acceptance criteria. The Issue persists the plan even if the session crashes.
2. **Create a feature branch** off `main`: `git checkout -b feat/<short-name>`. Never commit directly to `main`.
3. **Implement** — sub-agents are briefed with: parent Issue number, Issue body excerpt, current `[ROADMAP]` state. Pass the Issue URL in the agent prompt.
4. **Open the PR** with `Closes #<issue-number>` for the scope-bound child Issue, AND `Refs #<roadmap-id>` (NOT `Closes`) for the ROADMAP. The PR body must reference both. Pre-flight: confirm `Closes #N` where N is not the roadmap-labelled issue. See the ROADMAP — IMMUTABLE RULES section above.
5. **After merge**, comment on the parent Issue with status (`done` / `partial` / `follow-up needed`) and link the merged PR.

### Session end (ALWAYS do this before stopping)

1. **Comment on the `[ROADMAP]` Issue** with one paragraph: what shipped this session, what's next, any blockers. **Do NOT close it. Do NOT open a new one.** See ROADMAP — IMMUTABLE RULES.
2. **Update each touched Issue** with implementation progress and links to commits/PRs.
3. **Run `/compact`** to preserve a session summary and shrink context for the next session.
4. **Verify ROADMAP discipline before stopping:**
   ```bash
   gh issue list --label roadmap --state open --json number | jq 'length'   # must print: 1
   ```
   If the count is anything other than 1, perform the corrective action in ROADMAP rule #7 before ending the session.
5. **Why this matters:** Claude Code loses context across sessions. GitHub Issues are the persistent memory. The single canonical ROADMAP is the index into that memory; closing it or splitting it deletes the index.

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

## Code-Intelligence Workflow — commit0 is the default, not the demo

This is a non-negotiable working agreement. commit0's own tools are how the maintainer reasons about this codebase. Grep/Read are the fallback, not the starting point. If commit0 can't answer, that's a bug to fix in commit0 — not a reason to default back to grep.

### Tool routing (use this map, in this order)

| Question shape | First reach for |
|---|---|
| "How does X work?" / "Where is Y implemented?" / conceptual lookup | `commit0-cli query "<question>" --repo commit0-dev/commit0 --no-agent` (or `mcp__commit0__commit0_query`) |
| "Resolve this qualified symbol" | `commit0-cli show <qualified.Name> --repo commit0-dev/commit0` (or `commit0_lookup` + `commit0_show_node`) |
| "What's in this file?" | `commit0-cli ls <path> --repo commit0-dev/commit0` |
| "What calls this?" / "What does this call?" | `commit0-cli trace <symbol> --direction reverse\|forward` (or `commit0_trace`) |
| "What breaks if I change this?" — **before any non-trivial edit** | `commit0-cli blast <symbol>` (or `commit0_blast`). >20 affected ⇒ extra caution. |
| "How does this field flow through the code?" | `commit0-cli flow <symbol> --field <field>` (or `commit0_field_flow`) |
| Cross-cutting checks (architecture, dead code, hotspots) | `commit0-cli analyze --focus all` — run before starting work and after finishing |

### Fallback to Grep/Read is allowed only when

- Server is not running. Verify with `commit0-cli repo list 2>/dev/null`. If empty, Grep is fine.
- Index is stale and re-indexing would cost more than the alternative. Otherwise: `commit0-cli index .` first.
- The exact answer requires literal byte-level matching (specific config string, escape sequence in a regex, etc.) that semantic search can't beat.

When falling back, **say so in the response** ("falling back to Grep because <reason>") so the gap is auditable and we can fix commit0.

### Re-index discipline

After multi-file edits the index goes stale. Run `commit0-cli index .` so subsequent queries reflect current code. Skipping this once is fine; skipping it across a session quietly poisons every later answer.

### Why this matters

A maintainer who can't use the tool for daily work has shipped a tool they don't believe in. Routing your own work through commit0 is also the most actionable feedback channel the project has — friction surfaces resolver gaps and missing edge types as concrete bugs, not abstract roadmap items.

## When to Reference Docs

| You're working on... | Read this doc |
|---|---|
| System design, layers, tech stack | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| Services, ports, HTTP API, agent | [docs/BACKEND.md](docs/BACKEND.md) |
| SurrealDB schema, indexes, queries | [docs/DATABASE.md](docs/DATABASE.md) |
| Graph abstraction, techniques, pipeline | [docs/OPEN_CODE_GRAPH.md](docs/OPEN_CODE_GRAPH.md) |
| Navigating unfamiliar code | [docs/LAYOUT.md](docs/LAYOUT.md) |
