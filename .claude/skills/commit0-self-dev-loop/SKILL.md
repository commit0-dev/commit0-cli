---
name: commit0-self-dev-loop
description: >
  Autonomous self-development loop. Uses ONLY commit0-cli commands to DISCOVER
  issues. Read/Write/Edit allowed to IMPLEMENT fixes. Three persistent files in
  .development/ (gitignored, local only): FINDINGS.md (current issues),
  BACKLOG.md (all issues with status), SOLUTION.md (engineering plan before any
  fix). NO code changes without a solution written first. TRIGGER when: user says
  self-dev, self-improve, use commit0 on itself, or run the loop.
---

# commit0 Self-Development Loop

## Persistent State (`.development/` — local, gitignored)

| File | Purpose | When to update |
|------|---------|----------------|
| `FINDINGS.md` | Current iteration's raw findings | After each discovery pass |
| `BACKLOG.md` | All findings ever, with status tracking | After finding or solving anything |
| `SOLUTION.md` | Engineering plan for the current fix | BEFORE writing any code |

**The rule**: NO code changes without a solution in SOLUTION.md first.
The solution must specify exact files, changes, and verification command.
Fixes MUST follow the written solution.

---

## Two Modes

### Discovery Mode — commit0-cli ONLY

Use these tools to explore the codebase and find gaps. No Grep, Glob, Read, or
Agent subagents. If commit0 can't answer your question — that's a finding.

#### Search (semantic, ranked)
```bash
commit0-cli query "how does X work" --repo <slug> --no-agent --no-explain
commit0-cli query "functions related to sync" --repo <slug> --no-agent --no-explain --top-k 20
```

**Filters:**
```bash
--kind function|class|file|module    # filter by node kind
--file server/internal/app/          # filter by file/directory prefix
--no-explain                         # skip LLM explanation (6x faster)
```

#### Read code
```bash
commit0-cli show <symbol> --repo <slug>          # print function/struct body
commit0-cli show Pull --repo <slug>              # partial names work (fuzzy resolve)
commit0-cli ls <file-path> --repo <slug>         # list all nodes in a file
```

#### Trace call chains
```bash
commit0-cli trace <symbol> --repo <slug> --direction forward   # what does it call?
commit0-cli trace <symbol> --repo <slug> --direction reverse   # who calls it?
commit0-cli trace ImportBundle --repo <slug> --direction reverse --depth 3
```
Partial names work. 4 resolution strategies: exact → same-package → suffix → interface dispatch.

#### Impact analysis
```bash
commit0-cli blast <symbol> --repo <slug>            # transitive blast radius
commit0-cli blast ImportBundle --repo <slug>         # partial names work
```

#### Self-analysis (agent-powered, uses all tools internally)
```bash
commit0-cli analyze --repo <slug> --focus all
commit0-cli analyze --repo <slug> --focus architecture    # hexagonal layer violations
commit0-cli analyze --repo <slug> --focus dead-code       # unreachable functions
commit0-cli analyze --repo <slug> --focus consistency     # handler ↔ SDK ↔ CLI gaps
commit0-cli analyze --repo <slug> --focus hotspots        # high blast-radius nodes
commit0-cli analyze --repo <slug> --focus data-flow       # sensitive data paths, mutations
commit0-cli analyze --repo <slug> --focus temporal        # high-churn risky code
```

#### Server management
```bash
commit0-cli status                    # server state: idle or indexing (N jobs)
commit0-cli repo list                 # list indexed repos
commit0-cli index .                   # incremental index (skips unchanged files)
commit0-cli index . --reparse         # re-parse ALL files with current resolver (no delete)
```

### Fix Mode — Standard dev tools allowed

Read, Write, Edit, Bash (`go build`, `go test`, `go vet`) are allowed.
But ONLY after writing the solution in SOLUTION.md.

---

## Architecture Awareness

commit0 has **6 analysis techniques** across **13 edge types**. When investigating
issues, consider ALL of them — not just call graphs:

| Technique | Edge Types | Use For |
|-----------|-----------|---------|
| Call graph | calls, imports, defines, inherits, uses | Who calls what, dependencies |
| Data flow | data_flow, reads, writes | How data moves, field mutations, taint |
| Control flow | control_flow | If/else branches, loops, defers |
| Data dependence | data_dep | Variable def-use chains |
| Route discovery | route | HTTP endpoints, handler chains |
| Temporal | introduced_commit, last_modified | When code changed, who changed it |

The resolver has **4 strategies** for call edge resolution:
1. Exact match (qualified name)
2. Same-package prefix (bare function → pkg.Function)
3. Suffix match (s.Method → .Method)
4. Interface dispatch (ambiguous → single non-test production impl)

**Database**: Dual connection pool — 8 read + 4 write connections.
Queries work during indexing. Configurable via `SURREAL_READ_POOL`, `SURREAL_WRITE_POOL`.

---

## The Loop

### Step 1: DISCOVER (commit0-cli only)

Run tools. Try to answer real questions about the codebase. Record every friction
in `FINDINGS.md`. Add new entries to `BACKLOG.md` with status `open`.

### Step 2: PRIORITIZE

Read `BACKLOG.md`. Pick the highest-severity `open` item:
- `blocking` — tool doesn't work at all
- `major` — tool works but gives useless results
- `minor` — tool works but UX is rough

### Step 3: WRITE SOLUTION (in SOLUTION.md)

Before touching any code, write the full engineering solution:
- Problem (what the user sees)
- Root cause (why it happens)
- Step-by-step implementation plan
- Exact files to change
- Verification command (the commit0-cli command to re-run)
- Acceptance criteria

### Step 4: IMPLEMENT (following SOLUTION.md)

Fix the code exactly as specified in the solution. No extra changes.
- `go vet` after every edit
- `go test` after every edit
- `go build` to verify binaries

### Step 5: VERIFY (commit0-cli only)

Re-run the exact commit0-cli command from the solution's verification section.
Did the fix work?

- **Yes** → Update `BACKLOG.md` status to `solved`. Move solution to "Completed"
  section in `SOLUTION.md`. Update `FINDINGS.md` to mark item `[x]`.
- **No** → Revise solution, try again.

### Step 6: RE-INDEX

```bash
commit0-cli index .              # incremental (fast, skips unchanged)
commit0-cli index . --reparse    # full re-parse (after resolver changes)
```

### Step 7: REPEAT from Step 1

Stop when: no `blocking` or `major` items remain in BACKLOG.md, or user says stop.

---

## Quick Reference

```bash
# Discovery — search
commit0-cli query "question" --repo <slug> --no-agent --no-explain
commit0-cli query "question" --repo <slug> --no-agent --no-explain --kind function
commit0-cli query "question" --repo <slug> --no-agent --no-explain --file server/internal/app/

# Discovery — read code
commit0-cli show <symbol> --repo <slug>
commit0-cli ls <file-path> --repo <slug>

# Discovery — graph analysis
commit0-cli trace <symbol> --repo <slug> --direction forward|reverse
commit0-cli blast <symbol> --repo <slug>
commit0-cli analyze --repo <slug> --focus all|architecture|dead-code|consistency|hotspots|data-flow|temporal

# Discovery — server state
commit0-cli status
commit0-cli repo list

# Indexing
commit0-cli index .
commit0-cli index . --reparse

# Fix verification
go vet ./server/... ./cli/... ./sdk/...
go test ./server/internal/app/... ./server/internal/adapters/...
go build -o bin/commit0 ./server
go build -o bin/commit0-cli ./cli
```
