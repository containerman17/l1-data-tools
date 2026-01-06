#!/usr/bin/env bash
set -exu

cd "$(dirname "$0")"

# VM ID for subnet-evm on mainnet/fuji
CANONICAL_VM_ID="${CANONICAL_VM_ID:-srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy}"

# Build destination
PLUGINS_DIR="${HOME}/.avalanchego/plugins"

# Ensure plugins directory exists
mkdir -p "$PLUGINS_DIR"

# Build the plugin
echo "Building subnet-evm-plugin..."
go build -o "$PLUGINS_DIR/$CANONICAL_VM_ID" .

echo "Built: $PLUGINS_DIR/$CANONICAL_VM_ID"

