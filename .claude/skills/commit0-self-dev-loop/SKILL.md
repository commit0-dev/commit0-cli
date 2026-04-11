---
name: commit0-self-dev-loop
description: >
  Autonomous self-development loop. Uses ONLY commit0-cli commands to explore
  the codebase. When a tool fails, is slow, gives wrong results, or is missing —
  THAT is the finding. Writes all findings to .claude/self-dev-findings.md.
  DO NOT use Grep, Glob, Read, Agent, or any non-commit0 tool. This forces you
  to experience the product as a real user. TRIGGER when: user says self-dev,
  self-improve, use commit0 on itself, or run the loop.
---

# commit0 Self-Development Loop

## The Rule

**You may ONLY use commit0-cli commands.** No Grep. No Glob. No Read. No Agent subagents. Only:

```bash
commit0-cli query "..." --repo <slug> --no-agent
commit0-cli query "..." --repo <slug>          # agent mode
commit0-cli trace <symbol> --repo <slug> --direction forward|reverse
commit0-cli blast <symbol> --repo <slug>
commit0-cli analyze --repo <slug> --focus <area>
commit0-cli index .
commit0-cli repo list
```

If you need information and commit0 can't provide it — **that's a finding**. Write it down.

## The Loop

### Step 1: Use the tools

Run commit0-cli commands to understand the codebase. Try to answer real questions:
- "What does function X do?" → `query`
- "Who calls function X?" → `trace --direction reverse`
- "What breaks if I change X?" → `blast`
- "What are the problems in this codebase?" → `analyze`

### Step 2: Record every friction point

For EVERY command you run, note:
- Did it return useful results? If not, why?
- Was it fast enough? (>5s for a simple query = too slow)
- Did it find the right code? Or irrelevant results?
- Was there a tool you needed but doesn't exist?
- Did it crash, timeout, or return an error?

Write ALL findings to `.claude/self-dev-findings.md` in this format:

```markdown
## Findings — [date]

### Tool Gaps (commit0 is missing this capability)
- [ ] Description of what's needed and why

### Wrong Results (tool returned incorrect/irrelevant output)
- [ ] Command run, what it returned, what was expected

### Performance Issues (too slow, timeout, crash)
- [ ] Command run, how long it took, what happened

### UX Issues (confusing output, bad defaults, missing flags)
- [ ] Description of the friction
```

### Step 3: Prioritize and plan

After collecting findings, rank them:
1. **Blocking** — tool doesn't work at all (crash, wrong results)
2. **Major** — tool works but gives useless results
3. **Minor** — tool works but UX is rough

For each blocking/major finding, write a concrete improvement plan in the findings file.

### Step 4: Implement fixes

Fix the highest-priority tool issues. Then re-run the same commands to verify the fix worked.

### Step 5: Repeat

After fixing, go back to Step 1. Use the improved tools. Find new issues. Fix them. This is the loop.

## What NOT to do

- Do NOT use Grep to "cheat" around commit0's limitations
- Do NOT use Read to look at files directly
- Do NOT use Glob to find files
- Do NOT use Agent subagents for exploration
- If you catch yourself reaching for a non-commit0 tool, STOP and ask: "Why can't commit0 do this?" — then write that as a finding

## The goal

The goal is NOT to find bugs in the code. The goal is to find gaps in the TOOLS. Every friction point is a product improvement opportunity. The findings file becomes the roadmap.
