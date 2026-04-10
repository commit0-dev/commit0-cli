---
name: commit0-trace
description: Trace call chains through the codebase. TRIGGER when understanding data flow, debugging how a value reaches a destination, following a request from handler to database, or understanding who calls a function. Use INSTEAD of manually reading file-by-file.
---

# commit0 Call Graph Tracing

## Forward trace — "what does this function call?"

```bash
commit0-cli trace <symbol> --repo <slug> --direction forward --depth 5
```

Example — trace from an HTTP handler to the database:

```bash
commit0-cli trace handleSyncExport --repo commit0-dev/commit0 --direction forward
```

## Reverse trace — "who calls this function?"

```bash
commit0-cli trace <symbol> --repo <slug> --direction reverse --depth 3
```

Example — find all callers of `UpsertNode`:

```bash
commit0-cli trace UpsertNode --repo commit0-dev/commit0 --direction reverse
```

## Output

Tree of call hops:

```
-> BuildBundle (app/sync_service.go:45)
  -> ExportBundle (adapters/surreal/graph_exporter.go:17)
    -> ListAllNodes (adapters/surreal/graph_store.go:933)
    -> ListAllEdges (adapters/surreal/graph_store.go:957)
```

## When to use

| Scenario | Direction | Why |
|---|---|---|
| "How does a request flow?" | forward | Handler -> service -> adapter -> DB |
| "Who calls this?" | reverse | Find all entry points to a function |
| "How does data reach X?" | forward | Trace data flow from source |
| "What depends on X?" | reverse | Similar to blast but shows the tree |

## Depth guidelines

- `--depth 3`: quick overview (default for reverse)
- `--depth 5`: standard analysis (default for forward)
- `--depth 10`: deep exploration (for complex flows)
