#!/bin/sh
set -e

if [ -z "$GRPC_INDEXER_CHAIN_ID" ]; then
    echo "ERROR: GRPC_INDEXER_CHAIN_ID is required"
    exit 1
fi

PLUGINS_DIR="/avalanchego/build/plugins"
CANONICAL_VM_ID="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"

# Copy plugin to VM_ID if specified
if [ -n "$VM_ID" ] && [ "$VM_ID" != "$CANONICAL_VM_ID" ]; then
    cp "$PLUGINS_DIR/$CANONICAL_VM_ID" "$PLUGINS_DIR/$VM_ID"
    echo "Copied plugin to $VM_ID"
fi

exec /avalanchego/build/avalanchego "$@"

