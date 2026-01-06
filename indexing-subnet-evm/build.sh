#!/usr/bin/env bash
set -eu

cd "$(dirname "$0")/.."

AVALANCHEGO_VERSION="${AVALANCHEGO_VERSION:-v1.14.0}"
SUBNET_EVM_VERSION="${SUBNET_EVM_VERSION:-v0.8.0}"
IMAGE_REPO="${IMAGE_REPO:-containerman17/indexing-subnet-evm}"
IMAGE_TAG="${SUBNET_EVM_VERSION}_${AVALANCHEGO_VERSION}"

PUSH_FLAG=""
[[ "${1:-}" == "--push" ]] && PUSH_FLAG="--push"

docker buildx build $PUSH_FLAG \
    --build-arg AVALANCHEGO_NODE_IMAGE="avaplatform/avalanchego:${AVALANCHEGO_VERSION}" \
    -f indexing-subnet-evm/Dockerfile \
    -t "${IMAGE_REPO}:${IMAGE_TAG}" \
    -t "${IMAGE_REPO}:latest" \
    .

echo "Built: ${IMAGE_REPO}:${IMAGE_TAG}"
