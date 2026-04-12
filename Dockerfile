# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26

# ── Builder stage ─────────────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

# Copy workspace definition and all module descriptors first for layer caching.
COPY go.work go.work.sum ./
COPY server/go.mod  server/go.sum  ./server/
COPY cli/go.mod     cli/go.sum     ./cli/
COPY pkg/types/go.mod              ./pkg/types/
COPY sdk/go.mod     sdk/go.sum     ./sdk/

RUN go mod download

# Copy full source.
COPY . .

ARG VERSION=dev
ARG COMMIT=none

# Build server binary (CGO for tree-sitter).
RUN CGO_ENABLED=1 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/commit0 ./server

# ── Runtime stage ─────────────────────────────────────────────────────────────
# distroless/cc-debian12 carries glibc + libgcc — the only shared libraries
# that smacker/go-tree-sitter's CGO binary links against at runtime.
FROM gcr.io/distroless/cc-debian12

COPY --from=builder /out/commit0 /commit0

USER nonroot

EXPOSE 8080

ENTRYPOINT ["/commit0"]
