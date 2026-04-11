# commit0 Self-Development Findings

## Findings — 2026-04-11

### Tool Gaps (commit0 is missing this capability)

- [ ] **No "server status" command**: No way to check if the server is busy (indexing, processing). `repo list` just hangs silently. Need: `commit0-cli status` that shows server state (idle, indexing N/M files, processing query)
- [ ] **No client-side timeout**: CLI commands hang forever when server is unresponsive. Need: `--timeout` flag with sensible default (30s for queries, 5min for index)
- [ ] **No "read file" capability**: Cannot read a specific file's content through commit0. If I want to see what a function does, I have to use `query` which returns search results, not the actual code. Need: `commit0-cli show <qualified.Name>` that prints the function body
- [ ] **No "list functions in file" capability**: Cannot ask "what functions are in sync_service.go?" Need: `commit0-cli ls <file-path>` that lists all nodes in a file
- [ ] **No "diff since last index" capability**: After making changes, no way to see what commit0 would re-index. Need: `commit0-cli diff` showing changed files since last index

### Wrong Results (tool returned incorrect/irrelevant output)

- [ ] **`query` returns agent functions for every search**: Even after centrality cap fix, `agent.AgentService.Chat` appears in most top-5 results regardless of query topic. The embedding model associates "chat" with too many concepts
- [ ] **`blast` returns 0 for most functions**: Interface dispatch not resolved — `is.store.UpsertNode` has no edge to `surreal.SurrealAdapter.UpsertNode`. Only same-package and receiver method calls are resolved
- [ ] **`trace` shows 0-1 hops for most functions**: Same root cause as blast — call edges through interfaces aren't in the graph. A forward trace from an HTTP handler shows nothing because the handler calls `s.syncSvc.Pull()` via a struct field, which isn't resolved

### Performance Issues (too slow, timeout, crash)

- [ ] **Server unresponsive during indexing**: All API calls hang while `index` is running. The indexing process locks SurrealDB, blocking query/trace/blast. No concurrent access possible
- [ ] **Query takes 10-20s**: `query --no-agent` takes 10-20s for every search (700ms embed + 300ms search + 5-15s explain). The "explain" LLM call dominates. Need: `--no-explain` flag for fast results
- [ ] **`analyze` hits Gemini rate limits**: The agent makes many tool calls, each requiring an LLM call. Hits 1M tokens/min limit within 2 minutes. Need: rate-limit retry/backoff in the agent
- [ ] **Force re-index crashes SurrealDB**: `index --force` triggers bulk delete that overloads SurrealDB (broken pipe). Can only do incremental index

### UX Issues (confusing output, bad defaults, missing flags)

- [ ] **No `--no-explain` flag on query**: Every query spends 5-15s on LLM explanation even when I just want the ranked list. Most of the time I only need the table
- [ ] **`analyze --focus` options not listed in help**: `--help` shows `all` as default but doesn't list valid options (architecture, dead-code, consistency, hotspots, data-flow, temporal)
- [ ] **CLI binary not in PATH**: Have to use `./bin/commit0-cli` instead of just `commit0-cli`. The Makefile builds to `bin/` but doesn't install
- [ ] **No progress bar for indexing**: `index .` shows "N files, M nodes..." on a single line that keeps appending. Hard to tell if it's stuck or making progress
- [ ] **`blast` and `trace` require fully qualified names**: `blast ImportBundle` fails — need `blast app.SyncService.ImportBundle`. No fuzzy matching or autocomplete

---

## Priority Ranking

### Blocking
1. **Server unresponsive during indexing** — can't use ANY tool while indexing. Must fix for the self-dev loop to work at all
2. **Interface dispatch not resolved** — blast and trace are blind to 90% of real call chains. The graph is fundamentally incomplete

### Major
3. **No `--no-explain` flag** — every query wastes 5-15s on explanation I don't need
4. **No `show` command** — can't read code through commit0, forced to use external tools
5. **No `ls` command** — can't list functions in a file
6. **Rate limit handling in agent** — analyze breaks after 2 minutes

### Minor
7. **No `--timeout` flag** — CLI hangs forever
8. **Binary not in PATH** — friction every command
9. **Analyze help text incomplete** — confusing for new users
10. **Blast/trace require qualified names** — friction, should support partial match

---

## Improvement Plan

### Fix #1: Non-blocking indexing
The indexing process should NOT block the query/trace/blast API. Options:
- Use separate SurrealDB connections for index vs query
- Queue index writes and process asynchronously
- Add a read-only mode while indexing is active

### Fix #2: `--no-explain` flag on query
Add a flag that skips the LLM explanation step. Return just the ranked table.
Changes: `cli/cmd/query.go` (add flag), `sdk/query.go` (pass flag), 
`server/internal/app/query_service.go` (skip explain call)

### Fix #3: `commit0-cli show <symbol>` command
New command that looks up a node by qualified name and prints its body.
Uses existing `GetNodeByQualified` → returns the `Body` field.
Changes: `cli/cmd/show.go` (new), `sdk/show.go` (new), 
handler in `server/internal/adapters/http/handlers.go` (uses existing node lookup)

### Fix #4: `commit0-cli ls <file-path>` command
New command that lists all nodes in a file with their kind, qualified name, and line range.
Uses existing `ListNodesByFile`.
Changes: `cli/cmd/ls.go` (new), `sdk/ls.go` (new)
