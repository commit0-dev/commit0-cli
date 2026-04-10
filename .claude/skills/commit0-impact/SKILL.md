---
name: commit0-impact
description: Check impact before changing code. TRIGGER PROACTIVELY when about to modify or delete a function, class, or interface. Run blast analysis to see all transitively affected code BEFORE making the edit. This is the most important commit0 skill — it prevents regressions.
---

# commit0 Impact Analysis (Blast Radius)

## BEFORE modifying any function, run this:

```bash
commit0-cli blast <qualified.Name> --repo <slug>
```

Example — before modifying `ImportBundle`:

```bash
commit0-cli blast ImportBundle --repo commit0-dev/commit0
```

## Output

Shows:
- **Target**: the function you're about to change
- **Affected nodes**: all functions that transitively depend on this one
- **Hop count**: how many call levels away each affected function is
- **Summary**: LLM explanation of the blast radius

## How to use the results

1. **Small blast radius** (< 5 affected): safe to change, verify callers
2. **Medium blast radius** (5-20 affected): change carefully, check each caller
3. **Large blast radius** (> 20 affected): this is a critical function — consider adding a new function instead of modifying, or deprecate + migrate

## When to use

- Before changing function signatures (adding/removing params)
- Before deleting functions
- Before modifying return types or error conditions
- Before refactoring port interfaces (affects all adapters)
