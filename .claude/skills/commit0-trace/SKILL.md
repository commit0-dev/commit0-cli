---
name: commit0-trace
description: Trace call chains through the codebase. TRIGGER when understanding data flow, debugging how a value reaches a destination, following a request from handler to database, or understanding who calls a function. Partial names work. 4 resolver strategies including interface dispatch. Use INSTEAD of manually reading file-by-file.
---

# commit0 Call Graph Tracing

## Forward trace — "what does this function call?"
```bash
commit0-cli trace <symbol> --repo <slug> --direction forward --depth 5
```

## Reverse trace — "who calls this function?"
```bash
commit0-cli trace <symbol> --repo <slug> --direction reverse --depth 3
```

## Partial names work
```bash
commit0-cli trace Pull --repo <slug> --direction forward       # → app.SyncService.Pull
commit0-cli trace ImportBundle --repo <slug> --direction reverse # → finds all callers
```

## Read the code of traced functions
```bash
commit0-cli show app.SyncService.Pull --repo <slug>     # print full function body
commit0-cli ls server/internal/app/sync_service.go --repo <slug>  # list all functions in file
```

## Resolution (4 strategies)
1. Exact match (fully qualified name)
2. Same-package prefix (bare function → pkg.Function)
3. Suffix match (s.Method → .Method)
4. Interface dispatch (ambiguous → single non-test production implementation)

## When to use

| Scenario | Direction | Why |
|---|---|---|
| "How does a request flow?" | forward | Handler → service → adapter → DB |
| "Who calls this?" | reverse | Find all entry points to a function |
| "How does data reach X?" | forward | Trace data flow from source |
| "What depends on X?" | reverse | Similar to blast but shows the tree |

## Works during indexing
Dual connection pool ensures trace responds even while index writes are active.
