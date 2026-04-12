# commit0

[![CI](https://github.com/commit0-dev/commit0/actions/workflows/ci.yml/badge.svg)](https://github.com/commit0-dev/commit0/actions/workflows/ci.yml)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](https://go.dev/dl/)
[![Release](https://img.shields.io/github/v/release/commit0-dev/commit0)](https://github.com/commit0-dev/commit0/releases/latest)

Source code analyzer built on a graph database. Parses codebases into a knowledge graph of functions, classes, files, and their relationships (calls, imports, data flow). Supports natural-language queries, call-chain tracing, and impact analysis.

```
commit0 index ./my-project
commit0 query  "where does JWT validation happen?"
commit0 trace  api.Handler.ServeHTTP
commit0 blast  UserService.Create
```

---

## Installation

### Pre-built binary (Linux / macOS, amd64 / arm64)

Download from [Releases](https://github.com/commit0-dev/commit0/releases/latest):

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

Platforms: `linux/amd64`, `linux/arm64`

### Build from source

Requires Go 1.26+ with `CGO_ENABLED=1` (tree-sitter dependency).

```sh
git clone https://github.com/commit0-dev/commit0.git
cd commit0
make build
```

---

## Requirements

| Dependency | Notes |
|---|---|
| **SurrealDB 3.0** | `commit0 db start` manages a local instance |
| **Embedding provider** | Gemini (API key required), Voyage AI, or Ollama (local, no key) |

---

## Quick Start

```sh
# Start a local SurrealDB instance
commit0 db start

# Start the server
commit0 serve &

# Index a project
commit0-cli index ./my-project

# Query the graph
commit0-cli query "where is rate limiting applied?"

# Trace a call chain
commit0-cli trace api.Handler.ServeHTTP

# Measure impact of a change
commit0-cli blast UserService.Create
```

---

## Commands

| Command | Description |
|---|---|
| `index <path>` | Parse, embed, and store a codebase |
| `query "<question>"` | Hybrid vector + full-text search with optional LLM explanation |
| `trace <symbol>` | Forward or reverse call-chain traversal |
| `blast <symbol>` | Reverse transitive impact analysis |
| `flow <symbol>` | Field-level data flow tracing |
| `find-root <symbol>` | Root cause analysis across git history |
| `serve` | Start the HTTP API server |
| `repo list\|add\|rm` | Manage indexed repositories |
| `db start\|stop` | Manage a local SurrealDB instance |

---

## Configuration

Settings are read from environment variables. A `.env` file in the working directory is loaded automatically.

| Variable | Default | Description |
|---|---|---|
| `EMBED_PROVIDER` | `gemini` | Embedding provider: `gemini`, `voyage`, `ollama` |
| `LLM_PROVIDER` | `gemini` | LLM provider: `gemini`, `openrouter`, `ollama` |
| `EMBED_DIM` | `1024` | Embedding dimension for HNSW vector indexes |
| `SURREAL_URL` | `ws://localhost:8000` | SurrealDB WebSocket URL |
| `SURREAL_USER` | `root` | SurrealDB username |
| `SURREAL_PASS` | `root` | SurrealDB password |
| `GEMINI_API_KEY` | | Required when using Gemini provider |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama server URL |
| `SERVER_PORT` | `8080` | HTTP server listen port |

See [docs/BACKEND.md](docs/BACKEND.md#8-configuration) for the full configuration reference.

---

## Supported Languages

Go, Python, TypeScript, JavaScript

---

## Documentation

| Document | Contents |
|----------|----------|
| [Architecture](docs/ARCHITECTURE.md) | System design, hexagonal layers, technology choices |
| [Backend](docs/BACKEND.md) | Services, port interfaces, HTTP API, agent, configuration |
| [Database](docs/DATABASE.md) | SurrealDB schema, vector indexes, traversal patterns |
| [OpenCodeGraph](docs/OPEN_CODE_GRAPH.md) | Graph abstraction, analysis techniques, edge resolution |
| [Layout](docs/LAYOUT.md) | Annotated directory tree |

---

## Contributing

```sh
make install-hooks  # pre-commit (fmt/vet) and pre-push (lint) hooks
make test           # run all tests
make lint           # golangci-lint
```

Please open an issue before submitting large changes.
