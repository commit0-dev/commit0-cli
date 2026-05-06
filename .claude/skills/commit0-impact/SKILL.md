---
name: commit0-impact
description: Check impact before changing code. TRIGGER PROACTIVELY when about to modify or delete a function, class, or interface. Run blast analysis to see all transitively affected code BEFORE making the edit. Partial names work (ImportBundle resolves automatically). This is the most important commit0 skill — it prevents regressions.
---

# commit0 Impact Analysis (Blast Radius)

## BEFORE modifying any function, run this:
```bash
commit0-cli blast <symbol> --repo <slug>
```

**Partial names work** — no need for fully qualified names:
```bash
commit0-cli blast ImportBundle --repo commit0-dev/commit0    # resolves to app.SyncService.ImportBundle
commit0-cli blast GetNode --repo commit0-dev/commit0         # resolves to surreal.SurrealAdapter.GetNode
```

## How to use the results

1. **Small blast radius** (< 5 affected): safe to change, verify callers
2. **Medium blast radius** (5-20 affected): change carefully, check each caller
3. **Large blast radius** (> 20 affected): critical function — consider adding new function or deprecate + migrate

## Resolution strategies

The resolver tries 4 strategies to find the symbol:
1. Exact match (fully qualified)
2. Same-package prefix
3. Suffix match (.MethodName)
4. Interface dispatch (prefers non-test production implementation)

## When to use

- Before changing function signatures
- Before deleting functions
- Before modifying return types or error conditions
- Before refactoring port interfaces (affects all adapters)

## Works during indexing

The dual connection pool ensures blast/trace respond even while indexing is active.
