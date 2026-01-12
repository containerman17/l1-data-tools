# syntax=docker/dockerfile:1

# ============= Build Stage ================
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy ingestion/evm/rpc source
COPY ingestion/evm/rpc/ ./ingestion/evm/rpc/

# Build with cache mounts
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /ingestion/evm/rpc ./ingestion/evm/rpc

# ============= Runtime Stage ================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /ingestion/evm/rpc /ingestion/evm/rpc

ENTRYPOINT ["/ingestion/evm/rpc"]

