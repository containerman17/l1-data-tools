# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download deps with cache mount
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source
COPY . .

# Build with cache mounts for module and build cache
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /sink ./cmd/sink

# Runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /sink /sink

ENTRYPOINT ["/sink"]
CMD ["-config", "/config.yaml"]

