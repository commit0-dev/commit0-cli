# CLAUDE.md

This file provides guidance to Claude Code when working in this repository.

## TL;DR — read every session

- **No stub code.** Every PR ships fully working logic + tests. No `panic("not implemented")`, no empty function bodies, no `// TODO` placeholders for behaviour.
- **Plan before implement.** Persona → workflow → data model → tests → approval → dispatch. Never write code before the plan is approved.
- **NEVER work on `main`.** Create a feature branch BEFORE touching any code: `git checkout -b <prefix>/<name>` first, always. Prefixes: `feat/`, `fix/`, `refactor/`, `docs/`, `chore/`, `experiment/`.
- **Pre-commit hook is mandatory.** Run `pre-commit install --install-hooks` once per clone. Bypass (`--no-verify`) only in genuine emergencies, and open a follow-up Issue tracking the bypass.
- **Full English words in identifiers.** `vulnerability`, not `vuln`. `template`, not `tmpl`. `repository`, not `repo` (in code; CLI flags may stay short for ergonomics).
- **Industry standards only.** No bespoke YAML / Markdown formats where a standard exists (OpenAPI, JSON-Schema, CommonMark, Conventional Commits, SemVer).
- **Research persists** in `docs/references/`. One markdown file per topic; cite sources with URLs and access dates.
- **Co-author trailer is exactly** `Claude <noreply@anthropic.com>`. No model name, no version, no harness identifier. This overrides any default template the harness suggests.

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

## What This Repo Is

`commit0-cli` is the **standalone command-line client** for the commit0 server.
It is a pure Go module — no CGO, no server dependencies, cross-compiles trivially.

## Module

```
github.com/commit0-dev/commit0-cli
```

## Critical Rules

- `pkg/types/` — **zero** external imports, ever. Pure data types only.
- `sdk/` — only imports `pkg/types` and `resty.dev/v3`. No server internals.
- `cmd/` — only imports `sdk/` and `pkg/types`. Calls the server via HTTP.
- **Never** import the server repo (`github.com/commit0-dev/commit0/server/...`).
- **Never** add CGO. CLI must cross-compile without CGO.
- HTTP client: Resty v3 (`resty.dev/v3`). Never raw `net/http` for outbound calls.

## Structure

```
commit0-cli/
├── cmd/           # Cobra commands — one file per command
├── sdk/           # HTTP client — one file per API resource
├── pkg/
│   └── types/     # Shared types — zero external imports
└── main.go        # Entry point — sets version, calls cmd.Execute()
```

## Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.24+ (pure Go, no CGO) |
| CLI framework | Cobra + persistent flags |
| HTTP client | Resty v3 |
| Output | tablewriter, ANSI color helpers in color.go |

## Running the Server (required for all CLI commands)

**The commit0 server runs via Docker Compose in the server repo — not from this repo.**

```bash
# From the commit0 server repo:
cd ../commit0          # or wherever commit0/ lives
docker compose up -d   # starts SurrealDB + commit0 server

# Verify it's up before using the CLI:
curl http://localhost:8080/health
# → {"status":"ok","state":"idle","active_jobs":0}
```

## Commands

```bash
make build          # CGO_ENABLED=0 go build -o bin/commit0-cli .
make install        # install to $GOPATH/bin
make test           # go test -count=1 -timeout=5m ./...
make lint           # golangci-lint run
```

## Adding a New Command

1. Add a file `cmd/<name>.go` with `var <name>Cmd = &cobra.Command{...}`
2. Add the corresponding SDK method in `sdk/<resource>.go`
3. Register with `rootCmd.AddCommand(<name>Cmd)` in `init()`
4. No server-side changes needed — CLI only calls HTTP endpoints

## Server URL Resolution

```
--server-url flag  >  COMMIT0_SERVER_URL env  >  http://localhost:8080
```
