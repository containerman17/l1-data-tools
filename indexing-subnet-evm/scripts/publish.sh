#!/bin/bash

# Build and push the ingestion binary in one command
# TODO: CI
cd $(dirname $0)/..

docker buildx build --push -t containerman17/evm-ingestion-experiments:latest .