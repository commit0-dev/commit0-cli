# commit0 MCP Server — Interface Design

> **Note (2026-05-11):** This document references the GitHub-Issue ROADMAP convention (`Refs ROADMAP issue commit0-dev/commit0#15`, "Refs #15", "the `[ROADMAP]` issue") that was deprecated on 2026-05-11. The canonical roadmap now lives in `plans/ROADMAP.md` (in this repo, migrated from Issue #2; the parent commit0 repo migrated from Issue #15 — see `plans/260511-2227-migrate-roadmap-to-plans-kanban/` there). The branch-shipping plan in §"Implementation plan" describes how the work would have been tracked under the old convention; the technical design itself is unchanged by the migration.

> **Status:** Phase 1 — research and design only. No production code in this PR.
> Refs ROADMAP issue [`commit0-dev/commit0#15`](https://github.com/commit0-dev/commit0/issues/15).
> Source for this design: [`docs/references/agentic-code-analyzers-survey-2026.md`](agentic-code-analyzers-survey-2026.md), recommendation #2.

## 1. TL;DR

- **Goal:** ship a stdio MCP server so Claude Code, Cline, Cursor, goose, and any other MCP-aware client can call commit0's code-intelligence services as first-class tools.
- **Spec target:** MCP protocol version `2025-11-25` (latest as of run date — confirmed via spec.modelcontextprotocol.io). Older negotiation down to `2024-11-05` is forced by the spec wherever a client sends an older version.
- **Picked SDK:** [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) v1.5.0 — official, Anthropic + Google co-maintained, Apache 2.0, struct-reflection schema generation, stdio + Streamable HTTP transports. Justified in §3.
- **Picked architecture:** **Option A** — `commit0 mcp` subcommand inside the existing server module. Process embeds the full adapter graph and calls services in-process. No HTTP serialization tax. Justified in §7.
- **Tool surface:** **13 tools** prefixed `commit0_` covering query, trace, blast, field_flow, find_root_cause, scan_security, api_surface, lookup, neighborhood, show_node, list_repos, list_files, index_status. Index kick-off is fire-and-forget returning a job ID. Tool surface mirrors GitNexus’s prior art (13 tools, not 16 as the survey claimed — see §4) but improves naming consistency and adds graph-native primitives (`neighborhood`, `show_node`).
- **Resources:** opt-in only. `node://<id>` and `file://<repo>/<path>` schemes if implementation is cheap; otherwise punt to v2. **Recommend yes** for `node://`, **no** for the rest. See §6.
- **Prompts:** **no.** Slash-command-style prompts add surface area without value when the LLM can already discover and call tools. See §6.
- **Top 3 risks:** (1) `commit0 mcp` requires SurrealDB and adapters available at startup — single-binary stdio with embedded SurrealDB or a hard dependency on `docker compose up` for the DB? (2) sync vs streaming for `query` and `trace` — current LLM explainer streams 5–15 s; MCP `tools/call` is one-shot unless we use `progress` notifications. (3) auth — stdio has no auth surface, but if we expose a Streamable HTTP transport later we owe OAuth + Origin checks.
- **Effort:** **~1 800–2 200 LoC** including tests, split across **4 PRs** (skeleton, search-tools, trace-tools, meta-tools). See §13.
- **Implementation plan:** see §10. Branch off `main`: `feat/mcp-server-skeleton` → `feat/mcp-tools-search` → `feat/mcp-tools-trace` → `feat/mcp-tools-meta`.

## 2. MCP protocol primer (commit0-relevant subset)

**Protocol version string (latest):** `2025-11-25`. Wire format: JSON-RPC 2.0 over UTF-8. Older versions (`2025-06-18`, `2025-03-26`, `2024-11-05`) remain negotiable. The official Go SDK supports the full range starting at `2024-11-05`.

**Message types we need:**

| Method | Direction | Required for commit0 | Notes |
|---|---|---|---|
| `initialize` | C→S | **Yes** | Capability + version negotiation, must precede everything else. |
| `notifications/initialized` | C→S | **Yes** | Sent after successful init. |
| `tools/list` | C→S | **Yes** | Discovery. Pagination via `cursor` is supported but commit0 has 13 tools — single page. |
| `tools/call` | C→S | **Yes** | The whole point. |
| `notifications/tools/list_changed` | S→C | Optional | Only useful if commit0 dynamically reloads tool definitions — we don’t. |
| `resources/list` + `resources/read` | C→S | Optional (deferred) | See §6, ship in v2 if at all. |
| `prompts/list` + `prompts/get` | C→S | Optional (deferred) | Skip — see §6. |
| `completion/complete` | C→S | Optional | Useful for client autocomplete on `repo_slug` etc.; nice-to-have. |
| `sampling/createMessage` | S→C | **Skip** | commit0’s services already use their own `LLMExplainer` adapter; piggy-backing on the host’s LLM via sampling is a separate (large) refactor. |
| `elicitation/create` | S→C | **Skip** | We don’t need to ask the user mid-tool-call. |
| `logging/setLevel` + `notifications/message` | S→C | Optional | Cheap to wire up via `slog`. Recommend yes. |
| `notifications/progress` | S→C | **Strongly recommended** | Long-running tools (`query`, `trace`, `find_root_cause`) emit progress so Claude Code shows a spinner instead of timing out. |
| `ping` | both | Optional | SDK ships it for free. |

**Transports.** The 2025-11-25 spec defines exactly two: **stdio** and **Streamable HTTP**. The older `HTTP+SSE` transport is deprecated. For Claude Code stdio is the path of least resistance — a single `claude mcp add` line, no auth, no port. For commit0 we ship **stdio first**, leave Streamable HTTP behind a flag for a future remote-deployment story.

**Stdio framing** (per spec): line-delimited JSON, no embedded newlines, UTF-8. Server may write logs to stderr. Server **MUST NOT** write anything to stdout that is not a valid JSON-RPC message. This is the #1 footgun — every `fmt.Println` or stray `log.Printf` to stdout corrupts the channel.

**Capability negotiation.** Server declares which capabilities it supports in the `initialize` response. We declare `tools` (with `listChanged: false`), and optionally `logging`. We do not declare `resources`, `prompts`, or `completions` until we ship them. Clients ignore capabilities we don’t declare.

**Error semantics.** Two distinct mechanisms — the spec is explicit:

1. **Protocol errors** — JSON-RPC `error` object, codes per JSON-RPC 2.0 (`-32700` parse, `-32600` invalid request, `-32601` method not found, `-32602` invalid params, `-32603` internal). Used for unknown-tool and malformed-call cases. Models can rarely self-correct from these.
2. **Tool execution errors** — `result` object with `isError: true` and a textual explanation in `content`. The model **can** read these and retry with corrected arguments. **commit0 maps every `domain.DomainError` to this form**, not to a JSON-RPC error.

**Pitfalls.**
- **stdout pollution** — see above; single biggest reason early MCP servers fail.
- **Token budget** — Claude Code warns when MCP tool output exceeds 10 000 tokens; users can set `MAX_MCP_OUTPUT_TOKENS`. commit0’s LLM explanations can be 2–4 k tokens; tool results that include trace trees plus explanations can blow this. We default `no_explain: true` and add an explicit `with_explanation` flag (see §5).
- **Client capability spread** — Cursor and Cline both *do* support tools but neither implements `sampling`/`elicitation` consistently. Don’t require them.

## 3. Go SDK landscape — pick one

Surveyed three actively maintained Go MCP SDKs. Stats fetched from GitHub on 2026-05-07.

| SDK | Stars | Last release | Maintainer | Spec coverage | Stdio | Streamable HTTP | Schema gen | Ships client? | License |
|---|---|---|---|---|---|---|---|---|---|
| `mark3labs/mcp-go` | **8 670** | v0.52.0, 2026-05-05 | Ed Zynda + community | 2024-11-05 → 2025-11-25 | yes | **yes** (`server.NewStreamableHTTPServer`) | reflection + `mcp.WithString("...", Required(), Enum(...))` builders | yes | MIT |
| `modelcontextprotocol/go-sdk` | **4 486** | v1.5.0, ~Apr 2026 | Anthropic + Google | 2024-11-05 → 2025-11-25 | yes (`StdioTransport`, `CommandTransport`) | yes (newer, less battle-tested than mcp-go) | reflection from struct tags (`json` + `jsonschema`) | yes | Apache 2.0 |
| `metoro-io/mcp-golang` | 1 217 | active | metoro.io | partial | yes | partial | yes | yes | MIT |

**Pick: `modelcontextprotocol/go-sdk`.**

Justification:

1. **Official.** Maintained in collaboration between Anthropic (the spec authors) and Google. The SDK is the reference implementation — every spec change lands here first. mcp-go has had to play catch-up on multiple revisions.
2. **License.** Apache 2.0 vs MIT is roughly equivalent permissively, but the official SDK adds an explicit patent grant. commit0 is shipping under a permissive license too — Apache compounds well.
3. **Schema generation from `jsonschema` struct tags** matches commit0’s existing convention — every service request type is already a Go struct with `json` tags. We add `jsonschema:"description"` tags and the SDK reflects the rest. mcp-go’s `WithString().Required().Enum()` builder is fine but it’s a parallel source of truth diverging from our request structs.
4. **Stdio is rock-solid.** Both SDKs handle the framing correctly; the official one has fewer foot-guns around concurrent writes (single writer goroutine).
5. **The future is the official SDK.** mcp-go itself acknowledges this — the official SDK was specifically designed to align with mcp-go, then "diverged in a number of ways in order to keep the APIs minimal." Picking mcp-go now means migrating later.

**Trade-off accepted:** mcp-go has a more mature Streamable HTTP server (`server.NewStreamableHTTPServer`). If we later need remote MCP and the official SDK’s HTTP support is still rough, we can wrap the official SDK’s `Server` in our own HTTP transport without losing the tool definitions. Stdio-first means this trade-off is pre-paid.

## 4. GitNexus’s 13 MCP tools — concrete reference

The agentic-code-analyzers survey claimed GitNexus exposes "16 tools." That number is wrong. Reading the actual source (`gitnexus/src/mcp/tools.ts` at HEAD on 2026-05-07) yields exactly **13 tools**. The survey appears to have counted three legacy aliases (`search`, `explore`, `overview`) that the README mentions but `tools.ts` does not register.

Source files:
- `gitnexus/src/mcp/server.ts` — server bootstrap (`setRequestHandler(ListToolsRequestSchema, …)`, `tools.map(...)`)
- `gitnexus/src/mcp/tools.ts` — the 13 tool definitions, 550 lines

| # | Name | Description (1-line) | Notable inputs |
|---|---|---|---|
| 1 | `list_repos` | List all indexed repositories with stats. | empty input |
| 2 | `query` | Hybrid BM25 + semantic + RRF search for execution flows by concept. | `query` (req), `task_context`, `goal`, `limit`, `max_symbols`, `include_content`, `repo`, `service` |
| 3 | `cypher` | Execute raw Cypher against the LadybugDB graph. | `query` (req), `repo` |
| 4 | `context` | 360° view of one symbol: callers, callees, refs, processes. | `name`, `uid`, `file_path`, `kind`, `include_content`, `repo`, `service` |
| 5 | `detect_changes` | Map uncommitted git changes to affected execution flows. | `scope` (`unstaged`/`staged`/`all`/`compare`), `base_ref`, `repo` |
| 6 | `rename` | Coordinated multi-file rename via graph + text search. | `symbol_name`, `symbol_uid`, `new_name` (req), `file_path`, `dry_run`, `repo` |
| 7 | `impact` | Blast radius for a symbol with depth grouping and confidence filter. | `target` (req), `target_uid`, `direction` (req), `file_path`, `kind`, `maxDepth`, `crossDepth`, `relationTypes`, `includeTests`, `minConfidence`, `repo`, `service`, `subgroup`, `timeoutMs`, `timeout` |
| 8 | `route_map` | Show API route mappings: which components fetch which endpoints. | `route`, `repo` |
| 9 | `tool_map` | Show MCP/RPC tool definitions with handlers. | `tool`, `repo` |
| 10 | `shape_check` | Check response shapes for routes against consumer property accesses. | `route`, `repo` |
| 11 | `api_impact` | Pre-change report for a route handler showing consumers and risks. | `route` or `file` (one req), `repo` |
| 12 | `group_list` | List repository groups or one group’s details. | `name` |
| 13 | `group_sync` | Rebuild contract registry for a repo group. | `name` (req), `skipEmbeddings`, `exactOnly` |

**What’s good:**
- Dedicated `cypher` for power users — agents that learn the schema can do graph queries we never expose as tools.
- `direction` + `maxDepth` + `crossDepth` + `relationTypes` on `impact` — rich blast-radius shape-shifting from one tool.
- Multi-repo concept (groups) — interesting but YAGNI for commit0 v1.

**What’s clumsy:**
- Inconsistent naming: `query` vs `context` vs `impact` vs `route_map` (verb / noun / verb / noun). commit0 standardises on `commit0_<noun_or_verb>` and stays in one mode.
- `target` vs `target_uid` vs `name` vs `uid` — accepting either string-name or graph-UID is fine but the duplication invites confusion. commit0 uses one input field per tool, with optional disambiguation via `file_path`.
- `timeoutMs` *and* `timeout` on the same tool — a forced pick-one is cleaner.
- No long-running-job model; `impact` with maxDepth=10 on a big repo can hang the whole MCP channel.

commit0 inherits the good bits and drops the clumsy ones.

## 5. commit0 MCP tool surface — concrete schemas

**Naming convention.** All tools prefixed `commit0_` to namespace under MCP. Names use snake_case. All inputs are JSON Schema 2020-12 (the spec default).

**Common parameters.**
- `repo_slug` (string, required for repo-scoped tools) — uniquely identifies an indexed repository.
- `with_explanation` (boolean, default `false`) — when true, the LLM-generated explanation is included in the response. We default off because it adds 5–15 seconds and 2–4 k tokens.

**Output convention.** Every tool returns *both*:
1. A `text` content item with a human-readable Markdown summary (so the LLM gets readable context).
2. A `structuredContent` JSON object validating against the tool’s `outputSchema` (so the LLM can pattern-match for further tool calls).

This double-emit is explicitly recommended by the spec: "a tool that returns structured content SHOULD also return the serialized JSON in a TextContent block."

---

### 5.1 `commit0_query`
**Description (verbatim, what the LLM reads):**
> Hybrid semantic + full-text search over the indexed code graph. Returns the top-K most relevant code nodes (functions, classes, files) for a natural-language question, fused via reciprocal rank fusion and graph-augmented with 1-hop neighbors. Use this for questions like "where is X implemented?" or "how does Y work?" — prefer it over grep for conceptual queries.

**Maps to:** `app.QueryService.Query` (`server/internal/app/query_service.go:59`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "question":         { "type": "string", "description": "Natural-language question about the code." },
    "repo_slug":        { "type": "string", "description": "Indexed repository slug." },
    "top_k":            { "type": "integer", "minimum": 1, "maximum": 50, "default": 10 },
    "min_score":        { "type": "number", "minimum": 0, "maximum": 1, "default": 0.5 },
    "node_kinds":       { "type": "array", "items": { "type": "string", "enum": ["function","class","file","module"] } },
    "file_path":        { "type": "string", "description": "Optional path/prefix filter (e.g. \"server/internal/adapters/\")." },
    "with_explanation": { "type": "boolean", "default": false }
  },
  "required": ["question", "repo_slug"]
}
```

**Output (Go struct → JSON):**
```go
type QueryToolResult struct {
    Nodes       []ScoredNodeOut    `json:"nodes"`        // qualified, file_path, start_line, end_line, kind, score
    Explanation string             `json:"explanation,omitempty"`
    RepoSlug    string             `json:"repo_slug"`
    Timing      TimingOut          `json:"timing"`       // embed_ms, search_ms, explain_ms, total_ms
}
```

---

### 5.2 `commit0_trace`
**Description:**
> Trace forward (callees) or reverse (callers) call chains starting from a symbol. Returns a tree of hops up to `depth`. Forward direction answers "what does this function do?"; reverse answers "who depends on this function?".

**Maps to:** `app.TraceService.Trace` (`trace_service.go:51`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "symbol":           { "type": "string", "description": "Qualified or partial symbol name." },
    "repo_slug":        { "type": "string" },
    "direction":        { "type": "string", "enum": ["forward","reverse"], "default": "forward" },
    "depth":            { "type": "integer", "minimum": 1, "maximum": 20, "default": 5 },
    "with_explanation": { "type": "boolean", "default": false }
  },
  "required": ["symbol", "repo_slug"]
}
```

**Output:** `Root CodeNodeOut`, `Tree []TraceHopOut` (recursive: `node`, `children`, `edge_kind`), `Direction`, `Timing`.

---

### 5.3 `commit0_blast`
**Description:**
> Compute the blast radius of changing a symbol — every node transitively affected via call, data-flow, and route edges, grouped by hop count. Use this BEFORE modifying a function to see who depends on it.

**Maps to:** `app.BlastService.Blast` (`blast_service.go:49`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "symbol":           { "type": "string" },
    "repo_slug":        { "type": "string" },
    "max_depth":        { "type": "integer", "minimum": 1, "maximum": 20, "default": 10 },
    "with_explanation": { "type": "boolean", "default": false }
  },
  "required": ["symbol", "repo_slug"]
}
```

**Output:** `Target CodeNodeOut`, `Affected []AffectedNodeOut` (`node`, `hop_count`, `relation`), `Summary`, `Timing`.

---

### 5.4 `commit0_field_flow`
**Description:**
> Trace how a specific field flows through functions — which functions read it, mutate it, and pass it on. Use this to understand data lineage, e.g. "where does `User.Email` get validated before it reaches the database?".

**Maps to:** `app.FieldFlowService.TraceFieldFlow` (`field_flow_service.go:52`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "symbol":         { "type": "string", "description": "Qualified function name to start from." },
    "field_path":     { "type": "string", "description": "Dotted field path, e.g. \"user.Email\"." },
    "repo_slug":      { "type": "string" },
    "direction":      { "type": "string", "enum": ["forward","reverse","both"], "default": "forward" },
    "depth":          { "type": "integer", "minimum": 1, "maximum": 20, "default": 10 },
    "show_mutations": { "type": "boolean", "default": false }
  },
  "required": ["symbol", "repo_slug"]
}
```

