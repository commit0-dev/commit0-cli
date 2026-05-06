BINARY_SERVER := commit0
PKG           := github.com/commit0-dev/commit0
VERSION       ?= dev
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS       := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all build build-server run clean \
        fmt vet lint \
        test test-cover test-race \
        install-hooks uninstall-hooks hooks-run \
        ext-build ext-package \
        help

all: build

# ── Build ──────────────────────────────────────────────────────────────────
## build: build the commit0 server binary (requires CGO for tree-sitter)
build: build-server

## build-server: build the commit0 server binary (requires CGO for tree-sitter)
build-server:
	CGO_ENABLED=1 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY_SERVER) ./server

run: build-server
	./bin/$(BINARY_SERVER) serve

clean:
	rm -f bin/$(BINARY_SERVER) coverage.out coverage.html
	rm -rf dist/

# ── Code quality ───────────────────────────────────────────────────────────
fmt:
	gofmt -w ./...

vet:
	go vet ./...

## lint: run golangci-lint (same config and tool as CI and pre-push hook)
lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

# ── Tests ──────────────────────────────────────────────────────────────────
test:
	go test -count=1 -timeout=5m ./...

test-race:
	go test -race -count=1 -timeout=5m ./...

## test-cover: run coverage for server/internal/app and enforce 98 % threshold
test-cover:
	@go test -count=1 -timeout=5m \
		-coverprofile=coverage.out \
		-covermode=atomic \
		-coverpkg=./server/internal/app/... \
		./server/internal/app/...
	@COVERAGE=$$(go tool cover -func=coverage.out \
		| grep -E "^total:" | awk '{print $$3}' | tr -d '%'); \
	echo "Coverage: $${COVERAGE}%"; \
	awk "BEGIN{exit ($${COVERAGE} < 98)}" || \
		{ echo "FAIL: coverage $${COVERAGE}% < 98%"; exit 1; }; \
	echo "PASS: $${COVERAGE}% >= 98%"

cover-html: test-cover
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html 2>/dev/null || xdg-open coverage.html

# ── Git hooks ─────────────────────────────────────────────────────────────
## install-hooks: install pre-commit and pre-push hooks (requires pre-commit)
install-hooks:
	@command -v pre-commit >/dev/null 2>&1 || \
		{ echo "pre-commit not found. Install: pip install pre-commit"; exit 1; }
	@command -v golangci-lint >/dev/null 2>&1 || \
		{ echo "golangci-lint not found. Install: brew install golangci-lint"; exit 1; }
	pre-commit install
	pre-commit install --hook-type pre-push
	@echo "Hooks installed: pre-commit (fmt, vet) and pre-push (golangci-lint)"

uninstall-hooks:
	pre-commit uninstall
	pre-commit uninstall --hook-type pre-push

## hooks-run: run all hooks against every file right now (dry-run check)
hooks-run:
	pre-commit run --all-files
	pre-commit run golangci-lint --hook-stage pre-push --all-files

# ── VSCode Extension ───────────────────────────────────────────────────────
## ext-build: compile the VSCode extension
ext-build:
	cd vscode-extension && npm run compile

## ext-package: package the VSCode extension as .vsix
ext-package:
	cd vscode-extension && npx @vscode/vsce package

# ── Help ───────────────────────────────────────────────────────────────────
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'
