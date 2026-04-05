BINARY   := commit0
PKG      := github.com/commit0-dev/commit0
VERSION  ?= dev
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# CGO is required for smacker/go-tree-sitter.
export CGO_ENABLED := 1

.PHONY: all build run clean \
        fmt vet lint \
        test test-cover test-race \
        install-hooks uninstall-hooks hooks-run \
        help

all: build

# ── Build ──────────────────────────────────────────────────────────────────
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) coverage.out coverage.html
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

## test-cover: run coverage for internal/app and enforce 98 % threshold
test-cover:
	@go test -count=1 -timeout=5m \
		-coverprofile=coverage.out \
		-covermode=atomic \
		-coverpkg=./internal/app/... \
		./internal/app/...
	@COVERAGE=$$(go tool cover -func=coverage.out \
		| grep -E "^total:" | awk '{print $$3}' | tr -d '%'); \
	echo "Coverage: $${COVERAGE}%"; \
	awk "BEGIN{exit !($${COVERAGE} < 98)}" || \
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

# ── Help ───────────────────────────────────────────────────────────────────
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'
