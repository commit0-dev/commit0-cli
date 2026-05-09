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

- `Refs #<N>` for any related Issue (incl. the [ROADMAP] Issue)
- `Closes #<N>` ONLY for scope-bound child Issues — NEVER for the ROADMAP
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

## ROADMAP discipline

The pinned `[ROADMAP]` Issue (carrying the `roadmap` label) is persistent cross-session memory. Hard rules:

1. **Exactly one** `[ROADMAP]` Issue per repository, ever
2. **Never close it** — verify with `gh issue list --label roadmap --state open --json number | jq 'length'` (must be 1)
3. **Never write the close-keyword form** (`Close`, `Closes`, `Closed`, `Fix`, `Fixes`, `Fixed`, `Resolve`, `Resolves`, `Resolved`, case-insensitive) targeting the ROADMAP anywhere — PR body, commit message, merge message. **Backticks do NOT protect.** GitHub's auto-close scanner is regex-based and matches inside code spans
4. **Never open a second roadmap.** If the existing one doesn't fit, update its body and post a comment
5. **Milestones land as comments**, not state transitions

Pre-flight check before every `gh pr create`:

```bash
ROADMAP=$(gh issue list --label roadmap --state open --json number --jq '.[0].number')
echo "$PR_BODY" | grep -iE "(close[sd]?|fix(es|ed)?|resolve[sd]?) +#${ROADMAP}\b" \
    && { echo "VIOLATION — change to Refs #${ROADMAP}"; exit 1; }
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
