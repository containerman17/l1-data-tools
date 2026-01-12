#!/usr/bin/env bash
set -eu

cd "$(dirname "$0")/../../.."

IMAGE_REPO="${IMAGE_REPO:-containerman17/ingestion/evm/rpc}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

PUSH_FLAG=""
[[ "${1:-}" == "--push" ]] && PUSH_FLAG="--push"

docker buildx build $PUSH_FLAG \
    -f ingestion/evm/rpc/Dockerfile \
    -t "${IMAGE_REPO}:${IMAGE_TAG}" \
    .

echo "Built: ${IMAGE_REPO}:${IMAGE_TAG}"
