---
source-issue: https://github.com/commit0-dev/commit0-cli/issues/2
source-repo: commit0-dev/commit0-cli
source-issue-number: 2
ported-at: 2026-05-11T14:38:42.126Z
ported-by: plans/260511-2227-migrate-roadmap-to-plans-kanban
note: Canonical roadmap, accumulates state via session-end notes appended below.
---

# [ROADMAP] commit0-cli

> Migrated from GitHub Issue [#2](https://github.com/commit0-dev/commit0-cli/issues/2) on 2026-05-11.
> Issue created 2026-05-06T00:28:29Z. Source-of-truth from this date forward: this file.

## Original body

# Persistent Roadmap — commit0-cli

This Issue is the **cross-session memory** for the commit0 CLI client. Every Claude Code session starts by reading this Issue and ends by commenting on it. See [`CLAUDE.md`](https://github.com/commit0-dev/commit0-cli/blob/main/CLAUDE.md) → "SDLC Workflow" for the full ritual.

## How to use this Issue

- **Session start:** read the most recent session-log comments and "Active streams" below.
- **Per feature:** open a separate Issue with persona → workflow → data model → tests → acceptance criteria. Link it here.
- **Session end:** comment with `## Session YYYY-MM-DD` — what shipped, what's next, blockers, branches/PRs touched.

## Active streams

- [ ] (none yet)

## Done recently

- (none yet)

## Up next (queued)

- (none yet)

## Open questions / decisions needed

- (none yet)

## Architectural invariants (do not break)

Baked into `CLAUDE.md`; reproduced for quick reference:

- `pkg/types/` — **zero** external imports.
- `sdk/` — only `pkg/types` and `resty.dev/v3`.
- `cmd/` — only `sdk/` and `pkg/types`. Calls server via HTTP.
- **Never** import the server repo (`github.com/commit0-dev/commit0/server/...`).
- **Never** add CGO. CLI must cross-compile without CGO.
- HTTP client: Resty v3. No raw `net/http`.

## Session log


---

## Session notes (post-migration)

_Append `## Session note — YYYY-MM-DD` sections here at session end. Replaces the prior "comment on the GitHub roadmap issue" ritual._
