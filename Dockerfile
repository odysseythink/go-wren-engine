# Source of truth: docker/Dockerfile
# This file exists for compatibility with build tools that expect Dockerfile at repo root.
# Please edit docker/Dockerfile and keep this file in sync.

# syntax=docker/dockerfile:1

# ── Builder stage ───────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

WORKDIR /build

# Install build toolchain for CGo
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Cache go modules in a separate layer
COPY go.mod go.sum ./
RUN go mod download

# Build the binary with CGo enabled (required for go-duckdb static lib)
COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -ldflags='-s -w' -o wren-server ./cmd/wren-server

# ── Runtime stage ───────────────────────────────────────────────
FROM debian:bookworm-slim

# Install parity packages from Java image
RUN apt-get update && apt-get install -y --no-install-recommends \
    postgresql-client \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Mirror Java image workdir
WORKDIR /usr/src/app

# Copy binary and entrypoint
COPY --from=builder /build/wren-server ./
COPY --chmod=755 docker/entrypoint.sh ./

# Java image does NOT declare EXPOSE; keep parity
# No USER directive — Java image runs as root

# Pass-through heap args: $1=binary $2=maxHeap $3=minHeap
CMD ["./entrypoint.sh", "wren-server", "512m", "64m"]
