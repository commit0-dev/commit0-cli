# commit0 — Technical Design: Find Commit Zero

> Architecture for data flow tracing, temporal code graph, causal reasoning, and compressed memory.

---

## 1. Domain Layer: New Ports and Types

### 1.1 New Types (`pkg/types/`)

**`ast.go` additions:**

```go
type MutationKind string

const (
    MutationNone        MutationKind = "none"
    MutationTransform   MutationKind = "transform"    // string ops, math
    MutationTypeConvert MutationKind = "type_convert"  // cast, conversion
    MutationFieldSet    MutationKind = "field_set"     // struct field assignment
    MutationFieldDelete MutationKind = "field_delete"  // map delete, nil
    MutationFilter      MutationKind = "filter"        // conditional inclusion
)

// Temporal fields on CodeNode:
//   IntroducedCommit, IntroducedAt, LastModifiedCommit, LastModifiedAt

// Temporal fields on CodeEdge:
//   IntroducedCommit, IntroducedAt, RemovedCommit
```

**`result.go` additions:**

| Type | Purpose |
|------|---------|
| `FieldFlowHop` | Single step in a field-level data flow trace with mutation metadata |
| `FieldFlowChain` | End-to-end path of a field through functions, with taint point |
| `FieldFlowResult` | Complete field flow query result |
| `TemporalChange` | What changed in the code graph at a commit |
| `SuspectCommit` | Candidate commit with score and reasoning |
| `RootCauseReport` | Final output: commit zero + causal chain + explanation + fix |
| `MemoryEntry` | Stored memory with tier, content, concepts, embedding |

### 1.2 New Port Interfaces (`internal/domain/ports.go`)

| Interface | Methods | Implementor |
|-----------|---------|-------------|
| **TemporalStore** | `UpsertNodeTemporal`, `UpsertEdgeTemporal`, `MarkNodeRemoved`, `MarkEdgeRemoved`, `QueryTemporalRange`, `NodeHistory`, `EdgesIntroducedAt` | SurrealAdapter |
| **FieldFlowStore** | `TraceFieldFlow`, `FindMutations` | SurrealAdapter |
| **MemoryStore** | `StoreMemory`, `RetrieveMemories`, `ListSessionMemories`, `DeleteSessionMemories` | SurrealAdapter (MemoryAdapter wrapper) |
| **GitWalker** | `ListCommits`, `DiffCommit`, `ReadFileAtCommit`, `CommitInfo` | GitWalkerAdapter (os/exec) |
| **Compressor** | `CompressTurn`, `CompressSession` | GeminiCompressor / local Gemma 4 |

**Design principle**: Separate interfaces per concern (Interface Segregation). `SurrealAdapter` implements `GraphStore` + `TemporalStore` + `FieldFlowStore`. New consumers depend only on the interface they need.

---

## 2. Service Layer: New Services

```
RootCauseService ─── the headline feature
  ├── QueryService (existing) ─── LOCATE: find bug-related functions
  ├── FieldFlowService (new) ─── TRACE: follow field-level data flow
  │     ├── FieldFlowStore
  │     └── GraphStore
  ├── TemporalService (new) ─── TIMELINE: when did relationships change
  │     ├── GitWalker
  │     ├── Parser (existing)
  │     ├── TemporalStore
  │     └── GraphStore
  ├── MemoryManager (new) ─── compressed context across turns
  │     ├── MemoryStore
  │     ├── Embedder (existing)
  │     └── Compressor
  └── LLMExplainer (existing) ─── VERIFY + REPORT
```

### 2.1 FieldFlowService (`internal/app/field_flow_service.go`)

**Purpose**: Field-level data flow tracing with mutation detection.

```go
func (s *FieldFlowService) TraceFieldFlow(ctx, FieldFlowRequest) (*FieldFlowResult, error)
```

Traces a specific field (e.g., `user.Email`) through the code graph, following `data_flow` edges that carry `field_path`, `mutation_type`, `mutation_expr` metadata. Returns chains with taint points marked.

### 2.2 TemporalService (`internal/app/temporal_service.go`)

**Purpose**: Diff-aware indexing and temporal queries.

```go
func (s *TemporalService) IndexCommitRange(ctx, TemporalIndexRequest) error
func (s *TemporalService) QueryHistory(ctx, TemporalQueryRequest) ([]TemporalChange, error)
```