**Output:** `Chains []FieldFlowChainOut`, `MutationPoints []MutationOut`, `Timing`.

---

### 5.5 `commit0_find_root_cause`
**Description:**
> Run the 6-step "commit zero" detection algorithm to find the commit that most likely introduced a described bug. Combines semantic search, field-flow tracing, the temporal graph, and an LLM-verified diff analysis. Long-running (10–60 s).

**Maps to:** `app.RootCauseAnalysisService.FindRootCause` (`rootcause_analysis_service.go:68`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "description": { "type": "string", "description": "Bug description or symptom." },
    "test_name":   { "type": "string", "description": "Optional failing-test name." },
    "repo_slug":   { "type": "string" },
    "repo_path":   { "type": "string", "description": "Filesystem path to the repo (needed to read git history)." },
    "since":       { "type": "string", "description": "Optional time bound: \"3 days ago\" or a commit SHA." }
  },
  "required": ["description", "repo_slug", "repo_path"]
}
```

**Output:** `SuspectCommits []CommitOut`, `Confidence float64`, `Reasoning string`, `Timeline []TimelineEntry`, `Timing`.

This tool emits `notifications/progress` per step (LOCATE → TRACE → TIMELINE → CORRELATE → VERIFY → REPORT) so Claude Code shows progress instead of timing out.

---

### 5.6 `commit0_scan_security`
**Description:**
> Scan a repo for security vulnerabilities using taint analysis on the data-flow graph plus auth-gap detection on the call graph. Returns issues ranked by severity with taint paths from source to sink.

**Maps to:** `app.AnalysisService.Scan` (`analysis_service.go:101`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "repo_slug": { "type": "string" }
  },
  "required": ["repo_slug"]
}
```

