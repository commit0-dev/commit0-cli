# commit0 — Local-First Code Intelligence Platform

> Zero-cost, air-gapped, privacy-first code intelligence.
> Single binary. No API keys. No cloud. No subscriptions.

---

## 1. Vision

commit0 becomes a **fully local code intelligence platform** powered by Gemma 4 running on the developer's machine. Every feature is zero-cost and works offline — the opposite of Cursor ($20/mo) and Windsurf ($15/mo).

**Deployment**: Single Go binary with embedded `llama.cpp` for local model inference. No Ollama, no Docker, no Python — one binary does everything. Downloads the model on first run.

```bash
# That's it. No API keys, no Docker, no setup.
commit0 serve
```

---

## 2. Architecture: All-in-One Binary

```
┌───────────────────────────────────────────────────────┐
│                    commit0 binary                      │
│                                                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────┐│
│  │  CLI      │  │  HTTP    │  │  VSCode Extension    ││
│  │  Commands │  │  Server  │  │  (separate install)  ││
│  └────┬─────┘  └────┬─────┘  └──────────┬───────────┘│
│       │              │                    │            │
│  ┌────▼──────────────▼────────────────────▼──────────┐│
│  │              SERVICE LAYER                         ││
│  │  Index │ Query │ Trace │ Blast │ Agent │ Review   ││
│  │  Docs  │ Security │ Watcher                       ││
│  └────────────────────┬──────────────────────────────┘│
│                       │                                │
│  ┌────────────────────▼──────────────────────────────┐│
│  │              MODEL ROUTER                          ││
│  │                                                    ││
│  │  ┌─────────────────┐  ┌────────────────────────┐  ││
│  │  │  LOCAL (default) │  │  CLOUD (optional)      │  ││
│  │  │                  │  │                         │  ││
│  │  │  llama.cpp       │  │  Gemini API             │  ││
│  │  │  embedded in     │  │  (if GEMINI_API_KEY     │  ││
│  │  │  the binary      │  │   is set)               │  ││
│  │  │                  │  │                         │  ││
│  │  │  Gemma 4 E4B     │  │  Gemini 2.5 Flash      │  ││
│  │  │  (auto-download) │  │  (higher accuracy)      │  ││
│  │  └─────────────────┘  └────────────────────────┘  ││
│  └────────────────────────────────────────────────────┘│
│                       │                                │
│  ┌────────────────────▼──────────────────────────────┐│
│  │  SurrealDB (embedded or external)                  ││
│  │  Code Graph + Vector Index + Full-Text Search      ││
│  └────────────────────────────────────────────────────┘│
└───────────────────────────────────────────────────────┘
```

---

## 3. Zero-Cost Features

### 3.1 Continuous Background Indexing

Watch the filesystem for changes, auto-re-index modified files in real-time. Like Copilot's workspace indexing but fully local.

**Why zero-cost matters**: Cloud indexing limits re-index frequency due to API costs. Local Gemma 4 = re-index on **every file save** for free.

```
On file change:
  1. Debounce 500ms (batch rapid saves)
  2. Parse changed file (tree-sitter)
  3. Diff nodes vs stored (content hash)
  4. Summarize new/changed functions (Gemma 4 — free)
  5. Re-embed changed nodes
  6. Update graph edges
  7. Notify VSCode extension (WebSocket)

Result: CodeLens + hover + graph links update within 2-3 seconds of saving.
```

**Implementation**: `internal/app/watcher_service.go` using `fsnotify`.

---

### 3.2 Local Code Review Agent

Analyze git diffs with Gemma 4 + the code graph. Identify bugs, security issues, blast radius, missing tests.

```bash
commit0 review                    # review staged changes
commit0 review HEAD~1             # review last commit
commit0 review --base main        # review branch vs main
commit0 hooks install             # auto-run on pre-commit
```

**How it works**:
1. Parse git diff → changed files + line ranges
2. Look up changed functions in code graph
3. Compute blast radius for each change
4. Feed to Gemma 4 agent with tools (search, blast, trace)
5. Agent synthesizes review with line references

**Implementation**: `cmd/review.go` + `internal/app/review_service.go`

---

### 3.3 Auto-Generated Documentation

Generate and continuously update project documentation from the code graph + Gemma 4 analysis.

```bash
commit0 docs generate   # generate full docs
commit0 docs serve       # serve as local website
commit0 docs watch       # auto-update on changes
```

**Output**: README, ARCHITECTURE, API reference, module docs, flow diagrams (Mermaid).

**Implementation**: `cmd/docs.go` + `internal/app/docs_service.go`

---

### 3.4 Security Vulnerability Scanner

Analyze code for vulnerabilities using the code graph's data flow edges. The graph tracks `reads`, `writes`, `data_flow` — revealing taint propagation paths that static analyzers miss.

```bash
commit0 security scan              # full scan
commit0 security scan --diff HEAD  # scan changes only
commit0 security watch             # continuous scanning
```

**Our unique advantage — graph-powered taint tracking**:
- `user_input → processInput → db.Query()` — data_flow edges reveal unsanitized paths
- Auth gap detection: trace from HTTP handlers backward — missing authMiddleware in caller chain
- Gemma 4 filters false positives and suggests fixes

**Implementation**: `cmd/security.go` + `internal/app/security_service.go` + SARIF output

---

## 4. Hardware Requirements

| Model | RAM | Disk | Speed | Best For |
|-------|-----|------|-------|----------|
| Gemma 4 E2B (Q4) | 2GB | 1.5GB | ~50 tok/s CPU | Summarization only |
| **Gemma 4 E4B (Q4)** | **4GB** | **2.8GB** | **~30 tok/s CPU** | **Full agent (default)** |
| Gemma 4 26B A4B (Q4) | 8GB | 14GB | ~15 tok/s GPU | Best quality |

---

## 5. Implementation Roadmap

| Phase | Feature | Dependencies |
|-------|---------|-------------|
| **1** | Model Provider abstraction + llama.cpp embed | New port, CGO binding |
| **2** | Background watcher | fsnotify, incremental index |
| **3** | Code review agent | Git diff parser, existing agent tools |
| **4** | Auto documentation | Graph traversal, Mermaid rendering |
| **5** | Security scanner | Taint rules engine, data flow traversal |

---

## 6. Competitive Matrix

| Feature | Cursor | Windsurf | Copilot | Sourcegraph | **commit0** |
|---------|--------|----------|---------|-------------|-------------|
| Price | $20/mo | $15/mo | $10/mo | $49/mo | **$0** |
| Offline | No | No | No | No | **Yes** |
| Privacy | Cloud | Cloud | Cloud | Cloud | **Fully local** |
| Code graph | No | Partial | No | Yes | **Yes** |
| Code review | No | No | Yes | Yes | **Yes (free)** |
| Auto docs | No | No | No | No | **Yes** |
| Security scan | No | No | No | No | **Yes (graph)** |
| Background index | Yes | Yes | Yes | No | **Yes (free)** |
| Air-gapped | No | No | No | Self-host | **Yes** |
| Open source | No | No | No | Partial | **Yes** |
