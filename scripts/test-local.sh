#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "== go vet =="
go vet ./...

echo "== go test -race =="
go test -race ./...

echo "== docker image =="
IMAGE="${IMAGE:-salad-bzminer-worker:test}"
docker build -t "$IMAGE" .

echo "== integration =="
IMAGE="$IMAGE" ./tests/integration/test-image.sh
