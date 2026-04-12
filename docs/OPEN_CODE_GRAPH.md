# OpenCodeGraph

> The unified graph abstraction for code intelligence. Every node, every edge, every
> analysis technique — one graph model, one port interface, one traversal API.
>
> Open by design: new languages, new techniques, new data sources — zero schema changes.

---

## 1. Graph Model

### GraphNode

```go
type GraphNode struct {
    ID        string         `json:"id"`         // "function:app.IndexService.Index"
    Label     string         `json:"label"`      // "function", "class", "dependency", ...
    Qualified string         `json:"qualified"`  // "app.IndexService.Index"
    Name      string         `json:"name"`       // "Index"
    FilePath  string         `json:"file_path"`  // "server/internal/app/index_service.go"
    RepoSlug  string         `json:"repo_slug"`  // "commit0-dev/commit0"
    Props     map[string]any `json:"props"`      // label-specific properties
    Embedding []float32      `json:"embedding,omitempty"`
}
```

A `GraphNode` is any entity in the knowledge graph. Current labels: `function`, `class`, `file`, `module`. Future: `dependency`, `vulnerability`, `endpoint`, `field`, `test`.

`Label` is a string, not an enum. `Props` is a property bag, not a fixed struct. Label-specific properties are documented by convention and accessed via typed helpers.

### GraphEdge

```go
type GraphEdge struct {
    Label  string         `json:"label"`   // "calls", "depends_on", "has_vuln", ...
    FromID string         `json:"from_id"` // source node ID
    ToID   string         `json:"to_id"`   // target node ID
    Props  map[string]any `json:"props"`   // label-specific properties
}
```

A `GraphEdge` is any relationship. Current labels: `calls`, `data_flow`, `reads`, `writes`, `defines`, `imports`, `inherits`, `uses`, `route`, `control_flow`, `data_dep`. Future: `depends_on`, `has_vuln`, `tests`, `exposes`.

---

## 2. The OpenCodeGraph Port

```go
type OpenCodeGraph interface {
    // Node CRUD
    PutNode(ctx, node)     GetNode(ctx, id)
    FindNode(ctx, repo, qualified)     DeleteNode(ctx, id)

    // Edge CRUD
    PutEdge(ctx, edge)     DeleteEdgesFrom(ctx, nodeID)

    // Batch
    PutBatch(ctx, nodes, edges)
    DeleteByRepo(ctx, repo)     DeleteByFile(ctx, repo, filePath)

    // Traversal (label-parameterized)
    TraverseGraph(ctx, startID, edgeLabels, direction, maxDepth) → []TraceHop
    Neighbors(ctx, nodeID) → *Neighborhood

    // Search
    VectorSearch(ctx, vec, opts) → []ScoredNode
    TextSearch(ctx, query, opts) → []ScoredNode

    // Listing
    ListNodes(ctx, repo, opts) → []CodeNode
    ListEdges(ctx, repo, labels) → []CodeEdge
    ListFilePaths(ctx, repo) → []string

    // Repo
    PutRepo  GetRepo  ListRepos  DeleteRepo
    FindRepoByRemoteURL  UpdateRepoIndexedAt

    // Schema
    ApplySchema(ctx)
}
```

All graph operations go through this single interface. Traversal is label-parameterized: the caller specifies which edge labels to follow. The same `TraverseGraph` call powers trace, blast, flow, and security analysis.

### How Techniques Map to Traversal

| Technique | Edge Labels | Direction |
|-----------|------------|-----------|
| Call trace | `["calls"]` | forward |
| Blast radius | `["calls", "data_flow"]` | reverse |
| Data flow | `["data_flow", "reads", "writes"]` | forward |
| Taint analysis | `["data_flow", "route", "calls"]` | forward |
| API surface | `ListEdges(repo, ["route"])` | — |
| Neighborhood | `Neighbors(nodeID)` | both |

---

## 3. The Six Analysis Techniques

### Technique 1: Call Graph

```
Extractor:  GraphEdge{Label: "calls", Props: {call_site, call_type, is_dynamic}}
Linker:     CallLinker — 4-strategy resolution (exact → same-package → suffix → interface)
Traversal:  TraverseGraph(id, ["calls"], "forward", depth)
```

### Technique 2: Data Flow

```
Extractor:  GraphEdge{Label: "data_flow", Props: {param_name, arg_expr, field_path, mutation_*}}
Linker:     DataFlowLinker — same resolution as CallLinker
Traversal:  TraverseGraph(id, ["data_flow"], "forward", depth)
```

### Technique 3: Control Flow (CFG)

```
Extractor:  GraphEdge{Label: "control_flow", Props: {branch_type, condition, from_line, to_line}}
Linker:     None (intra-function)
Traversal:  TraverseGraph(id, ["control_flow"], "forward", depth)
```

### Technique 4: Data Dependence (Def-Use)

```
Extractor:  GraphEdge{Label: "data_dep", Props: {var_name, def_line, use_line}}
Linker:     None (intra-function)
Traversal:  TraverseGraph(id, ["data_dep"], "forward", depth)
```

### Technique 5: Route Discovery

