# syntax=docker/dockerfile:1

# AVALANCHEGO_NODE_IMAGE must point to an avalanchego image (e.g., avaplatform/avalanchego:v1.14.0)
ARG AVALANCHEGO_NODE_IMAGE="avaplatform/avalanchego:v1.14.0"

# ============= Build Stage ================
FROM golang:1.24.9-bookworm AS builder

# Install build dependencies for CGO
RUN apt-get update && apt-get install -y \
    gcc \
    libc-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy indexing-subnet-evm source
COPY indexing-subnet-evm/ ./indexing-subnet-evm/
# Copy evm-ingestion source (required dependency)
COPY evm-ingestion/ ./evm-ingestion/

# Build the plugin from repo root
RUN CGO_ENABLED=1 go build -o /subnet-evm-plugin ./indexing-subnet-evm

# ============= Runtime Stage ================
FROM $AVALANCHEGO_NODE_IMAGE AS runtime

# subnet-evm canonical VM ID
ENV CANONICAL_VM_ID=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy

# Copy plugin and entrypoint
COPY --from=builder /subnet-evm-plugin /avalanchego/build/plugins/$CANONICAL_VM_ID
COPY --from=builder /build/indexing-subnet-evm/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