**Output:** `Issues []AnalysisIssueOut` (`severity`, `category`, `title`, `file`, `line`, `function`, `description`, `taint_path`, `fix`), `ScannedNodes int`, `Timing`.

---

### 5.7 `commit0_api_surface`
**Description:**
> Discover all HTTP API endpoints in a repo with method, path, handler, parameters, and response shape. Optionally emit OpenAPI 3.1 JSON.

**Maps to:** `app.APISurfaceService.Discover` + `GenerateOpenAPI` (`api_surface_service.go:44, :110`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "repo_slug":     { "type": "string" },
    "format":        { "type": "string", "enum": ["json","openapi"], "default": "json" }
  },
  "required": ["repo_slug"]
}
```

**Output:** `Endpoints []EndpointOut`, plus `OpenAPI string` (only when `format=openapi`).

---

### 5.8 `commit0_lookup`
**Description:**
> Resolve a qualified symbol name to a single code node with its metadata (file path, line range, kind, qualified name). No search, no LLM — pure index lookup. Use this when an earlier tool returned a symbol and you need its exact location.

**Maps to:** `domain.OpenCodeGraph.FindNode` (`open_code_graph.go:23`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "qualified": { "type": "string" },
    "repo_slug": { "type": "string" }
  },
  "required": ["qualified", "repo_slug"]
}
```

