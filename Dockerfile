# syntax=docker/dockerfile:1

# ── Build arguments ───────────────────────────────────────────────────────────
ARG GO_VERSION=1.26

# ── Builder stage ─────────────────────────────────────────────────────────────
# Always runs on the CI host's native platform (linux/amd64) so that Go and the
# C toolchain execute at native speed.  Cross-compilation is done via CC and
# GOOS/GOARCH rather than QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS builder

# Cross-compilers for both directions:
#   amd64 host → arm64 target : aarch64-linux-gnu-gcc
#   arm64 host → amd64 target : x86_64-linux-gnu-gcc  (Mac M-series via Docker Desktop)
# Both packages are available on all Debian architectures.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
    gcc-aarch64-linux-gnu \
    gcc-x86-64-linux-gnu \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Restore module cache as a separate layer so it is only invalidated when
# go.mod / go.sum change, not on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build-time metadata injected via -ldflags.
ARG VERSION=dev
ARG COMMIT=none

# TARGETOS / TARGETARCH are set automatically by Docker buildx for each
# platform in the build matrix.
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN set -eux; \
    case "$TARGETARCH" in \
      arm64) CC=aarch64-linux-gnu-gcc ;; \
      amd64) CC=x86_64-linux-gnu-gcc ;; \
      *)     CC=gcc ;; \
    esac; \
    export CC; \
    CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/commit0 .

# ── Runtime stage ─────────────────────────────────────────────────────────────
# distroless/cc-debian12 carries glibc + libgcc — the only shared libraries
# that smacker/go-tree-sitter's CGO binary links against at runtime.
FROM gcr.io/distroless/cc-debian12

COPY --from=builder /out/commit0 /commit0

# Run as the distroless built-in non-root user (uid 65532).
USER nonroot

EXPOSE 8080

ENTRYPOINT ["/commit0"]
