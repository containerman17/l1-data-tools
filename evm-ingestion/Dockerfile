# syntax=docker/dockerfile:1

# ============= Build Stage ================
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy evm-ingestion source
COPY evm-ingestion/ ./evm-ingestion/

# Build with cache mounts
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /evm-ingestion ./evm-ingestion

# ============= Runtime Stage ================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /evm-ingestion /evm-ingestion

ENTRYPOINT ["/evm-ingestion"]