**Output:** `Node CodeNodeOut` or `null` if not found.

---

### 5.9 `commit0_neighborhood`
**Description:**
> Return the immediate graph neighborhood of a node: callers, callees, data sources, data sinks, reads, writes. Cheaper than `trace` (one hop only). Use this to understand what one function touches before deciding whether to call `trace` or `blast`.

**Maps to:** `domain.OpenCodeGraph.Neighbors` (`open_code_graph.go`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "node_id":   { "type": "string", "description": "Internal graph ID returned by an earlier tool." },
    "qualified": { "type": "string", "description": "Alternative: qualified symbol name (resolved internally)." },
    "repo_slug": { "type": "string", "description": "Required when looking up by qualified." }
  }
}
```
(One of `node_id` or `qualified` required — enforced server-side, mirroring how GitNexus accepts either.)

**Output:** `Callers, Callees, DataSources, DataSinks []NeighborOut`, `Reads, Writes []string`.

---

### 5.10 `commit0_show_node`
**Description:**
> Return the full body and metadata for one node by ID. Use after `query` or `lookup` when you need the actual source code.

**Maps to:** `domain.OpenCodeGraph.GetNode` (`open_code_graph.go:22`).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "node_id": { "type": "string" }
  },
  "required": ["node_id"]
}
```

