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

```bash
commit0-cli repo list
commit0-cli query "..." --repo <slug> --no-agent
commit0-cli query "..." --repo <slug>
commit0-cli trace <symbol> --repo <slug> --direction forward|reverse
commit0-cli blast <symbol> --repo <slug>
commit0-cli analyze --repo <slug> --focus all|architecture|dead-code|consistency|hotspots|data-flow|temporal
commit0-cli index .
```

No Grep. No Glob. No Agent subagents. If commit0 can't answer your question, that's a finding.

### Fix Mode — Standard dev tools allowed

Read, Write, Edit, Bash (`go build`, `go test`, `go vet`) are allowed.
But ONLY after writing the solution in SOLUTION.md.

---

## The Loop

### Step 1: DISCOVER (commit0-cli only)

Run tools. Try to answer real questions about the codebase. Record every friction point in `FINDINGS.md`. Add new entries to `BACKLOG.md` with status `open`.

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

- **Yes** → Update `BACKLOG.md` status to `solved`. Move solution to "Completed" section in `SOLUTION.md`. Update `FINDINGS.md` to mark item `[x]`.
- **No** → Revise solution, try again.

### Step 6: RE-INDEX

```bash
commit0-cli index .
```

### Step 7: REPEAT from Step 1

Stop when: no `blocking` or `major` items remain in BACKLOG.md, or user says stop.

---

## Quick Reference

```bash
# Discovery
commit0-cli repo list
commit0-cli query "question" --repo <slug> --no-agent
commit0-cli trace <Symbol> --repo <slug> --direction forward|reverse
commit0-cli blast <Symbol> --repo <slug>
commit0-cli analyze --repo <slug> --focus <area>
commit0-cli index .

# Fix verification
go vet ./server/... ./cli/... ./sdk/...
go test ./server/internal/app/... ./server/internal/adapters/...
go build -o bin/commit0 ./server
go build -o bin/commit0-cli ./cli
```
