# commit0-cli

The command-line client for the [commit0](https://github.com/commit0-dev/commit0) server.

Thin HTTP client — no CGO, cross-compiles trivially.

## Install

```bash
go install github.com/commit0-dev/commit0-cli@latest
```

## Usage

```bash
# Point at your running commit0 server
export COMMIT0_SERVER_URL=http://localhost:8080

commit0-cli query "where is JWT validation?" --repo owner/repo
commit0-cli trace handleAgentChat --repo owner/repo --direction forward
commit0-cli blast QueryService.Query --repo owner/repo
commit0-cli index https://github.com/owner/repo.git
commit0-cli analyze --repo owner/repo --focus all
```

## Development

This repo is split from [commit0-dev/commit0](https://github.com/commit0-dev/commit0).
The `feature` branch tracks active development.
