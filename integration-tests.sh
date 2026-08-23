#!/bin/bash

set -xe

MODE="${1:-local}"
INTEGRATION_RUN_COUNT="${INTEGRATION_RUN_COUNT:-1}"
SERVER_LOG="/tmp/pgcqrs-server.log"

dump_server_goroutines() {
    local pid=$1
    local dump_file="/tmp/pgcqrs-goroutines-$pid.txt"
    echo "=== Dumping server goroutines (PID $pid) to $dump_file ==="
    kill -3 "$pid" 2>/dev/null || true
    sleep 0.5
    if [ -f "$SERVER_LOG" ]; then
        cp "$SERVER_LOG" "$dump_file"
        cat "$dump_file"
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
TEST_OUTPUT="/tmp/pgcqrs-test-memory.out"
go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout 5s ./systest/... >"$TEST_OUTPUT" 2>&1
TEST_EXIT=$?
if [ $TEST_EXIT -ne 0 ]; then
    echo "=== MEMORY TRANSPORT FAILED ==="
    cat "$TEST_OUTPUT"
    exit $TEST_EXIT
fi
echo "=== Memory transport tests PASSED ==="

echo "=== HTTP transport tests ==="
export PGCQRS_TEST_URL="$HTTP_URL"
export PGCQRS_TEST_APP_BASE="systest-"
unset PGCQRS_TEST_TRANSPORT
TEST_OUTPUT="/tmp/pgcqrs-test-http.out"
go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout 5s ./systest/... >"$TEST_OUTPUT" 2>&1
TEST_EXIT=$?
if [ $TEST_EXIT -ne 0 ]; then
    echo "=== HTTP TRANSPORT FAILED ==="
    cat "$TEST_OUTPUT"
    exit $TEST_EXIT
fi
echo "=== HTTP transport tests PASSED ==="

echo "=== gRPC transport tests ==="
export PGCQRS_TEST_TRANSPORT="grpc"
export PGCQRS_TEST_URL="$GRPC_URL"
export PGCQRS_TEST_APP_BASE="systest-"
TEST_OUTPUT="/tmp/pgcqrs-test-grpc.out"
if ! go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout 5s ./systest/... >"$TEST_OUTPUT" 2>&1; then
    dump_server_goroutines "$SERVER_PID"
    cat "$TEST_OUTPUT"
    exit 1
fi
echo "=== gRPC transport tests PASSED ==="
