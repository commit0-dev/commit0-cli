# CLAUDE.md

This file provides guidance to Claude Code when working in this repository.

## What This Repo Is

`commit0-cli` is the **standalone command-line client** for the commit0 server.
It is a pure Go module — no CGO, no server dependencies, cross-compiles trivially.

## Module

```
github.com/commit0-dev/commit0-cli
```

## Critical Rules

- `pkg/types/` — **zero** external imports, ever. Pure data types only.
- `sdk/` — only imports `pkg/types` and `resty.dev/v3`. No server internals.
- `cmd/` — only imports `sdk/` and `pkg/types`. Calls the server via HTTP.
- **Never** import the server repo (`github.com/commit0-dev/commit0/server/...`).
- **Never** add CGO. CLI must cross-compile without CGO.
- HTTP client: Resty v3 (`resty.dev/v3`). Never raw `net/http` for outbound calls.

## Structure

```
commit0-cli/
├── cmd/           # Cobra commands — one file per command
├── sdk/           # HTTP client — one file per API resource
├── pkg/
│   └── types/     # Shared types — zero external imports
└── main.go        # Entry point — sets version, calls cmd.Execute()
```

## Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.24+ (pure Go, no CGO) |
| CLI framework | Cobra + persistent flags |
| HTTP client | Resty v3 |
| Output | tablewriter, ANSI color helpers in color.go |

## Running the Server (required for all CLI commands)

**The commit0 server runs via Docker Compose in the server repo — not from this repo.**

```bash
# From the commit0 server repo:
cd ../commit0          # or wherever commit0/ lives
docker compose up -d   # starts SurrealDB + commit0 server

# Verify it's up before using the CLI:
curl http://localhost:8080/health
# → {"status":"ok","state":"idle","active_jobs":0}
```

## Commands

```bash
make build          # CGO_ENABLED=0 go build -o bin/commit0-cli .
make install        # install to $GOPATH/bin
make test           # go test -count=1 -timeout=5m ./...
make lint           # golangci-lint run
```

## Adding a New Command

1. Add a file `cmd/<name>.go` with `var <name>Cmd = &cobra.Command{...}`
2. Add the corresponding SDK method in `sdk/<resource>.go`
3. Register with `rootCmd.AddCommand(<name>Cmd)` in `init()`
4. No server-side changes needed — CLI only calls HTTP endpoints

## Server URL Resolution

```
--server-url flag  >  COMMIT0_SERVER_URL env  >  http://localhost:8080
```
