# Conventions

Naming, commit, branch, and PR conventions for this repository. Conformant tooling (pre-commit hooks, CI) enforces most of this automatically; the prose here exists to explain WHY.

## Branch names

`<prefix>/<short-name>` where prefix is one of:

| Prefix | When to use |
|---|---|
| `feat/` | A new feature or new public surface |
| `fix/` | A bug fix; behavior was broken, now correct |
| `refactor/` | Internal change with no observable behavior change |
| `docs/` | Documentation only |
| `chore/` | Build, CI, dependencies, internal tooling |
| `experiment/` | Throwaway exploration; not intended to merge |
| `test/` | Test-only additions/changes |

`<short-name>` is kebab-case, ≤4 words, ≤40 chars. Examples: `feat/oidc-middleware`, `fix/cache-invalidation`, `chore/bump-go-1-23`.

## PR title

Exactly: `<type>: (feature/component) <what changes>`

- **Type** is one of: `feat`, `fix`, `refactor`, `docs`, `chore`, `test`, `ci`, `build`, `perf`
- **(feature/component)** is the affected area (`auth`, `api`, `ci`, `voyage-embedder`, …)
- **What changes** is imperative, ≤72 chars total

✅ `feat: (auth) wire OIDC middleware`
✅ `fix: (cache) invalidate on schema bump`
✅ `chore: (ci) cheap-fail-first DAG`
❌ `feat(auth): wire OIDC middleware` — no glued form
❌ `Add OIDC middleware` — no type prefix
❌ `feat: wire OIDC middleware` — no component slot

## Commit messages

Subject line follows the same `<type>: (feature/component) <what>` format as PR titles. Body wraps at 72 chars, explains WHY (not WHAT — the diff is the WHAT).

Trailers:

- `Refs #<N>` for any related Issue
- `Closes #<N>` for scope-bound child Issues. The roadmap is no longer an Issue (see *Plans-kanban discipline* below); the per-feature plan dir is linked via the PR body's `Plan: plans/<date>-<slug>/` line, not via a commit trailer
- NO `Co-Authored-By` — sole-author rule (enforced by commit-msg hook)
- NO "Generated with X" attribution — same rule

## Identifier naming

Full English words, no abbreviations:

| Bad | Good |
|---|---|
| `vuln` | `vulnerability` |
| `tmpl` | `template` |
| `repo` (the noun) | `repository` |
| `cfg` | `config` (acceptable) or `configuration` |
| `auth` | `auth` (acceptable — universally understood) |
| `db` | `database` |
| `req` / `resp` | `request` / `response` |

CLI flag names may use short forms (`--repo`, `--cfg`) for ergonomics. Code identifiers must use the long form.

## Plans-kanban discipline

`plans/ROADMAP.md` is persistent cross-session memory. Migrated from GitHub Issue #2 on 2026-05-11 (see commit0-dev/commit0 `plans/260511-2227-migrate-roadmap-to-plans-kanban/`). See global `~/.claude/CLAUDE.md` → *Plans-kanban discipline* for the hard rules; the project-local summary:

1. **Exactly one** `plans/ROADMAP.md` per repository, ever. Free-form markdown — not a `ck plan` plan.
2. **Never delete it** — `git rm plans/ROADMAP.md` requires explicit user direction. Session-end ritual is *append* a `## Session note — YYYY-MM-DD` section, never overwrite or truncate.
3. **Per-feature plans live in `plans/<date>-<slug>/`** managed by `ck plan create`; `ck plan kanban` opens the dashboard at `http://localhost:3456/plans`.
4. **PR body convention** is `Plan: plans/<date>-<slug>/` (or `Plan: n/a` for trivial fixes). The roadmap is a file, not an Issue, so close-keywords (`Closes`, `Fixes`, etc.) targeting the roadmap don't apply — they only apply to per-feature Issues, where they work normally.
5. **Milestones land as bullets in the latest session-note**, not as state transitions.

Pre-flight check before every `gh pr create`:

```bash
echo "$PR_BODY" | grep -qE "^Plan: (plans/[0-9]{6}-|n/a)" \
    || { echo "VIOLATION — add 'Plan: plans/<date>-<slug>/' or 'Plan: n/a' to PR body"; exit 1; }
```

## Pre-commit hooks

Mandatory. Run once per fresh clone:

```bash
pre-commit install --install-hooks
```

The repo ships these hook stages:

- `commit-msg` → blocks AI-attribution markers in commit messages
- `pre-commit` → fast checks (whitespace, EOF, large-file, language-specific format/vet, author identity validation)
- `pre-push` → slow checks (full lint suite mirroring CI)

`--no-verify` is for genuine emergencies only. Every bypass requires a follow-up Issue.

## Pre-merge gate

Before flipping a draft PR to ready:

```bash
make pr-ready-check          # Go / Python / Rust
npm run pr-ready-check       # TypeScript / JavaScript
```

The single command runs every gate that CI runs. Each gate is a sub-target so iterating on one is fast.
