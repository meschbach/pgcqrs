#!/bin/bash

set -xe

MODE="${1:-local}"
INTEGRATION_RUN_COUNT="${INTEGRATION_RUN_COUNT:-1}"
SERVER_LOG="/tmp/pgcqrs-server.log"

dump_server_goroutines() {
    local pid=$1
    echo "=== Dumping server goroutines (PID $pid) ==="
    kill -3 "$pid" 2>/dev/null || true
    sleep 0.5
    if [ -f "$SERVER_LOG" ]; then
        cat "$SERVER_LOG"
    fi
    echo "=== End server goroutine dump ==="
}

case "$MODE" in
  local)
    echo "=== Starting local service ==="
    ./service serve 2>"$SERVER_LOG" &
    SERVER_PID=$!
    function cleanup() {
        dump_server_goroutines "$SERVER_PID" 2>/dev/null || true
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    }
    trap cleanup EXIT
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
go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout 5s ./systest/...

echo "=== HTTP transport tests ==="
export PGCQRS_TEST_URL="$HTTP_URL"
export PGCQRS_TEST_APP_BASE="systest-"
unset PGCQRS_TEST_TRANSPORT
go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout 5s ./systest/...

echo "=== gRPC transport tests ==="
export PGCQRS_TEST_TRANSPORT="grpc"
export PGCQRS_TEST_URL="$GRPC_URL"
export PGCQRS_TEST_APP_BASE="systest-"
if ! go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout 5s ./systest/...; then
    dump_server_goroutines "$SERVER_PID"
    exit 1
fi
