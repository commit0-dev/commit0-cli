# commit0

[![CI](https://github.com/commit0-dev/commit0/actions/workflows/ci.yml/badge.svg)](https://github.com/commit0-dev/commit0/actions/workflows/ci.yml)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](https://go.dev/dl/)
[![Release](https://img.shields.io/github/v/release/commit0-dev/commit0)](https://github.com/commit0-dev/commit0/releases/latest)

> Graph-based source code analyzer. Index any codebase, then ask questions, trace call flows, and measure blast radius — all in plain English.

commit0 parses your source code into a knowledge graph where every function, class, and file is a node and every call, import, and inheritance is a typed edge. Each node carries a dense multi-modal embedding (Gemini Embedding 2) that places code, comments, and natural-language queries in the same vector space. A single static binary, no Python runtime required.

```
commit0 index ./my-project
commit0 query  "where does JWT validation happen?"
commit0 trace  api.Handler.ServeHTTP
commit0 blast  UserService.Create
```

---

## Installation

### Pre-built binary (Linux · macOS · amd64 · arm64)

Download the latest archive from [Releases](https://github.com/commit0-dev/commit0/releases/latest), extract, and place `commit0` on your `PATH`.

```sh
# macOS arm64 example
curl -fsSL https://github.com/commit0-dev/commit0/releases/latest/download/commit0_latest_darwin_arm64.tar.gz \
  | tar -xz -C /usr/local/bin commit0
```

### Container image

```sh
docker pull ghcr.io/commit0-dev/commit0:latest
docker run --rm -e GEMINI_API_KEY=... ghcr.io/commit0-dev/commit0:latest --help
```

Platforms: `linux/amd64` · `linux/arm64`

### Build from source

Requires Go 1.26+ and `CGO_ENABLED=1` (tree-sitter uses CGO).

```sh
git clone https://github.com/commit0-dev/commit0.git
cd commit0
make build          # outputs ./commit0
```

---

## Prerequisites

| Requirement | Notes |
|---|---|
| **SurrealDB 3.0** | `commit0 db start` can manage a local instance |
| **`GEMINI_API_KEY`** | Google AI Studio → [Get a key](https://aistudio.google.com/app/apikey) |

---

## Quick Start

```sh
# 1. Start a local SurrealDB instance (skip if you have one running)
commit0 db start

# 2. Index a project
commit0 index ./my-project

# 3. Ask a question
commit0 query "where is rate limiting applied?"

# 4. Trace a call chain from a symbol
commit0 trace api.Handler.ServeHTTP

# 5. Find everything that a function change would affect
commit0 blast UserService.Create

# 6. Start the HTTP API (JSON + SSE streaming)
commit0 serve
```

---

## Commands

| Command | Description |
|---|---|
| `index <path>` | Walk, parse, embed, and store a codebase |
| `query "<question>"` | Hybrid vector + full-text search with AI explanation |
| `trace <symbol>` | Forward call-chain traversal from a symbol |
| `blast <symbol>` | Reverse transitive impact analysis |
| `serve` | HTTP API on `:8080` (JSON responses, SSE streaming for explanations) |
| `repo list\|add\|rm` | Manage indexed repositories |
| `db start\|stop` | Lifecycle management for a local SurrealDB instance |

---

## Configuration

All settings are controlled by environment variables. A JSON config file can be passed via `--config`.

| Variable | Default | Description |
|---|---|---|
| `EMBED_PROVIDER` | `gemini` | Embedding provider: `gemini`, `voyage`, `ollama` |
| `LLM_PROVIDER` | `gemini` | LLM provider: `gemini`, `openrouter`, `ollama` |
| `EMBED_DIM` | `1024` | Normalized embedding dimension for HNSW |
| `SURREAL_URL` | `ws://localhost:8000` | SurrealDB WebSocket URL |
| `SURREAL_USER` | `root` | SurrealDB username |
| `SURREAL_PASS` | `root` | SurrealDB password |
| `GEMINI_API_KEY` | — | Google Gemini API key (if using Gemini provider) |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama server URL (if using Ollama provider) |
| `SERVER_PORT` | `8080` | HTTP server port |

---

## Supported Languages

Go · Python · TypeScript · JavaScript

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — design principles, hexagonal architecture, tech stack
- [Backend](docs/BACKEND.md) — services, port interfaces, HTTP API, agent orchestration
- [Database](docs/DATABASE.md) — SurrealDB schema, indexes, query patterns
- [OpenCodeGraph](docs/OPEN_CODE_GRAPH.md) — unified graph abstraction, analysis techniques
- [Directory Layout](docs/LAYOUT.md) — annotated file tree

---

## Contributing

```sh
make install-hooks  # install pre-commit (fmt/vet) and pre-push (lint) hooks
make test           # run all tests
make lint           # golangci-lint
```

Pull requests welcome. Please open an issue first for significant changes.
