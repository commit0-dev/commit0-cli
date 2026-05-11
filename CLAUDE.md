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
- **No Co-Authored-By trailer.** Do NOT add any `Co-Authored-By` line to commits. All commits must appear as sole-author from the committer.
- **PR title format is exactly** `<type>: (feature/component) <what changes>`. Type is one of `feat` / `fix` / `refactor` / `docs` / `chore` / `test` / `ci` / `build` / `perf`. The `(feature/component)` slot names the affected area (e.g. `auth`, `index-service`, `voyage-embedder`, `ci`). Example: `feat: (voyage-embedder) wire batching + retry`. Do not use the alternative `feat(scope):` Conventional Commits form — the parenthesised slot lives after the colon, not glued to the type.
- **Dogfood commit0 — non-negotiable.** When working in this repo, `commit0-cli` (and the MCP tools once reloaded) are the **default** code-intelligence path. Grep/Read are the fallback when commit0 can't answer. A maintainer who can't use the tool for daily work has shipped trash. See **Code-Intelligence Workflow** below for the exact tool routing.
- **PRs ship DRAFT first; flip to ready only after `make pr-ready-check` exits 0.** Always open with `gh pr create --draft`. The `pr-ready-check` Makefile target mirrors every required CI status check (build, tests in both modules, coverage ≥ 96%, golangci-lint, govulncheck) in one command. Run it before `gh pr ready <N>`. CI skips draft PRs (Issue #19), so iterating in draft burns zero Actions minutes — the moment you flip ready, the pipeline fires once on the final commit.
- **No Co-Authored-By trailer — enforced by commit-msg hook.** The `no-ai-attribution` commit-msg hook (pattern catalog at `scripts/ai_attribution_patterns.py`, allowlist at `.commit-msg-policy.allowlist`) blocks AI/agentic attribution trailers, Generated-with markers, robot emoji, vendor emails, and branding brackets in commit messages. All commits must appear as sole-author from the committer. The `author-not-bot` pre-commit hook additionally rejects commits whose git author/committer identity looks like a bot account.
- **CI cost discipline (Issue #19).** ci.yml and main.yml use a cheap-fail-first DAG (`lint → test → {govulncheck, image-scan}`), `paths-ignore` so docs-only changes consume zero Actions minutes, draft-PR skip, fork-PR guard on image-scan, pinned `ubuntu-22.04` runners, and a cached `govulncheck` binary.

## SDLC Workflow — MANDATORY for every session

### Plans-kanban discipline (HARD REQUIREMENT, do not violate)

`plans/ROADMAP.md` is **persistent cross-session memory**. Migrated from GitHub Issue #2 on 2026-05-11 (see commit0-dev/commit0 `plans/260511-2227-migrate-roadmap-to-plans-kanban/`). Treat it like a database, not a sprint board. These rules are HARD — not guidelines.

1. **Exactly ONE `plans/ROADMAP.md` per repository, ever.** Free-form markdown — not a `ck plan` plan, not on the kanban board. Verify presence with:
   ```bash
   test -f plans/ROADMAP.md   # must exist
   ```
2. **NEVER delete `plans/ROADMAP.md`.** Not when a milestone ships. Not when direction pivots. Not when scope changes. Deleting it deletes the cross-session memory. `git rm plans/ROADMAP.md` requires explicit user direction.
3. **PR-body convention: `Plan: plans/<date>-<slug>/`** — one line linking the per-feature plan dir, or `Plan: n/a` for trivial fixes. Per-feature GitHub Issues are still useful and `Closes #<feature-issue>` is fine for those — the rule only applies to the roadmap layer. The roadmap is a file, not an Issue, so close-keywords targeting it don't apply (mechanically impossible).
   - **Pre-flight before every `gh pr create` or `gh pr edit --body`:**
     ```bash
     echo "$PR_BODY" | grep -qE "^Plan: (plans/[0-9]{6}-|n/a)" \
         || { echo "VIOLATION — add 'Plan: plans/<date>-<slug>/' or 'Plan: n/a' to PR body"; exit 1; }
     ```
4. **Per-feature plans live in `plans/<date>-<slug>/`** managed by `ck plan create`. The kanban dashboard (`ck plan kanban`, opens at `http://localhost:3456/plans`) shows in-flight per-feature plans, *not* the roadmap.
5. **Milestones land as bullets in the latest session-note**, not as state transitions or new files. Session-end ritual is *append* a `## Session note — YYYY-MM-DD` section to `plans/ROADMAP.md`, never overwrite or truncate.
6. **Pre-flight before every session-end commit:**
   ```bash
   test -f plans/ROADMAP.md \
       || { echo "VIOLATION — plans/ROADMAP.md missing; restore from git or ask user before recreating"; exit 1; }
   ```
7. **If you discover `plans/ROADMAP.md` was deleted or truncated:** restore from git history (`git log -- plans/ROADMAP.md`, `git checkout <sha>~1 -- plans/ROADMAP.md`), append a corrective session-note explaining what happened, do not silently continue.

### Historical: GitHub-Issue ROADMAP convention (deprecated 2026-05-11)

Before 2026-05-11 this project used a pinned `[ROADMAP]` Issue (#2) with a `roadmap` label, `Refs #2` PR-body linking, and per-session comments on the issue. The 2026-05-08 incident in the parent commit0 repo — closed canonical ROADMAP via close-keyword in a PR body, then re-closed when narrating the lesson with literal backticked close-keywords — motivated the migration. Both classes of violation are mechanically impossible against `plans/ROADMAP.md` because GitHub's auto-close scanner only operates on Issues, not files. The plans-kanban migration eliminates the failure mode rather than papering over it with more pre-flight checks.

### Session start

1. Run `gh issue list --state open --limit 30` to see open per-feature work in this repo.
2. `cat plans/ROADMAP.md` for project state across sessions, then `ck plan status` for in-flight per-feature plans. If `plans/ROADMAP.md` is missing, ask the user before recreating it (see Plans-kanban discipline above).
3. If continuing prior work, read the relevant per-feature plan dir (`plans/<date>-<slug>/`) and the Issue's comments — both are persistent memory across sessions.

### Per-feature: one Plan, one Issue, one branch, one PR

1. **Create a per-feature plan first**: `ck plan create` scaffolds `plans/<date>-<slug>/` (overview + phases). The plan dir is the persistent design doc.
2. **Open an Issue** for non-trivial features. Title: short imperative. Body: persona → workflow → data model → tests → acceptance criteria, plus a `Plan: plans/<date>-<slug>/` line. Skip the Issue for trivial fixes (PR alone is fine, body says `Plan: n/a`).
3. **Create a feature branch** off `main`: `git checkout -b feat/<short-name>`. Never commit directly to `main`.
4. **Implement** — sub-agents are briefed with: parent Issue number (if any), per-feature plan dir path, current `plans/ROADMAP.md` excerpt. Pass the plan dir path in the agent prompt.
5. **Open the PR** with `Closes #<issue-number>` (or `Refs #<n>`) AND `Plan: plans/<date>-<slug>/` (or `Plan: n/a`) in the body. Pre-flight: see Plans-kanban discipline rule 3.
6. **After merge**, comment on the parent Issue with status (`done` / `partial` / `follow-up needed`) and link the merged PR; update the per-feature plan's phase statuses.

### Session end (ALWAYS do this before stopping)

1. **Append a `## Session note — YYYY-MM-DD` section to `plans/ROADMAP.md`** with one paragraph: what shipped this session, what's next, any blockers. **Do NOT overwrite or truncate.** Replaces the prior "comment on the `[ROADMAP]` Issue" ritual.
2. **Update each touched per-feature plan** (`ck plan update` or edit the phase file directly), and each touched Issue with commit/PR links.
3. **Run `/compact`** to preserve a session summary and shrink context for the next session.
4. **Verify discipline before stopping:**
   ```bash
   test -f plans/ROADMAP.md   # must succeed
   ```
   If the file is missing, perform the corrective action in Plans-kanban discipline rule 7 before ending the session.
5. **Why this matters:** Claude Code loses context across sessions. `plans/ROADMAP.md` + per-feature plan dirs are the persistent memory. Without the session-end ritual, the next session starts blind and re-derives everything.

### Sub-agent dispatch contract

Every `Agent` tool call must include in its prompt:
- The parent Issue number (if any) and a one-line summary of acceptance criteria.
- A pointer to `plans/ROADMAP.md` and the per-feature plan dir for global context.
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