**Output:** `Node CodeNodeOut` with full `Body` populated (other tools omit body to save tokens).

---

### 5.11 `commit0_list_repos`
**Description:** List all indexed repositories with their slug, languages, last-indexed timestamp, and node counts.

**Maps to:** `app.RepoService.ListRepos` (`repo_service.go:79`).

**Input schema:** `{ "type": "object", "additionalProperties": false }`.

**Output:** `Repos []RepoOut`.

---

### 5.12 `commit0_list_files`
**Description:** List indexed files for a repo, optionally filtered by path prefix.

**Maps to:** `domain.OpenCodeGraph.ListNodes` with `kind=file` (open_code_graph.go).

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "repo_slug":    { "type": "string" },
    "path_prefix":  { "type": "string" },
    "limit":        { "type": "integer", "minimum": 1, "maximum": 1000, "default": 100 }
  },
  "required": ["repo_slug"]
}
```

**Output:** `Files []FileOut` (`path`, `language`, `node_count`).

---

### 5.13 `commit0_index_status`
**Description:** Check the state of an in-progress or completed indexing job.

**Maps to:** `app.IndexTracker` snapshot accessor (`index_tracker.go`). Need to add a `Get(jobID)` method on `IndexService` that returns the tracker for a given job ID — currently the tracker is created per-call but there’s no public registry.

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "job_id": { "type": "string" }
  },
  "required": ["job_id"]
}
```

**Output:** `Status string` (`"pending"|"running"|"completed"|"failed"`), `Stage string`, `Progress { walked, parsed, embedded, stored, total }`, `Errors []StageError`, `StartedAt`, `CompletedAt`.

---

### Decision: NO `commit0_index` tool in v1
The brief flagged this as borderline. **Recommend not exposing** in the first ship for two reasons:

1. Indexing requires a filesystem path to the repo — agents don’t reliably know the absolute path their host process can see, so we’d either constantly fail with `ENOENT` or accept a path hint that can be wrong.
2. Indexing is the kind of "destructive enough to need user consent" action the spec warns about — better to keep it as an explicit `commit0-cli index .` step the user runs once.

If we change our mind, the fire-and-forget shape is: `commit0_index` returns `{ "job_id": "ix-abc123" }` immediately, the LLM polls `commit0_index_status` until done.

## 6. Resources + prompts (optional surfaces)

**Resources — recommend ONE: `node://<id>`.**
Rationale: when a tool returns a node (e.g. from `query`), the response includes a node ID. The LLM may want to re-fetch that node later in the conversation without re-running the search. A `resources/read` for `node://<id>` returns the node body — same shape as `commit0_show_node` but in the resources namespace. Cost is ~30 LoC over the existing `show_node` tool. **Yes ship it.**

`repo://<slug>` and `file://<repo>/<path>` add no capability the tools don’t already cover (`list_repos` and `list_files` + `show_node`). **No.**

