#!/usr/bin/env bash
set -exu

cd "$(dirname "$0")/../.."

AVALANCHEGO_VERSION="${AVALANCHEGO_VERSION:-v1.14.0}"
SUBNET_EVM_VERSION="${SUBNET_EVM_VERSION:-v0.8.0}"
VM_ID="${VM_ID:-srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy}"
IMAGE_REPO="${IMAGE_REPO:-containerman17/indexing-subnet-evm}"
IMAGE_TAG="${IMAGE_TAG:-${SUBNET_EVM_VERSION}_${AVALANCHEGO_VERSION}}"

docker build \
    --build-arg AVALANCHEGO_NODE_IMAGE="avaplatform/avalanchego:${AVALANCHEGO_VERSION}" \
    --build-arg VM_ID="${VM_ID}" \
    -f cmd/subnet-evm-plugin/Dockerfile \
    -t "${IMAGE_REPO}:${IMAGE_TAG}" \
    -t "${IMAGE_REPO}:latest" \
    .

echo "Built: ${IMAGE_REPO}:${IMAGE_TAG}"
echo ""
echo "Push with:"
echo "  docker push ${IMAGE_REPO}:${IMAGE_TAG}"
echo "  docker push ${IMAGE_REPO}:latest"

