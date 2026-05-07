# Agentic Code-Understanding Solutions — OSS Survey 2026

> Strategic survey of full-stack agentic OSS that index a codebase as a graph (or graph-like context) and drive an LLM. Infrastructure layers (tree-sitter, Joern, Semgrep, CodeQL, Glean, Stack Graphs, SCIP/LSIF) are explicitly out of scope.
>
> Audience: commit0 maintainers. Run date: 2026-05-07. Star/fork counts and GitHub timestamps are from the GitHub REST API on the run date.

## 1. TL;DR

- The category is dominated by **agent runtimes** (Cline, OpenHands, goose) and **embedded codebase RAG** (Continue, Aider). Only **Aider** (PageRank repo map) and **GitNexus** (full Graph RAG) treat the graph as a first-class agent surface.
- **Cody is no longer a competitor.** `sourcegraph/cody-public-snapshot` was archived 2025-08-01; Sourcegraph closed-sourced Cody and discontinued Free/Pro/Enterprise Starter plans on 2025-07-23.
- **DeepWiki and GitNexus, commit0's stated inspirations, are still ahead.** DeepWiki is hosted by Cognition; OSS clone `AsyncFuncAI/deepwiki-open` (16k★) ships wiki + chat. GitNexus (36k★) ships a 6-phase pipeline, 16 MCP tools, confidence-scored impact analysis — but PolyForm Noncommercial.
- **MCP is now the standard for agent ↔ codebase integration.** goose, Cline, GitNexus, Continue all expose tools over MCP. commit0's bespoke HTTP/SSE blocks adoption from the Cline/goose/Cursor ecosystem.
- Aider's PageRank repo map is the single most-copied algorithm in the category; commit0 has no analogue — small effort to close.
- OpenHands' **event-sourced agent loop** (immutable EventLog + sandboxed Workspace) is the production-grade pattern; commit0's planned SubRunnerFactory lacks both abstractions.
- **Local-first code-aware embeddings is an open wedge.** Continue ships local transformers.js by default but recommends `voyage-code-3`; Aider has no embeddings; GitNexus runs WASM in-browser. commit0's Gemini-only choice is out of step.
- **No surveyed project ships a CPG with data-flow + reads/writes edges as agent context.** commit0 already has `data_flow`, `reads`, `writes` in its `EdgeKind` set — genuinely unique and underused.
- **Single-binary Go is rare.** Every surveyed project needs a Python/Node/Rust/Docker runtime; commit0's `curl | sh` story is differentiated for enterprise.
- **Top recommendations** (§6): (1) PageRank ranker on the graph; (2) MCP server adapter; (3) `voyage-code-3` + Ollama embedder adapters; (4) OpenHands-style agent loop; (5) defer Echo→Gin until after the above.

## 2. Methodology

**Scope filter.** "Full agentic OSS that index a codebase and drive an LLM" — operationally: open-source repos, actively maintained (commits in 2026), with both (a) a non-trivial codebase representation richer than naive grep + (b) an LLM-driven user-facing loop. Pure infrastructure (tree-sitter, Joern, Semgrep, CodeQL, Glean, Stack Graphs, SCIP, LSIF) was excluded by the user's clarification. IDE autocomplete-only tools were excluded.

**Selection signals (multi-source triangulation).**