`IndexCommitRange` walks git history commit-by-commit:
1. For each commit: `git diff-tree` → changed files
2. Parse changed files with tree-sitter
3. Diff parsed nodes/edges against current graph
4. Mark `introduced_commit` on new nodes, `last_modified_commit` on changed nodes
5. Store commit metadata in `commit_history` table

### 2.3 RootCauseService (`internal/app/rootcause_service.go`)

**Purpose**: Automated commit zero detection. The 6-step algorithm:

```
1. LOCATE   → QueryService.Query() finds bug-related functions
2. TRACE    → FieldFlowService.TraceFieldFlow() follows data backward, finds mutations
3. TIMELINE → TemporalService.QueryHistory() finds when mutations were introduced
4. CORRELATE → Score suspects: temporal_proximity × data_flow_position × change_magnitude
5. VERIFY   → LLMExplainer analyzes suspect commit's diff against causal chain
6. REPORT   → Assemble RootCauseReport with commit zero + explanation + fix
```

### 2.4 MemoryManager (`internal/app/memory/manager.go`)

**Purpose**: Three-tier context management for long investigations.

```
┌─ WORKING (8K tokens) ─── current turn + recent tool results ──────────┐
├─ SESSION (4K tokens) ─── compressed history of this investigation ────┤
├─ PERSISTENT (2K tokens) ── cross-session knowledge (vector retrieved) ┤
└─ TOTAL BUDGET: ~14K tokens allocated, 114K remaining for response ────┘
```

```go
func (m *MemoryManager) BuildContext(ctx, sessionID, repoSlug, currentTurn) (string, error)
func (m *MemoryManager) AfterTurn(ctx, sessionID, role, content, toolCalls) error
```

After each turn: compress older entries. When tier exceeds budget: ultra-compress oldest entries. Context compression uses LLM (Gemma 4 locally, Gemini in cloud).

---

## 3. Adapter Layer: New Adapters

### 3.1 Git Walker (`internal/adapters/git/walker.go`)

Implements `domain.GitWalker` via `os/exec` git commands:

| Method | Git Command |
|--------|-------------|
| `ListCommits` | `git log --format="%H\|%an\|%at\|%s" from..to` |
| `DiffCommit` | `git diff-tree --no-commit-id -r -p hash` |
| `ReadFileAtCommit` | `git show hash:path` |
| `CommitInfo` | `git log -1 --format=...` |

### 3.2 Compressor (`internal/adapters/gemini/compressor.go`)

Implements `domain.Compressor` using the existing `genai.Client`:

```go
func (c *GeminiCompressor) CompressTurn(ctx, role, content, toolCalls) (string, error)
  // Prompt: "Summarize this investigation turn in 2 sentences,
  //          preserving file names, line numbers, and key findings."

func (c *GeminiCompressor) CompressSession(ctx, turns) (string, error)
  // Prompt: "Create a 3-sentence summary of this investigation,
  //          preserving the causal chain and key evidence."
```

### 3.3 Memory Store (`internal/adapters/surreal/memory_store.go`)

Thin wrapper on `SurrealAdapter` (same pattern as `VectorAdapter`/`TextAdapter`):

```go
type MemoryAdapter struct{ *SurrealAdapter }
func (a *SurrealAdapter) AsMemoryStore() domain.MemoryStore { return &MemoryAdapter{a} }
```

Uses HNSW vector index on `memory.embedding` for semantic retrieval of persistent memories.

### 3.4 Enhanced Tree-Sitter Extractors

Modify `lang/golang.go`, `typescript.go`, `python.go`, `javascript.go` to capture:
- **Field-level data flow**: `user.Email` flows from function A param to function B
- **Mutation detection**: assignment with method call (e.g., `x = strings.ToLower(x)`)
- **New metadata on data_flow edges**: `field_path`, `mutation_type`, `mutation_expr`, `mutation_line`

---

## 4. SurrealDB Schema Additions

Temporal fields on nodes (`introduced_commit`, `last_modified_commit`, timestamps) and edges. Enhanced `data_flow` edges with `field_path`, `mutation_type`, `mutation_expr`, `mutation_line`. Memory table with HNSW vector index. Commit history cache table. See [DATABASE.md](DATABASE.md) for complete schema.

