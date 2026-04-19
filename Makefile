BINARY  := commit0-cli
PKG     := github.com/commit0-dev/commit0-cli
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all build install run clean fmt vet lint lint-fix test test-race help

all: build

# ── Build ──────────────────────────────────────────────────────────────────
## build: compile commit0-cli (pure Go, no CGO required)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) .

## install: install commit0-cli to GOPATH/bin
install:
	CGO_ENABLED=0 go install -trimpath -ldflags="$(LDFLAGS)" .

run: build
	./bin/$(BINARY)

clean:
	rm -f bin/$(BINARY) coverage.out

# ── Code quality ───────────────────────────────────────────────────────────
fmt:
	gofmt -w ./...

vet:
	go vet ./...

## lint: run golangci-lint
lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

# ── Tests ──────────────────────────────────────────────────────────────────
## test: run all tests
test:
	go test -count=1 -timeout=5m ./...

test-race:
	go test -race -count=1 -timeout=5m ./...

# ── Help ───────────────────────────────────────────────────────────────────
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'
