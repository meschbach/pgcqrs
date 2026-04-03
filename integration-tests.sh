#!/bin/bash

set -e

MODE="${1:-local}"

case "$MODE" in
  local)
    echo "=== Starting local service ==="
    ./service serve &
    function cleanup() {
        kill %1
    }
    trap cleanup SIGINT SIGTERM EXIT
    sleep 1
    HTTP_URL="http://localhost:9000"
    GRPC_URL="localhost:9001"
    ;;
  docked)
    echo "=== Using docked service ==="
    HTTP_URL="http://localhost:26000"
    GRPC_URL="localhost:26001"
    ;;
  *)
    echo "Unknown mode: $MODE"
    echo "Usage: $0 [local|docked]"
    exit 1
    ;;
esac

echo "=== Memory transport tests ==="
export PGCQRS_TEST_TRANSPORT="memory"
export PGCQRS_TEST_URL="$HTTP_URL"
export PGCQRS_TEST_APP_BASE="systest-"
go test -count=1 --timeout 5s ./systest/...

echo "=== HTTP transport tests ==="
export PGCQRS_TEST_URL="$HTTP_URL"
export PGCQRS_TEST_APP_BASE="systest-"
unset PGCQRS_TEST_TRANSPORT
go test -count=1 --timeout 5s ./systest/...

echo "=== gRPC transport tests ==="
export PGCQRS_TEST_TRANSPORT="grpc"
export PGCQRS_TEST_URL="$GRPC_URL"
export PGCQRS_TEST_APP_BASE="systest-"
go test -count=1 --timeout 5s ./systest/...
