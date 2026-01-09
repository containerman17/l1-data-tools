#!/usr/bin/env bash
set -eu

./build.sh

source ./.env

docker run --rm -it \
  -e RPC_URL=$C_RPC_URL \
  -e CHAIN_ID=2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 \
  -e MAX_PARALLELISM=2000 \
  -e PEBBLE_PATH=/data \
  -v $(pwd)/data:/data \
  -p 9090:9090 \
  -p 9091:9091 \
  containerman17/evm-ingestion:latest