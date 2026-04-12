# OpenCodeGraph

Unified graph abstraction for code intelligence. Defines the data model, port interface, analysis techniques, and edge resolution pipeline.

---

## 1. Data Model

### Nodes

```go
type GraphNode struct {
    ID        string         `json:"id"`
    Label     string         `json:"label"`      // "function", "class", "file", "module"
    Qualified string         `json:"qualified"`   // fully qualified name
    Name      string         `json:"name"`
    FilePath  string         `json:"file_path"`
    RepoSlug  string         `json:"repo_slug"`
    Props     map[string]any `json:"props"`       // label-specific properties
    Embedding []float32      `json:"embedding,omitempty"`
}
```

`Label` is a string, not an enum. `Props` is an open property map. Label-specific properties (e.g., `signature`, `body`, `start_line` for functions) are documented by convention and accessed via typed helper functions.

Current node labels: `function`, `class`, `file`, `module`.

### Edges

```go
type GraphEdge struct {
    Label  string         `json:"label"`    // "calls", "data_flow", "defines", ...
    FromID string         `json:"from_id"`
    ToID   string         `json:"to_id"`
    Props  map[string]any `json:"props"`    // label-specific properties
}
```

Current edge labels: `calls`, `data_flow`, `reads`, `writes`, `defines`, `imports`, `inherits`, `uses`, `route`, `control_flow`, `data_dep`.

---

## 2. Port Interface

```go
type OpenCodeGraph interface {
    // Node CRUD
    PutNode(ctx, node)          GetNode(ctx, id)
    FindNode(ctx, repo, qual)   DeleteNode(ctx, id)

    // Edge CRUD
    PutEdge(ctx, edge)          DeleteEdgesFrom(ctx, nodeID)

    // Batch
    PutBatch(ctx, nodes, edges)
    DeleteByRepo(ctx, repo)     DeleteByFile(ctx, repo, path)

    // Traversal
    TraverseGraph(ctx, startID, edgeLabels, direction, maxDepth)
    Neighbors(ctx, nodeID)

    // Search
    VectorSearch(ctx, vec, opts)
    TextSearch(ctx, query, opts)

    // Listing
    ListNodes(ctx, repo, opts)
    ListEdges(ctx, repo, labels)
    ListFilePaths(ctx, repo)

    // Repo management
    PutRepo  GetRepo  ListRepos  DeleteRepo
    FindRepoByRemoteURL  UpdateRepoIndexedAt

    // Schema
    ApplySchema(ctx)
}
```

All application services depend on this interface. Traversal is parameterized by edge labels: the caller specifies which relationship types to follow. The same `TraverseGraph` method is used for call tracing, impact analysis, data flow, and other techniques.

### Technique-to-Traversal Mapping

| Analysis | Edge labels | Direction |
|----------|------------|-----------|
| Call trace | `["calls"]` | forward |
| Impact (blast radius) | `["calls", "data_flow"]` | reverse |
| Data flow | `["data_flow", "reads", "writes"]` | forward |
| Taint analysis | `["data_flow", "route", "calls"]` | forward |
| API surface | `ListEdges(repo, ["route"])` | n/a |

---

## 3. Analysis Techniques

### Call Graph

Tree-sitter extractors produce `calls` edges with `call_site`, `call_type`, and `is_dynamic` properties. The `CallLinker` resolves unresolved callee names to node IDs using four strategies: exact qualified match, same-package match, suffix match, and interface dispatch.

### Data Flow

Extractors produce `data_flow` edges with `param_name`, `arg_expr`, `field_path`, and mutation metadata. The `DataFlowLinker` resolves targets using the same strategies as the call linker.

### Control Flow

Extractors produce `control_flow` edges with `branch_type`, `condition`, and line ranges. These are intra-function edges and do not require cross-file resolution.

### Data Dependence

Extractors produce `data_dep` edges with variable name, definition line, and use line. These are intra-function and do not require cross-file resolution.

### Route Discovery

Extractors identify HTTP handler registrations and produce `route` edges with `http_method`, `http_path`, and `middleware` properties. The `RouteLinker` resolves handler function references.


---

## 4. Edge Resolution Pipeline

During indexing, edges are extracted per-file with unresolved target IDs (raw text from the AST). A global resolution step then maps these to actual node IDs.

```
Phase 1: EXTRACT (per-file, parallel)
  Parse each file with tree-sitter.
  Accumulate all nodes and edges. Edge ToIDs are unresolved.

Phase 2: LINK (global, sequential)
  Build a SymbolTable from all parsed nodes.
  Run each EdgeLinker against the complete edge set:
    CallLinker, DataFlowLinker, DefinesLinker,
    FieldAccessLinker, RouteLinker
  Collect resolution statistics per linker.

Phase 3: PROCESS (per-batch, parallel)
  Summarize nodes (optional, via LLM).
  Compute embeddings.
  Store nodes and resolved edges in SurrealDB.
```

### SymbolTable

Built once from all parsed nodes. Maps qualified names, file paths, and name suffixes to node IDs. Resolution strategies:

1. Exact qualified name match
2. Same-package match (when the caller and callee share a package prefix)
3. Suffix match (for short names like `.UpsertNode`)
4. Interface dispatch (match method name against all types implementing it)

### EdgeLinker Interface

```go
type EdgeLinker interface {
    Name() string
    Labels() []string
    Link(edges []types.CodeEdge, symbols *SymbolTable) ([]types.CodeEdge, LinkStats)
}
```

To add a new analysis technique, implement `EdgeLinker` and register it with `IndexService.SetLinkers()`. No changes are required to the port interface, adapter, or existing services.

---

## 5. Extensibility

New node and edge types can be introduced without schema changes. Edge tables are SCHEMALESS in SurrealDB; the first `RELATE` statement for a new label creates the table automatically.

**Adding a language.** Implement a tree-sitter extractor in `internal/adapters/treesitter/lang/`. The extractor produces the same node and edge types as existing languages.

**Adding an analysis dimension.** Implement an `EdgeLinker` for the new edge label. Register it in the index service. The existing traversal, search, and listing operations work with any label.

---

## 6. SurrealDB Implementation

Node storage uses dedicated SCHEMAFULL tables for the four current node types (required for HNSW indexes and COMPUTED fields). New node types would use auto-created SCHEMALESS tables.

Edge storage uses a single generic `RELATE` query for all edge types:

```go
q := fmt.Sprintf("RELATE $from->%s->$to CONTENT $props;", edge.Label)
```

Multi-label traversal runs parallel per-label queries and merges results, because SurrealDB does not support multi-table recursive traversal in a single expression.