1. **GitHub stars + recent activity** via `gh api repos/<owner>/<name>` on 2026-05-07. Repos with no commits in 2025–2026 were dropped.
2. **2024–2026 mention frequency.** WebSearch over Hacker News, conference talks, podcasts, and "alternative-to-X" repo descriptions. Specifically searched: `WebSearch` queries on Cline architecture, OpenHands runtime, Continue indexing, goose MCP, DeepWiki, GitNexus.
3. **DeepWiki / GitNexus are non-negotiable** — commit0's own `docs/ARCHITECTURE.md:23` cites them as inspirations, so the survey must give an honest read on whether they're still ahead.
4. **Source verification.** For every architectural claim, fetched the README, docs site, or representative source file (e.g., Aider's `repomap.py`).

**Candidates considered, dropped, and why.**

| Candidate | Status | Reason |
|---|---|---|
| Aider (`Aider-AI/aider`, 44k★) | **Kept** | Canonical PageRank repo map; commit0's most-cited prior art. |
| Cline (`cline/cline`, 61k★) | **Kept** | Largest agent runtime by stars; explicit "agentic search instead of RAG" stance. |
| OpenHands (`OpenHands/OpenHands`, 73k★) | **Kept** | Production-grade event-sourced agent SDK; ICLR 2025 paper. |
| Continue (`continuedev/continue`, 33k★) | **Kept** | Reference implementation of "embeddings + tree-sitter + IDE" — closest stack to commit0. |
| goose (`aaif-goose/goose`, 44k★) | **Kept** | Linux Foundation AAIF anchor; deepest MCP integration. Replaces Cody in slot 5. |
| Sourcegraph Cody | **Dropped** | `cody-public-snapshot` archived 2025-08-01; closed-sourced. Mentioned for historical context only. |
| DeepWiki (Cognition, hosted) + `AsyncFuncAI/deepwiki-open` (16k★) | **Inspirations check-in §4.5** | Hosted product not OSS; OSS clone exists but it's a wiki generator, not an agent loop. |
| GitNexus (`abhigyanpatwari/GitNexus`, 36k★) | **Inspirations check-in §4.5** | Non-commercial license; closest direct competitor architecturally. |
| Tabby (33k★) | Dropped | Code-completion product; not an agent loop. |
| Bloop (9.5k★) | Dropped | **Archived** 2024-12-04. |
| SWE-agent (19k★) | Dropped | Research benchmark harness; closer to a CLI scaffolding than a daily-driver. |
| morph-labs / morph.so | Dropped | No active OSS surface found via `gh search` on 2026-05-07. |

## 3. Top 5 deep dives

### 3.1 Aider — PageRank repo map as canonical prior art

**Repo / governance.** [`Aider-AI/aider`](https://github.com/Aider-AI/aider), 44,428★ / 4,364 forks, last push 2026-04-25. Started by Paul Gauthier solo, now community-maintained under the Aider-AI org. No VC. Apache-2.0. Single-maintainer-plus-volunteers — fragile but persistent.

**Agent loop.** Not a plan-and-execute agent; an **edit-loop**. User message → repo map injected → LLM responds with `SEARCH/REPLACE` blocks → Aider applies them → tests run if configured → next turn. No sub-agent delegation. Streaming, one LLM call per turn.

**Code graph model.** A NetworkX `MultiDiGraph` where **nodes are filenames** and **edges are identifier dependencies** (file A defines `foo`, file B references `foo` → edge B→A). Tags are extracted by tree-sitter via per-language `*-tags.scm` query files; each tag is a `Tag` namedtuple with `name`, `kind ∈ {def, ref}`, and line. (`aider/repomap.py` — `get_tags_raw`, `get_ranked_tags` — confirmed by reading the file.)

**Context selection.** This is the core innovation. `get_ranked_tags()` runs `nx.pagerank()` on the weighted graph with a **personalization vector** biasing files in chat (50x), mentioned identifiers (10x), well-named camelCase identifiers ≥8 chars (10x), and damping `_underscore` and over-defined names (0.1x). Reference frequency uses `math.sqrt(num_refs)` to prevent runaway dominance. The output is a token-budgeted ranked list of definitions rendered as elided code views, controlled by `--map-tokens` (default 1024).

**LLM/embedding integration.** Aider supports OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, Ollama, and via LiteLLM, ~100 providers. **No embeddings are used at all.** Context selection is purely graph + lexical. Prompt caching is enabled for Anthropic/DeepSeek where supported.

**Indexing strategy.** Lazy + incremental. The repo map is rebuilt on the fly per request (with tag caching on disk under `.aider.tags.cache.v3/`). No persistent vector store. Scales to ~10k files comfortably; multi-100k files becomes slow because PageRank is global.

**Tool surface.** Minimal — file edit (search/replace blocks), `git` integration (auto-commit per turn), `/run`, `/test`, `/lint`. No browser, no shell-as-agent-tool. Aider is opinionated: edit-and-test, nothing else.

**Memory model.** Per-session chat history. The repo map is the persistent "memory" — recomputed each turn from current files + chat state. No episodic / cross-session memory.

**Multi-language.** Anything with a tree-sitter `*-tags.scm`. Currently 30+ languages including Go, Python, TS/JS, Rust, Java, C/C++, Ruby, PHP, Swift. Adding a language = writing tree-sitter tag queries (~1 day of work).

**Self-hosted. Honest weaknesses (open issues 2026):** Python packaging fragility (recurring `Uncaught ImportError`/`ModuleNotFoundError` traces); `console.py` UnicodeEncodeError on Windows; PageRank has scaling cliffs at large repos. No agent loop, no web tools.

**Sources:** see §7.

### 3.2 Cline — Agentic search, MCP-first, no embeddings

**Repo / governance.** [`cline/cline`](https://github.com/cline/cline), 61,440★ / 6,367 forks, last push 2026-05-06. cline.bot Inc., VC-backed. Apache-2.0. Aggressive release cadence; "Cline SDK migration" is the current churn.

**Agent loop.** Multi-step ReAct-style with explicit `<thinking>` + tool-use blocks. No formal sub-agent delegation but supports parallel tool invocation. Streaming. Default turn limit configurable; users routinely hit it on large refactors. Approval mode = manual / auto-approve list.

**Code graph model.** **None.** This is the strategically interesting choice. Cline maintainers explicitly moved away from RAG — they argue that "agentic search" (give the LLM `read_file`, `search_files`, `list_files`, `list_code_definition_names`) plus a long context window outperforms maintained embedding indexes. The "graph" is whatever the LLM constructs in-conversation by chaining tool calls.

**Context selection.** Tool-driven. Cline imposes a 300KB/file hard limit and elides large files. Its `list_code_definition_names` tool runs tree-sitter to surface top-level symbols cheaply. There's no precomputed semantic index by default; users can attach external context via MCP servers.

**LLM/embedding integration.** Anthropic, OpenAI, Gemini, Bedrock, OpenRouter, Ollama, LM Studio. Supports prompt caching for Anthropic. No embedding model is needed because there's no vector store.

**Indexing strategy.** Effectively none — relies on long context + agentic exploration. Pro: zero index maintenance, always fresh. Con: every new conversation re-explores; no cross-session learning of "where things live."

**Tool surface.** Rich — `read_file`, `write_to_file`, `replace_in_file`, `execute_command` (shell), `browser_action` (Puppeteer-driven web), `search_files`, `list_files`, `list_code_definition_names`, `use_mcp_tool`, `access_mcp_resource`. **First-class MCP** with a curated marketplace launched Feb 2025 (v3.4).

**Memory model.** Per-task chat history; "Memory Bank" pattern (user-maintained markdown files committed to repo) for cross-session persistence — convention, not infrastructure.

**Multi-language.** Language-agnostic for the LLM; `list_code_definition_names` uses tree-sitter for ~10 languages.

**Self-hosted IDE plugin.** Free OSS; cline.bot sells managed accounts. Honest weaknesses (open issues May 2026): SDK migration broke login flows ("Unable to login to staging environment"), task restart bugs, Bedrock Opus 4.7 `temperature` deprecation. The "agentic search" thesis breaks down on 1M-LoC monorepos where exploration eats turns.

**Sources:** see §7.

### 3.3 OpenHands — Event-sourced production agent SDK

**Repo / governance.** [`OpenHands/OpenHands`](https://github.com/OpenHands/OpenHands), 72,764★ / 9,215 forks, last push 2026-05-07. Renamed from OpenDevin in 2024. All Hands AI Inc. (YC-backed). MIT. ICLR 2025 paper; SDK paper at arXiv:2511.03690 (Nov 2025).

**Agent loop.** Strict event-sourced architecture. The core is `User Message → Agent → LLM → Action → Runtime (sandbox) → Observation → Agent`, with **all interactions appended as immutable events to an `EventLog`**. V1 SDK (2025) packages this as four packages: SDK / Tools / Workspace / Server. Stateless Agent emits Actions; Conversation runs the loop; Workspace (local process or Docker) executes. Sub-agent delegation is supported.

**Code graph model.** **None at the framework level.** OpenHands treats the codebase as a sandboxed filesystem. Any graph reasoning is the LLM's responsibility, optionally augmented by user-supplied MCP tools. This is intentional — they want "the agent" decoupled from "the codebase representation."

**Context selection.** Filesystem listing, file read, shell-driven grep/find. Optional `microagents/` directory in the repo provides scoped persistent instructions. Prompt caching aggressive.

**LLM/embedding integration.** LiteLLM-mediated — every major provider plus self-hosted. No embeddings.

**Indexing strategy.** None — runtime is stateless except for the EventLog. This is the bet: indexes go stale, agentic exploration in a sandbox is reproducible.

**Tool surface.** `execute_bash`, `str_replace_editor` (file edit), `browser` (Playwright), `finish`, `delegate_to_agent`, MCP tool plug-in. The Workspace abstraction (Docker / local / E2B / remote) is the cleanest in the survey.

**Memory model.** EventLog is the persistent state. Replayable — fault tolerance + reproducibility built in.

**Multi-language.** Language-agnostic; relies on shell tools the LLM invokes.

**Self-hosted (Docker) + OpenHands Cloud.** Honest weaknesses (issues May 2026): "Openhands Cloud cannot use custom provider", PATCH 500s on org settings during legacy `agent_kind='llm'` migration, MCP stdio env-block bugs from settings UI. Setup friction is real (Docker-in-Docker quirks). For codebase **understanding** — as opposed to task execution — OpenHands gives you no help; it's a runtime, not a graph.

**Sources:** see §7.

### 3.4 Continue — IDE-native embeddings + tree-sitter chunking

**Repo / governance.** [`continuedev/continue`](https://github.com/continuedev/continue), 33,008★ / 4,459 forks, last push 2026-05-06. Continue Dev Inc., VC-backed. Apache-2.0. Closest stack to commit0 (tree-sitter + embeddings + IDE plugin).

**Agent loop.** Chat / Edit / Agent modes. Agent mode is ReAct with tool use. Per-IDE host (VS Code + JetBrains). Streaming.

**Code graph model.** Hybrid. The `CodebaseIndexer` produces multiple parallel indexes: chunk embeddings, full-text BM25-ish keyword index, AST chunk hierarchy via tree-sitter, recently-edited "lance" index. There's no explicit symbol graph in the Aider sense — chunks are the unit, related by file proximity, not by call edges.

**Context selection.** `@codebase` retrieval = embedding-similarity over chunks + keyword retrieval, fused. RRF-like. Default top-k tunable. Branching on file vs symbol references is regex-driven.

**LLM/embedding integration.** Provider-agnostic via the Continue config schema. **Defaults to local embeddings via `transformers.js`** (`all-MiniLM-L6-v2`-class), stored in `~/.continue/index`. Recommends [`voyage-code-3`](https://docs.continue.dev/customize/model-roles/embeddings) for quality. LLMs: anything via LiteLLM-style routing — Ollama, OpenAI, Anthropic, Gemini, Bedrock.

**Indexing strategy.** Persistent local SQLite + LanceDB-style vector store. Batch size 200, `IndexLock` ensures single concurrent build, watches filesystem for changes (incremental). Scale: works to mid-six-figures of files; documented memory pressure at 1M+.

**Tool surface.** Code edit, run-terminal, run-tests, MCP tools, custom slash commands.

**Memory model.** Index is per-workspace persistent. Per-session chat history. No episodic memory.

**Multi-language.** Tree-sitter chunker: ~15 languages. Adding a language = adding a chunker definition.

**Self-hosted IDE plugin.** Honest weaknesses (issues): autocomplete intermittently fails to register server responses; embedding generation breaks in JetBrains while working in VS Code (#2289); `@codebase` retrieval misses relevant files at scale (#7072 — long discussion). The chunk-and-embed approach has accuracy ceilings; their own [blog post](https://blog.continue.dev/accuracy-limits-of-codebase-retrieval/) admits it.

**Sources:** see §7.

### 3.5 goose — MCP-native extensible agent (Linux Foundation)

**Repo / governance.** [`aaif-goose/goose`](https://github.com/aaif-goose/goose), 44,108★ / 4,524 forks, last push 2026-05-07. Originally Block (Square/Cash App). **Contributed to the Linux Foundation Agentic AI Foundation (AAIF) in December 2025** alongside MCP and AGENTS.md — strongest governance in the survey. Apache-2.0. Rust.

**Agent loop.** Three-layer: Interface (CLI / Desktop) → Agent core (reasoning loop, currently single-loop ReAct, `goose2` experimenting with planner / worker split) → Extensions (each = one MCP server). Streaming.

**Code graph model.** **None native.** Same philosophy as Cline / OpenHands — no built-in graph, the agent reaches code via MCP-supplied tools.

**Context selection.** Tool-driven exploration. Codebase awareness comes entirely from extensions (e.g., `developer` extension for shell + file tools; community MCP servers for git, GitHub, etc.).

**LLM/embedding integration.** Provider-agnostic via the Rust LLM client layer. No bundled embeddings. Excellent local-model UX (Ollama + LM Studio first-class).

**Indexing strategy.** None.

**Tool surface.** ~70 documented extensions; ecosystem of 3,000+ MCP servers. Goose's contribution is making MCP **the** way you extend an agent.

**Memory model.** Per-session conversation log; "Memory" extension stores notes across sessions in a local SQLite via MCP.

**Multi-language.** Language-agnostic.

**Self-hosted CLI/Desktop, free.** Honest weaknesses: agent reasoning quality depends entirely on the LLM (no graph anchors); large refactors hit turn limits; the Rust codebase is impressively clean but means contributing extensions in non-Rust requires the MCP boundary (which is the point, but the boundary is the floor on latency).

**Sources:** see §7.

## 4. Comparison matrix

| Dimension | Aider | Cline | OpenHands | Continue | goose |
|---|---|---|---|---|---|
| Stars (2026-05-07) | 44.4k | 61.4k | 72.8k | 33.0k | 44.1k |
| License | Apache-2.0 | Apache-2.0 | MIT | Apache-2.0 | Apache-2.0 |
| Language | Python | TS | Python | TS | Rust |
| Governance | Solo→community | VC | VC + ICLR paper | VC | **Linux Foundation AAIF** |
| Agent loop | Edit loop (no plan) | ReAct + MCP | Event-sourced SDK | ReAct (per-IDE) | ReAct + MCP layer |
| Sub-agent delegation | ❌ | ⚠️ parallel tools | ✅ `delegate_to_agent` | ❌ | ⚠️ via extensions |
| Code graph model | ✅ **file-graph + PageRank** | ❌ | ❌ | ⚠️ chunk hierarchy | ❌ |
| Edge types | symbol def/ref | N/A | N/A | proximity only | N/A |
| Context selection | personalized PageRank | agentic LLM tool calls | filesystem + LLM | embeddings RRF + keyword | LLM tool calls |
| Embeddings | ❌ | ❌ | ❌ | ✅ local default + cloud opt | ❌ |
| LLM providers | LiteLLM (~100) | ~10 + Bedrock | LiteLLM | many | many |
| Local-model UX | ✅ | ✅ | ✅ | ✅ best-in-class | ✅ first-class |
| Indexing | lazy, on-the-fly | none | none | persistent SQLite + vec | none |
| Tool surface | edit + git + run | edit + shell + browser + MCP | edit + bash + browser + MCP | edit + run + MCP | extensions = MCP servers |
| MCP support | ⚠️ via plugins | ✅ marketplace | ✅ | ✅ | ✅ **canonical adopter** |
| Memory | repo map per turn | "Memory Bank" convention | EventLog | per-workspace index | extension-based |
| Multi-lang | 30+ | ~10 def-list | language-agnostic | ~15 chunker | language-agnostic |
| Honest weakness | Python packaging; PageRank scale | exploration eats turns at scale | runtime-only; Docker setup | retrieval accuracy ceiling | reasoning quality = LLM quality |

### 4.5 Inspirations check-in: DeepWiki and GitNexus

**DeepWiki (Cognition, hosted) + `AsyncFuncAI/deepwiki-open` (16,139★, Python).** Hosted DeepWiki is a wiki + Q&A SaaS demo of Devin's internals (`github.com/x/y` → `deepwiki.com/x/y`); not OSS. The OSS clone reproduces the wiki surface — RAG-over-chunks + Mermaid diagrams via Gemini/OpenAI/Ollama embeddings. Verdict: the inspiration is still ahead on UX (auto-generated browsable wiki) but closed-source for the reasoning; the OSS clone is RAG-only, no graph. commit0 is positioned to **leapfrog `deepwiki-open`** by emitting a wiki from its existing richer graph.

**GitNexus (`abhigyanpatwari/GitNexus`, 36,438★, TypeScript).** **The most direct architectural competitor in the survey.** Six-phase pipeline (Structure → Parsing → Resolution → Clustering → Processes → Search), `CodeRelation` edges with confidence scores, 16 MCP tools (`query`, `context`, `impact`, `detect_changes`, `rename`, `cypher`, etc.), 13 languages, in-browser via WASM tree-sitter + LadybugDB-WASM. **License: PolyForm Noncommercial** — read for ideas, can't copy code. Verdict: **ahead on indexing breadth (clustering, process tracing), MCP, language count.** commit0's Go + SurrealDB stack is more enterprise-deployable, but agent surface is behind. AAIF + GitNexus + DeepWiki together set the bar.

## 5. commit0 positioning analysis

(Inputs: `docs/ARCHITECTURE.md`, `docs/BACKEND.md`, `pkg/types/ast.go`, `internal/domain/ports.go`, `internal/app/index_service.go`. Note: the current `feat/commit0-v0.0.2` branch does **not** yet contain the agent layer or Gin migration that the project-level `CLAUDE.md` describes — those are a future state. The analysis below uses the actual code.)

### Per-dimension comparison

| Dimension | commit0 today | vs surveyed |
|---|---|---|
| Code graph model | `CodeNode {file, function, class, module}` + 8 `EdgeKind` (`calls`, `imports`, `defines`, `inherits`, `uses`, `data_flow`, `reads`, `writes`) | **Richer than every project surveyed.** Aider has file-graph + def/ref only; GitNexus has CodeRelation + confidence scores but no equivalent of `data_flow`/`reads`/`writes`. |
| Storage | SurrealDB 3.0: graph + HNSW vector + BM25 FTS in one DB | Continue uses SQLite + vec; GitNexus uses LadybugDB; commit0's choice is unique and powerful. |
| Embeddings | Gemini Embedding 2 only (3072-d), with `EmbedInput`/`EmbedResult` ports | Behind Continue (multi-provider local default). Provider lock-in is a strategic risk. |
| LLM | Gemini 2.0 Flash via `LLMExplainer` port | One adapter. Aider/Continue/Cline/OpenHands/goose all multi-provider via LiteLLM-class abstractions. |
| Agent loop | None on this branch (planned: SubRunnerFactory) | Behind every surveyed project except `deepwiki-open`. |
| Context selection | RRF fusion of vector + FTS (`internal/app/fusion.go`) | On par with Continue. Behind Aider on graph-aware ranking — no PageRank. |
| Indexing | 4-stage pipeline: walk → parse → embed → store; `errgroup` bounded | Comparable to Continue. Has explicit re-embed-with-neighborhood pass (`ReembedNeighborhood`) — **uniquely good.** |
| Tool surface for agents | None yet — HTTP+SSE for the human UI, no MCP | Behind every surveyed project. |
| Multi-language | Go, Python, TS, JS via tree-sitter | Behind GitNexus (13), Continue (~15), Aider (30+). |
| Distribution | Single Go binary + SurrealDB | Differentiated. No competitor ships one binary. |
| Memory model | `SessionService` per-session; graph itself is persistent | On par; but no scratchpad / episodic across sessions. |

### Three things commit0 is **genuinely competitive** on

1. **Richest edge model in the survey.** `EdgeDataFlow` carries `param_name`/`arg_expr`/`arg_type`; `EdgeReads`/`EdgeWrites` carry qualified field paths. No surveyed project records function-to-field reads/writes. This is a **CPG (code property graph) edge set** that nothing else here ships, and the existing `Neighborhood` type already exposes it for embedding-context enrichment (`ContextBuilder`). Underexploited.
2. **Single-binary Go + SurrealDB deployment story.** Aider, Cline, Continue, OpenHands, goose, GitNexus, deepwiki-open — each requires a runtime (Python venv / Node / Docker / browser). commit0 is `curl | sh` + one DB. This is the wedge in regulated-enterprise land.
3. **`ReembedNeighborhood` pass.** Re-embedding a node *after* its graph neighbors are known so the embedding text contains caller/callee/data-flow context is a real architectural advantage — Continue and `deepwiki-open` re-chunk lexically, not graph-aware. This actually addresses Continue's documented "accuracy limits of codebase retrieval."

### Three things commit0 is **clearly behind** on

1. **No agent loop, no MCP server.** Every project that matters in 2026 either *is* an agent (Cline, OpenHands, goose) or exposes itself *to* agents via MCP (Continue, GitNexus). commit0 is a query/trace HTTP API consumed by a hand-written human chat UI. The `feat/commit0-v0.0.2` branch hasn't shipped the agent layer the docs promise.
2. **Provider lock-in.** Single embedder (Gemini), single LLM (Gemini). Continue, Aider, OpenHands all use LiteLLM-class routing. commit0's hexagonal architecture is well-positioned for this — the ports exist — but no second adapter has been written.
3. **Language coverage.** 4 languages vs GitNexus's 13. Each new language is a non-trivial tree-sitter adapter (parsing + extraction + edge resolution) — and the user's identifier rule ("full English words") suggests tree-sitter `*-tags.scm` is not the main bottleneck; cross-file resolution (the GitNexus phase 3) is.

### Unique angle no one is covering well

**Multi-modal graph embeddings.** ARCHITECTURE.md's stated wedge — "code, comments, diagrams, and natural-language queries into the **same vector space**" via Gemini Embedding 2 — is genuinely novel. No surveyed project embeds repository diagrams (Mermaid, architecture PNGs in `docs/`) into the same space as code. DeepWiki *generates* diagrams; commit0 could *index* them. This is a cleaner story than re-implementing PageRank: "every diagram in your repo is searchable from a function" is a feature nobody else ships. The current `EmbedInput` already has `Images [][]byte` and `ImageMIMEs` fields — the plumbing exists, the integration doesn't.

## 6. Recommendations

Prioritised by impact ÷ effort.

| # | Recommendation | Inspired by | Effort | Concrete change |
|---|---|---|---|---|
| 1 | Personalized-PageRank ranker on the existing graph; chat mentions = personalization vector; token-budgeted output. | Aider | Small (~1 day) | `internal/app/pagerank.go`, called from `QueryService` between RRF and `ContextBuilder`. |
| 2 | Expose query/trace/blast/neighborhood as an MCP server. Lets Cline/goose/Cursor users use commit0 today. | goose, GitNexus, Continue | Small–Med (~3 days) | New `internal/adapters/mcp/`; reuses services. |
| 3 | Code-aware embedding adapters: `voyage-code-3` (Resty) + Ollama (`nomic-embed-code` / `bge-m3`). | Continue | Med (~5 days) | `internal/adapters/embed/{voyage,ollama}.go`; config switch. |
| 4 | Cross-file resolution phase with confidence scores; current call edges are intra-file. | GitNexus | Med (~1 week) | New extractor pass; add `EdgeMetadata.confidence`. |
| 5 | Agent loop with explicit `Workspace` + immutable `EventLog`; first tools = existing query/trace/blast. | OpenHands | Med–Large (~2 weeks) | New `internal/app/agent/`; supersedes the stub SubRunnerFactory plan. |
| 6 | Index repo diagrams (`docs/**/*.{png,jpg,svg,mmd}`) into the same vector space — `EmbedInput.Images` already exists. | Novel — none do this | Small (~2 days) | Extend `FileWalker` + `IndexService.Index`. |
| 7 | Add Rust + Java tree-sitter adapters; doubles language coverage. | GitNexus, Continue | Med (~1 week each) | `internal/adapters/treesitter/{rust,java}.go`. |
| 8 | `commit0 wiki <repo>` command emitting Markdown + Mermaid from the graph. | DeepWiki | Med (~3 days) | `cmd/wiki.go` + `internal/app/wiki_service.go`. |
| 9 | LiteLLM-style provider router for `LLMExplainer`; collapse per-provider adapters to config presets. | Aider, Continue, OpenHands | Med | Refactor `LLMExplainer` adapter to one Resty router. |
| 10 | Defer the planned Echo→Gin migration. Buys nothing on the strategic axes above. | — | Negative effort | Postpone until after #1 and #2 ship. |

## 7. Sources

- [Aider repository (Aider-AI/aider)](https://github.com/Aider-AI/aider)
- [Aider docs — Repository Map](https://aider.chat/docs/repomap.html)
- [Aider source — `aider/repomap.py`](https://github.com/Aider-AI/aider/blob/main/aider/repomap.py)
- [DeepWiki — Aider repository understanding](https://deepwiki.com/Aider-AI/aider/4-repository-understanding-and-context)
- [Cline repository (cline/cline)](https://github.com/cline/cline)
- [Latent Space podcast — Cline with Saoud Rizwan and Nik Pash](https://www.latent.space/p/cline)
- [cline.bot product page](https://cline.bot/)
- [Augment Code — Cline vs Intent](https://www.augmentcode.com/tools/intent-vs-cline)
- [OpenHands repository (OpenHands/OpenHands)](https://github.com/OpenHands/OpenHands)
- [OpenHands SDK paper (arXiv:2511.03690)](https://arxiv.org/abs/2511.03690)
- [OpenHands ICLR 2025 paper](https://proceedings.iclr.cc/paper_files/paper/2025/file/a4b6ad6b48850c0c331d1259fc66a69c-Paper-Conference.pdf)
- [OpenHands runtime README](https://github.com/OpenHands/OpenHands/blob/main/openhands/runtime/README.md)
- [Continue repository (continuedev/continue)](https://github.com/continuedev/continue)
- [Continue docs — codebase context](https://docs.continue.dev/customize/context/codebase)
- [DeepWiki — Continue codebase indexing](https://deepwiki.com/continuedev/continue/3.4-codebase-indexing)
- [Continue blog — accuracy limits of codebase retrieval](https://blog.continue.dev/accuracy-limits-of-codebase-retrieval/)
- [Continue docs — embed model role](https://docs.continue.dev/customize/model-roles/embeddings)
- [goose repository (aaif-goose/goose)](https://github.com/aaif-goose/goose)
- [goose-docs](https://goose-docs.ai/)
- [Block — codename goose announcement](https://block.xyz/inside/block-open-source-introduces-codename-goose)
- [Linux Foundation — Agentic AI Foundation announcement](https://www.linuxfoundation.org/press/linux-foundation-announces-the-formation-of-the-agentic-ai-foundation)
- [Sourcegraph Cody public snapshot (archived 2025-08-01)](https://github.com/sourcegraph/cody-public-snapshot)
- [Sourcegraph — open sourcing Cody (2023, historical)](https://sourcegraph.com/blog/open-sourcing-cody)
- [DeepWiki by Cognition — blog](https://cognition.ai/blog/deepwiki)
- [DeepWiki-Open repository (AsyncFuncAI/deepwiki-open)](https://github.com/AsyncFuncAI/deepwiki-open)
- [GitNexus repository (abhigyanpatwari/GitNexus)](https://github.com/abhigyanpatwari/GitNexus)
- [MarkTechPost — Meet GitNexus](https://www.marktechpost.com/2026/04/24/meet-gitnexus-an-open-source-mcp-native-knowledge-graph-engine-that-gives-claude-code-and-cursor-full-codebase-structural-awareness/)
- [SitePoint — Client-side RAG with GitNexus](https://www.sitepoint.com/client-side-rag-building-knowledge-graphs-in-the-browser-with-gitnexus/)
- [Bloop repository (archived 2024-12-04)](https://github.com/BloopAI/bloop)
- [Tabby repository (TabbyML/tabby)](https://github.com/TabbyML/tabby)
- [SWE-agent repository](https://github.com/SWE-agent/SWE-agent)
- [Voyage AI — voyage-code-3](https://docs.voyageai.com/docs/embeddings)