**Prompts — no.**
The temptation is templates like `/explain-call-chain {symbol}` or `/find-data-flow {field}`. But Claude Code already exposes commands; users don’t need a second prompt-template surface inside the MCP server. The LLM can compose tool calls directly. Prompts also force a string-template UX (the user clicks a slash command) that doesn’t fit commit0’s conversational flow. **Skip until a real user asks for it.**

## 7. Architecture: where the MCP server lives

Three options. Pick: **(A)** — embedded subcommand in the server module.

### Option A — `commit0 mcp` subcommand (chosen)
```
commit0/server/
├── cmd/mcp.go               # cobra subcommand, calls into adapters/mcp
└── internal/adapters/mcp/   # MCP server bootstrap, tools, schemas
```

Pros:
- Zero serialization tax — services are called as Go functions.
- Reuses `wireDeps()` so adapter wiring is in one place.
- Same binary as `commit0 serve` — operators install once.

Cons:
- Stdio process must boot the full adapter graph (SurrealDB connector, embedder, parser). That means SurrealDB must be reachable when Claude Code spawns the subprocess.
- A misbehaving adapter (e.g. SurrealDB unreachable) crashes the MCP channel rather than failing one tool call.

Mitigations:
- Adapters are constructed lazily — `commit0 mcp` boots with placeholders, only initialises an adapter on first tool call that needs it. Failed initialisation surfaces as an MCP `tool_error` (`isError: true`), not a process exit.
- Health-probe SurrealDB during `initialize` and bake a clear error message into the response if down: "commit0 MCP requires SurrealDB at $SURREAL_URL; run `docker compose up surreal`."

### Option B — Standalone binary in commit0-cli, calls HTTP API
Pros: decouples MCP from the server stack; deployable independently; the MCP process is tiny.
Cons: every tool call pays HTTP serialization (~5–20 ms) plus an extra network hop. Authentication becomes a real problem (the CLI binary needs credentials). And worst, **stdio MCP without a running `commit0 serve` fails silently** — Claude Code shows a vague initialize error.

### Option C — Subcommand in commit0-cli, calls HTTP API
Same trade-offs as B but bundled in the existing CLI binary. Slightly better deploy story than B, same latency penalty.

**Decision: A.** The server already runs in Docker for users; embedding the MCP subcommand in the server image (with `claude mcp add commit0 -- docker exec -i commit0-server commit0 mcp` or running locally via `commit0 mcp` if the user has a local install) is the cleanest path. Documenting `docker compose up` as a prerequisite is no worse than what `commit0 serve` already requires.

## 8. Claude Code integration

