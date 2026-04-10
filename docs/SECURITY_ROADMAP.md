# commit0 — Code Analysis Feature Roadmap

> Features that leverage commit0's code graph for analysis capabilities structurally impossible for pattern-matching tools like Semgrep.

**Last updated:** 2026-04-09

---

## Current State

### What the graph stores (13 edge types)

| Edge | Source | Status |
|------|--------|--------|
| `calls` | tree-sitter call extraction | Shipped |
| `imports` | tree-sitter import extraction | Shipped |
| `defines` | resolver pass | Shipped |
| `inherits` | tree-sitter class extraction | Shipped |
| `uses` | tree-sitter type usage | Shipped |
| `data_flow` | argument passing + field mutations + **return-value flows** | Shipped |
| `reads` | field read access | Shipped |
| `writes` | field write access | Shipped |
| `route` | HTTP route registration (Echo/Flask/Express) | Shipped |
| `control_flow` | CFG: if/else branches, loops, returns | Shipped |
| `data_dep` | def-use chains: variable definition → variable use | Shipped |

### Return-value taint propagation — Shipped

Implemented in all 3 language extractors:
- `extractGoReturnFlows` — `golang.go:1284`
- `extractPyReturnFlows` — `python.go:785`
- `extractTSReturnFlows` — `typescript.go:948`

Emits `data_flow` edges with `flow_type: "return_value"`, `via_var`, `from_call` metadata. Catches `result := helper(input); sink(result)` patterns that were previously invisible.

### API surface discovery — Shipped

Route extraction in all 3 language extractors:
- `extractGoRoutes` (Echo, stdlib) — `golang.go:482`
- `extractGoBindings` (c.Param, c.Bind, c.JSON) — `golang.go:693`
- `extractPyRoutes` (Flask, FastAPI) — `python.go:328`
- `extractTSRoutes` (Express, NestJS) — `typescript.go:511`

Infrastructure:
- `EdgeRoute` in `pkg/types/ast.go:40`
- `route` relation table in `assets/schema.surql:226`
- `ListRoutes()` on GraphStore — `internal/domain/ports.go:157`
- `APISurfaceService` with `Discover()` + `GenerateOpenAPI()` — `internal/app/api_surface_service.go`
- `commit0 api discover` + `commit0 api spec` — `cmd/api.go`
- PII detection heuristic (`detectPIIFields`)
- Auth middleware detection (`detectAuthMiddleware`)
- Tests: `internal/app/api_surface_service_test.go` (6 tests)

### CPG-inspired edges — Shipped

Control flow and data dependence in all 3 language extractors:
- `extractGoCFG` / `extractGoDataDep` — `golang.go:869` / `golang.go:1058`
- `extractPyCFG` / `extractPyDataDep` — `python.go:518` / `python.go:623`
- `extractTSCFG` / `extractTSDataDep` — `typescript.go:664` / `typescript.go:775`

Infrastructure:
- `EdgeControlFlow` + `EdgeDataDep` in `pkg/types/ast.go:45-49`
- `control_flow` + `data_dep` tables in `assets/schema.surql:238-258`
- Schema version: 11

CFG metadata: `branch_type` (sequential, if_true, if_false, loop_entry, loop_back, return), `condition`
DataDep metadata: `var_name`, `def_line`, `use_line`, `def_type` (parameter, assignment, return_value, for_range)

### AnalysisService — Shipped (needs improvement)

`internal/app/analysis_service.go` — renamed from SecurityService.
- `Scan()` runs 4 taint rules (SQL injection, command injection, XSS, path traversal)
- `checkTaintRule()` uses `strings.Contains()` for source/sink matching — **naive, produces false positives/negatives**
- `checkAuthGaps()` detects HTTP handlers without auth middleware callers
- `llmVerifyIssues()` optional LLM-based false positive filtering
- **No dedicated test file** — `analysis_service_test.go` does not exist
- **No CLI command** — `cmd/security.go` does not exist

---

## Principle

The code graph stores neutral facts. Vulnerabilities are properties of **flows** (unsanitized user input reaching a sensitive operation), not properties of individual nodes. No security classifications are persisted on nodes — all analysis happens at query time.

---

## Next Features (not yet implemented)

### Feature 1: Upgrade AnalysisService to use CPG edges

The graph now has `control_flow` and `data_dep` edges, but `AnalysisService.checkTaintRule()` still uses `strings.Contains()` matching on qualified names and ignores the CPG edges entirely.

**What to build:**
- Replace `strings.Contains` source/sink matching with graph-based flow analysis
- Use `data_dep` edges for precise def-use chain taint tracking (which variable definition reaches the sink?)
- Use `control_flow` edges for path-sensitive analysis (is the sanitizer on ALL paths to the sink, or only some?)
- Add `analysis_service_test.go` with test coverage

**Why:** The infrastructure is built (CPG edges are in the graph). The analysis layer needs to read it.

### Feature 2: CLI security scan command

`cmd/security.go` with:
- `commit0 security scan --repo <slug>` — runs AnalysisService.Scan()
- `commit0 security scan --ci --min-severity high` — exit code 1 for CI gating
- Formatted output with taint paths, file:line references

**Why:** AnalysisService exists but has no CLI entry point.

### Feature 3: Per-endpoint exposure analysis

`commit0 api expose "GET /api/v1/users/:id"` — shows:
- Input parameters and where they flow
- Response fields with PII flags
- Auth middleware chain
- Data stores reached

**Why:** APISurfaceService.Discover() returns aggregate data. Per-endpoint deep-dive requires tracing taint from specific route parameters through the handler's call chain.

### Feature 4: Dependency risk assessment

OSV API client + manifest parsers + reachability analysis via graph traversal. Cross-reference with API surface for exposure scoring.

**Why:** The graph can answer "does user input reach the vulnerable function through which API endpoints?" — no other dependency scanner can.

---

## Build Order

```
Feature 1 (Upgrade AnalysisService) → Feature 2 (CLI command) → Feature 3 (Exposure) → Feature 4 (Dependencies)
```

Feature 1 is prerequisite — the improved analysis service powers everything downstream.
