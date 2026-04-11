---
name: commit0-self-dev-loop
description: >
  Autonomous self-development loop. Uses ONLY commit0-cli commands to DISCOVER
  issues (search, trace, blast, analyze). When a tool fails, is slow, gives wrong
  results, or is missing — THAT is the finding. File Read/Write/Edit ARE allowed
  for IMPLEMENTING fixes. Writes findings to .claude/self-dev-findings.md.
  TRIGGER when: user says self-dev, self-improve, use commit0 on itself, run the loop.
---

# commit0 Self-Development Loop

## Two Modes

**Discovery mode** — ONLY commit0-cli. No Grep, Glob, Agent subagents.
**Fix mode** — Read, Write, Edit, Bash (go build/test) ARE allowed to implement fixes.

The rule: you must DISCOVER problems using commit0's own tools. But you may use
standard dev tools to FIX those problems.

---

## Discovery Mode — commit0-cli ONLY

```bash
commit0-cli repo list
commit0-cli query "..." --repo <slug> --no-agent
commit0-cli query "..." --repo <slug>          # agent mode
commit0-cli trace <symbol> --repo <slug> --direction forward|reverse
commit0-cli blast <symbol> --repo <slug>
commit0-cli analyze --repo <slug> --focus <area>
commit0-cli index .
```

If you need information and commit0 can't provide it — **that's a finding**.

### What to try

Run these commands and record every friction:
- "What does function X do?" → `query`
- "Who calls function X?" → `trace --direction reverse`
- "What breaks if I change X?" → `blast`
- "What are the problems?" → `analyze`
- "Show me this function's code" → ??? (missing tool = finding)
- "List functions in this file" → ??? (missing tool = finding)

### What to record

For EVERY command, note in `.claude/self-dev-findings.md`:
- Did it return useful results? If not, why?
- Was it fast enough? (>5s = too slow for simple queries)
- Did it find the right code? Or irrelevant results?
- Was there a tool you needed but doesn't exist?
- Did it crash, timeout, or return an error?

Format:

```markdown
### Tool Gaps (commit0 is missing this capability)
- [ ] Description — why it's needed

### Wrong Results (tool returned incorrect/irrelevant output)
- [ ] Command → what returned → what expected

### Performance Issues (too slow, timeout, crash)
- [ ] Command → how long → what happened

### UX Issues (confusing output, bad defaults, missing flags)
- [ ] Description of friction
```

---

## Fix Mode — Standard Dev Tools Allowed

After discovering issues, switch to fix mode:

1. **Read** the relevant source files
2. **Edit/Write** the fix
3. **Bash**: `go vet`, `go test`, `go build` to verify
4. **Re-run the commit0-cli command** that found the issue to verify the fix

### Rules for fixing

- Follow CLAUDE.md rules (hexagonal arch, domain errors, etc.)
- Read the file before editing
- Run `go vet` and `go test` after every change
- Re-run the original commit0-cli command to prove the fix works
- Update `.claude/self-dev-findings.md` — mark fixed items with [x]

---

## The Loop

```
Step 1: DISCOVER (commit0-cli only)
  → Run tools, record friction in findings file

Step 2: PRIORITIZE
  → Blocking > Major > Minor
  → Pick top 1-3 findings to fix

Step 3: FIX (standard dev tools allowed)
  → Read source, implement fix, test, verify

Step 4: VERIFY (commit0-cli only)
  → Re-run the original command that exposed the issue
  → Did the fix work? Update findings file

Step 5: RE-INDEX
  → commit0-cli index .
  → Graph now reflects the fixes

Step 6: REPEAT from Step 1
  → Stop when no blocking/major findings remain
```

---

## Quick Reference

```bash
# Discovery
commit0-cli repo list
commit0-cli query "question" --repo <slug> --no-agent
commit0-cli trace <Symbol> --repo <slug> --direction forward|reverse
commit0-cli blast <Symbol> --repo <slug>
commit0-cli analyze --repo <slug> --focus all|architecture|dead-code|consistency|hotspots|data-flow|temporal
commit0-cli index .

# Fix verification
go vet ./server/... ./cli/... ./sdk/...
go test ./server/internal/app/... ./server/internal/adapters/...
go build -o bin/commit0 ./server
go build -o bin/commit0-cli ./cli
```