Verified against [Claude Code MCP docs](https://code.claude.com/docs/en/mcp), 2026-05-07.

**Project-level `.mcp.json`** (committed to the repo, shared with the team):
```json
{
  "mcpServers": {
    "commit0": {
      "type": "stdio",
      "command": "commit0",
      "args": ["mcp"]
    }
  }
}
```

Or, for users who prefer to invoke through `docker exec`:
```json
{
  "mcpServers": {
    "commit0": {
      "type": "stdio",
      "command": "docker",
      "args": ["exec", "-i", "commit0-server", "commit0", "mcp"]
    }
  }
}
```

**User-level config** (Claude Code stores user-scoped MCP servers in `~/.claude.json`, not a separate `~/.claude/mcp.json` — the docs corrected this naming):
```bash
claude mcp add --scope user --transport stdio commit0 -- commit0 mcp
```

**Permissions.** commit0 reads files (via the indexer) and reads from SurrealDB, but the MCP surface itself only reads from already-indexed data. No filesystem mutations, no network egress to third parties (LLM provider aside, which is governed by env vars on the server). No restricted permissions required.

**Verification.** Two paths:
1. From inside Claude Code: type `/mcp` — the server should appear listed as connected with 13 tools.
2. From the shell: `claude mcp list` shows registered servers; `claude mcp get commit0` shows status.
3. Self-test: `commit0 mcp --self-test` (planned, see §11) round-trips initialize → tools/list → a sample query → exits 0.

## 9. Other client integrations

| Client | Config location | Notes |
|---|---|---|
| **Cline** (VS Code) | `cline_mcp_settings.json` in the VS Code extension state directory. UI: Cline panel → MCP Servers → Edit. Same `mcpServers` schema. | Tested with stdio servers; supports `tools` capability. No `sampling`. |
| **Cursor** | `~/.cursor/mcp.json` (user-scope) or `.cursor/mcp.json` in the project root. Same schema. | Cursor honours `tools` and `prompts`; `sampling` support is partial. |
| **goose** (Block) | `~/.config/goose/config.yaml`, under `extensions:` key. YAML, not JSON, but the underlying spec model is the same. | Stdio-only as of 2026; goose has its own task-augmented execution model that overlaps with MCP `tasks`. |

The `mcpServers` JSON shape is portable across Cline, Cursor, and Claude Code — the same `.mcp.json` file works in three of the four if dropped into the right location. We document this in the user-facing setup guide once we ship.

## 10. Implementation plan — file by file

All paths relative to `/Volumes/DATA/Coding/commit0/commit0/`.

### New files (server module)
| Path | Purpose | Est. LoC | Key types/functions |
|---|---|---|---|
| `server/cmd/mcp.go` | Cobra subcommand `commit0 mcp`. | 80 | `mcpCmd` (Cobra command), `runMCP(ctx, deps) error` |
| `server/internal/adapters/mcp/server.go` | Server bootstrap; wires `mcp.Server` and registers tools. | 180 | `New(deps Deps) (*mcp.Server, error)`, `type Deps struct { … }` |
| `server/internal/adapters/mcp/tools.go` | All tool registrations + handlers (one func per tool). | 700 | 13 handlers, each ~40–60 LoC |
| `server/internal/adapters/mcp/schemas.go` | Output structs with `json` + `jsonschema` tags. | 220 | `CodeNodeOut`, `ScoredNodeOut`, `TraceHopOut`, … |
| `server/internal/adapters/mcp/errors.go` | Map `domain.DomainError` → MCP tool-error result. | 60 | `toolError(err) (*mcp.CallToolResult, error)`, `protocolError(...)` |
| `server/internal/adapters/mcp/resources.go` | The single `node://<id>` resource handler. | 60 | `registerResources(s *mcp.Server, graph domain.OpenCodeGraph)` |
| `server/internal/adapters/mcp/progress.go` | Helper to emit `notifications/progress` from long-running tools. | 50 | `progressEmitter` |
| `server/internal/adapters/mcp/server_test.go` | Unit tests using the SDK's in-memory client. | 350 | 13 tool tests + initialize + capability negotiation |
| `server/internal/adapters/mcp/integration_test.go` | E2E using `mcp.CommandTransport` to spawn the subcommand. | 180 | `TestMCPRoundtrip` |

### Modified files
| Path | Change |
|---|---|
| `server/cmd/root.go` | Register `mcpCmd` alongside `serveCmd`. |
| `server/cmd/wire.go` | Add `WireMCPDeps(cfg)` returning the same adapter graph as `serve` minus the HTTP server. |
| `server/internal/app/index_service.go` | Add `GetTracker(jobID string) (*IndexTracker, error)` and a per-process tracker registry (~40 LoC). |
| `server/go.mod` | Add `github.com/modelcontextprotocol/go-sdk` dependency. |
| `docs/BACKEND.md` | Add an "MCP" section linking to this design + setup snippets. |
| `Makefile` | New target `make build-mcp` (alias for `build-server` since they’re the same binary). |
| `README.md` | One-line mention with link to setup. |

**Total estimate: 1 880–2 050 LoC** (production + tests).

## 11. Test strategy

**1. Unit tests per tool handler (`server_test.go`).**
Use the official SDK’s in-memory client: `mcp.NewInMemoryTransport()` pairs a client and server in the same process. For each tool:
- Happy path: call with valid input, assert `IsError == false`, assert `structuredContent` matches an expected JSON shape.
- Validation failure: call with empty required field, assert `IsError == true` and the error message names the missing field.
- Underlying service failure (mocked): assert `IsError == true` and a clean error message (no Go stack traces).

**2. Capability + lifecycle tests (`server_test.go`).**
- `initialize` with protocol version `2025-11-25` returns the same version.
- `initialize` with `2024-11-05` returns the latest version we support (or that version, depending on negotiation rules).
- `tools/list` returns exactly 13 tools, names sorted, all with non-empty descriptions and valid input schemas.

**3. Integration test (`integration_test.go`).**
Build the binary with `go build`, spawn it via `mcp.CommandTransport{Command: "./bin/commit0", Args: []string{"mcp"}}`, run a full handshake plus one `commit0_query` call against a known-indexed test repo. Skip when `-short` is set.

**4. Inspector tool (manual verification).**
The MCP project ships `npx @modelcontextprotocol/inspector` — a UI for poking servers. Document in the README:
```bash
npx @modelcontextprotocol/inspector commit0 mcp
```
Opens a localhost UI, lists tools, lets the developer invoke them with form-filled inputs and see structured + text content side-by-side.

**5. Self-test command (`commit0 mcp --self-test`).**
```go
// In server/cmd/mcp.go, behind the --self-test flag:
//   1. Spawn an in-process server + client pair.
//   2. initialize → expect server caps include "tools".
//   3. tools/list → expect 13 tools with sorted names.
//   4. tools/call commit0_list_repos → expect non-error result.
//   5. Print "OK" and exit 0; print failure detail and exit 1.
```
Used in CI and as a debugging affordance for users.

**6. Lint guard.**
Add a golangci-lint custom check (or a doc-only convention enforced in review) that `fmt.Print*` and direct `os.Stdout` writes are forbidden in `internal/adapters/mcp/**` — protects against the stdio framing footgun.

## 12. Risks + open questions

1. **Long-running indexing — sync, fire-and-forget, or streaming?**
   *We recommend fire-and-forget + polling via `commit0_index_status`, BUT we’re not exposing `commit0_index` in v1.* If the user changes their mind, we need to decide between (a) a synchronous tool that emits `notifications/progress` (simple, but a 5-minute index will hit Claude Code’s default tool timeout) or (b) job-based polling. Decision needed.

2. **Auth.**
   Stdio has no auth surface — the host process trusts whatever it spawns. If we ever expose Streamable HTTP, the spec mandates Origin checks + binding to localhost + optional OAuth 2.0. Out of scope for v1, but tracking now so we don’t paint ourselves into a corner.

3. **Schema evolution.**
   Tool input/output shapes will evolve. The MCP spec has no versioning at the tool level — clients see whatever schema the server returns. **Plan:** treat tool I/O like a public HTTP API (semver-ish breaking changes need a major release) and document the contract in `docs/BACKEND.md` alongside HTTP.

4. **Streaming results.**
   `commit0_query` and `commit0_trace` currently stream the LLM explanation server-side. MCP `tools/call` is one-shot for the *result* — but `notifications/progress` lets us emit "I’m generating the explanation, 30% done" while the call is in flight. Need to decide: do we stream the explanation token-by-token via progress (rich UX, but the result still comes in one chunk), or just deliver the final result after the explanation finishes? **Recommend: skip explanations by default (`with_explanation=false`), and when explicitly requested, send a single non-streamed final result.** Saves complexity and tokens.

5. **Token budget per tool result.**
   Claude Code warns above 10 000 tokens. A `commit0_blast` on a 200-affected-node hit easily blows that. **Plan:** truncate the `Body` field of returned nodes (omit by default; `commit0_show_node` is the way to retrieve full body). Also cap `top_k` at 50 in the schema.

6. **Indexing lifecycle.**
   The `IndexTracker` is currently per-call — there's no place to look up an old job ID after the call returns. We add a per-process `map[jobID]*IndexTracker` keyed in `IndexService`, cleared after 1 hour. Memory bound: ~5 KB per tracker × concurrent jobs.

7. **Testing across MCP clients.**
   We can verify Claude Code compatibility ourselves but Cline/Cursor/goose are external. **Plan:** ship v1 with Claude Code as the supported integration, document the `.mcp.json` snippets for the others, and accept that compatibility issues with other clients will be filed as bugs and fixed reactively.

## 13. Effort estimate + suggested branch / PR strategy

**Total LoC:** ~1 880–2 050 (about 60% production, 40% tests).
**Total elapsed:** 6–9 working days for one engineer.

| # | Branch | PR title | Scope | Est. LoC | Days |
|---|---|---|---|---|---|
| 1 | `feat/mcp-server-skeleton` | `feat: (mcp) server bootstrap and lifecycle` | `cmd/mcp.go`, `adapters/mcp/server.go`, `errors.go`, `progress.go`, `wire.go`, `--self-test`, plus `initialize` + `tools/list` returning an empty list. | ~450 | 2 |
| 2 | `feat/mcp-tools-search` | `feat: (mcp) search tools (query, lookup, neighborhood, show_node)` | 4 tools + their schemas + handler tests. | ~500 | 1.5 |
| 3 | `feat/mcp-tools-trace` | `feat: (mcp) trace tools (trace, blast, field_flow, find_root_cause)` | 4 tools incl. `notifications/progress` for `find_root_cause`. | ~600 | 2 |
| 4 | `feat/mcp-tools-meta` | `feat: (mcp) meta tools and security/api surface (list_repos, list_files, index_status, scan_security, api_surface)` | 5 tools + integration test + node:// resource + docs. | ~500 | 2 |

Each branch is one PR closing/referencing issue #15 with `Refs #15`. The skeleton PR ships first (#15a), the others land in any order behind it. Each PR includes its own tests; no PR merges with failing tests.

After #15d ships, the parent #15 issue gets a closing comment with the merged PRs and a one-paragraph status update for the `[ROADMAP]` issue.

## 14. Sources

- [MCP specification 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25/) — basic, transports, lifecycle, tools sections.
- [MCP basic / messages + JSON Schema](https://modelcontextprotocol.io/specification/2025-11-25/basic).
- [MCP transports (stdio, Streamable HTTP)](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports).
- [MCP lifecycle and capability negotiation](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle).
- [MCP tools (tools/list, tools/call, error semantics)](https://modelcontextprotocol.io/specification/2025-11-25/server/tools).
- [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) — official Go SDK README, v1.5.0.
- [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) — community SDK, v0.52.0.
- [`metoro-io/mcp-golang`](https://github.com/metoro-io/mcp-golang).
- [Official Go SDK design discussion](https://github.com/orgs/modelcontextprotocol/discussions/364) — schema generation, transport choice, divergence from mcp-go.
- [Claude Code MCP integration docs](https://code.claude.com/docs/en/mcp) — `.mcp.json` schema, scopes, `claude mcp add` syntax.
- [GitNexus repo](https://github.com/abhigyanpatwari/GitNexus) — `gitnexus/src/mcp/server.ts`, `gitnexus/src/mcp/tools.ts`, `.mcp.json` (HEAD as of 2026-05-07).
- [agentic-OSS survey](agentic-code-analyzers-survey-2026.md) — `docs/references/agentic-code-analyzers-survey-2026.md` (this repo).
- commit0 source: `server/internal/app/{query,trace,blast,field_flow,rootcause_analysis,analysis,api_surface,repo,index,index_tracker}_service.go` for service signatures.