---

## 5. Agent Tools (ADK)

Three new tools added to `internal/app/agent/tools.go`:

| Tool | Wraps | Purpose |
|------|-------|---------|
| `flow_trace` | FieldFlowService | Trace field-level data flow with mutations |
| `temporal_query` | TemporalService | Query when code elements changed |
| `analyze_commit_diff` | GitWalker + LLMExplainer | Analyze a commit's diff for causality |

Updated agent system prompt includes root cause investigation workflow.

---

## 6. CLI Commands

| Command | Service | Description |
|---------|---------|-------------|
| `commit0 find-root "description"` | RootCauseService | Automated commit zero detection |
| `commit0 flow "variable" --direction forward` | FieldFlowService | Field-level data flow tracing |
| `commit0 history "function"` | TemporalService | Temporal history of a code element |
| `commit0 investigate "description"` | AgentService + MemoryManager | Interactive investigation with memory |

---

## 7. Implementation Phases

| Phase | What | Files | Dependencies |
|-------|------|-------|-------------|
| **P0-1** | Types + ports + schema | `pkg/types/`, `domain/ports.go`, `schema.surql` | None |
| **P0-2** | Enhanced tree-sitter extraction | `lang/golang.go`, `typescript.go`, etc. | P0-1 |
| **P0-3** | Git walker adapter | `adapters/git/walker.go` | P0-1 |
| **P1-1** | FieldFlowService | `app/field_flow_service.go` | P0-1, P0-2 |
| **P1-2** | TemporalService | `app/temporal_service.go` | P0-1, P0-3 |
| **P1-3** | MemoryManager | `app/memory/manager.go`, `adapters/gemini/compressor.go` | P0-1 |
| **P1-4** | RootCauseService | `app/rootcause_service.go` | P1-1, P1-2 |
| **P2-1** | Agent tools + CLI commands | `agent/tools.go`, `cmd/findroot.go`, etc. | P1-* |
| **P2-2** | HTTP endpoints | `adapters/http/handlers.go` | P2-1 |

**Total new files**: 18
**Total modified files**: 18
**Estimated implementation**: ~4 phases over focused sessions

---

## 8. File Map

### New Files (18)
```
internal/adapters/git/walker.go              # GitWalker adapter
internal/adapters/git/walker_test.go
internal/adapters/gemini/compressor.go       # Context compressor
internal/adapters/gemini/compressor_test.go
internal/adapters/surreal/memory_store.go    # Memory persistence
internal/adapters/surreal/memory_store_test.go
internal/app/field_flow_service.go           # Field-level data flow
internal/app/field_flow_service_test.go
internal/app/temporal_service.go             # Git-aware temporal graph
internal/app/temporal_service_test.go
internal/app/rootcause_service.go            # Commit zero detection
internal/app/rootcause_service_test.go
internal/app/memory/manager.go               # Three-tier memory
internal/app/memory/manager_test.go
cmd/findroot.go                              # commit0 find-root
cmd/flow.go                                  # commit0 flow
cmd/history.go                               # commit0 history
cmd/investigate.go                           # commit0 investigate
```

### Modified Files (18)
```
pkg/types/ast.go                             # MutationKind, temporal fields
pkg/types/result.go                          # FieldFlowHop, RootCauseReport, etc.
internal/domain/ports.go                     # 5 new interfaces
assets/schema.surql                          # temporal + memory + commit_history
internal/adapters/surreal/schema.go          # version bump
internal/adapters/surreal/graph_store.go     # temporal + field flow methods
internal/adapters/surreal/client.go          # AsMemoryStore()
internal/adapters/treesitter/lang/golang.go  # field-level mutation detection
internal/adapters/treesitter/lang/python.go
internal/adapters/treesitter/lang/typescript.go
internal/adapters/treesitter/lang/javascript.go
internal/app/stubs_test.go                   # new interface stubs
internal/app/agent/tools.go                  # 3 new tools
internal/app/agent/service.go                # memory integration + new tools
cmd/wire.go                                  # wire new services
internal/adapters/http/handlers.go           # new endpoints
internal/adapters/http/server.go             # register routes
internal/adapters/http/handlers_test.go      # new stubs
```