```
Extractor:  GraphEdge{Label: "route", Props: {http_method, http_path, middleware}}
Linker:     RouteLinker — resolves handler function names
Query:      ListEdges(repo, ["route"])
```

### Technique 6: Temporal

```
Source:     GitWalker → diff per commit → re-extract → diff graph
Storage:    TemporalStore (separate port — temporal is orthogonal to graph shape)
Query:      NodeHistory(nodeID), QueryTemporalRange(repo, from, to)
```

---

## 4. Edge Resolution Pipeline

```
Phase 1: EXTRACT (per-file, parallel)
  Walk files → Parse (tree-sitter) → accumulate all GraphNode[] + GraphEdge[]
  Edge ToIDs are raw text at this stage (unresolved)

Phase 2: LINK (global, sequential)
  Build SymbolTable from ALL parsed nodes
  Run EdgeLinker chain:
    CallLinker         → resolves calls edges
    DataFlowLinker     → resolves data_flow edges
    DefinesLinker      → generates file→fn, class→method defines edges
    FieldAccessLinker  → resolves reads/writes with receiver inference
    RouteLinker        → resolves route handler targets
  Coverage stats collected per linker

Phase 3: PROCESS (per-batch, parallel)
  Summarize (LLM) → Embed (vector) → Store (SurrealDB)
  All nodes + ALL resolved edges
```

### SymbolTable

Built once from all parsed nodes. Provides O(1) resolution via four strategies:

1. **Exact match**: `QualifiedToID["app.IndexService.Index"]`
2. **Same-package**: match within the calling node's package
3. **Suffix match**: `SuffixToIDs[".UpsertNode"]` for ambiguous short names
4. **Interface dispatch**: match method name against all implementing types

### EdgeLinker Interface

```go
type EdgeLinker interface {
    Name() string
    Labels() []string
    Link(edges []types.CodeEdge, symbols *SymbolTable) ([]types.CodeEdge, LinkStats)
}
```

Adding a new analysis technique: implement `EdgeLinker`, register it in `IndexService.SetLinkers()`. Zero changes to the pipeline, port, adapter, or schema.

---

## 5. Extensibility

The architecture supports new dimensions without code changes outside the extractor/linker layer.

### New Languages

Write `lang/rust.go` implementing the Extractor interface. Produces the same `GraphNode{Label: "function"}` and `GraphEdge{Label: "calls"}` as existing languages. Zero changes to OpenCodeGraph, adapter, services, or CLI.

### Dependency Analysis

Parse `go.mod`/`package.json` → produce `GraphNode{Label: "dependency"}` + `GraphEdge{Label: "depends_on"}`. SCHEMALESS edge tables auto-create on first `RELATE`.

### Vulnerability Correlation

Query OSV API → produce `GraphNode{Label: "vulnerability"}` + `GraphEdge{Label: "has_vuln"}`. A single traversal through `[calls, data_flow, depends_on, has_vuln]` answers: "does user input reach a vulnerable library?"

### Test Coverage Mapping

Analyze test imports → produce `GraphEdge{Label: "tests"}`. Reverse traverse answers: "which tests cover this function?"

---

## 6. SurrealDB Adapter Strategy

### Node Storage

| Node Label | SurrealDB Table | Schema |
|------------|-----------------|--------|
| `function`, `class`, `file`, `module` | Dedicated table | SCHEMAFULL (HNSW + COMPUTED) |
| Any new label | Auto-created | SCHEMALESS |

### Edge Storage

All edges use a generic RELATE query. No per-type SQL:

```go
q := fmt.Sprintf("RELATE $from->%s->$to CONTENT $props;", edge.Label)
```

### Multi-Label Traversal

SurrealDB does not support `(->edge_a | ->edge_b)` in one recursive expression. The adapter runs parallel per-label traversals and merges results:

```go
g, gCtx := errgroup.WithContext(ctx)
for i, label := range labels {
    g.Go(func() error {
        hops, err := a.traverseSingle(gCtx, startID, label, q)
        results[i] = hops
        return err
    })
}
g.Wait()
return mergeAndDedup(results), nil
```

---

## 7. Composable Graph Queries

Single traversals answer simple questions. Complex questions require composing multiple graph operations.

| User Question | Graph Operations |
|---------------|-----------------|
| "Does user input reach a SQL query unsanitized?" | List routes → forward trace [calls, data_flow] → filter sinks → check sanitizer paths |
| "Which commit broke notifications?" | Semantic search → reverse data flow → temporal query → correlate commits |
| "If I change IndexService, which teams are affected?" | Reverse traverse [calls, data_flow] → collect file paths → group by CODEOWNERS team |

Three execution strategies:

1. **Service-layer composition** — Go functions chaining `TraverseGraph` + `Neighbors` calls. Deterministic, testable. Used by RootCauseAnalysisService, APISurfaceService.
2. **Declarative DSL** — JSON graph programs interpreted by a query engine. Scenarios as data.
3. **Agent-composed** — LLM agent with graph primitive tools. Handles any question dynamically.

All three layers use the same OpenCodeGraph primitives.
