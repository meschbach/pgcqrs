#!/bin/bash


set -e
function run_all() {
    local skip_watch=$1

    set -x
    go run ./examples/simple
    go run ./examples/bykind
    go run ./examples/query
    go run ./examples/queryInt
    go run ./examples/queryBatch
    go run ./examples/query2
    set +x
    if [ "$skip_watch" != "no_watch" ]; then
      set -x
      go run ./examples/watch
      set +x
    fi
    set -x
    go run ./examples/readme
    set +x
}

echo
echo "Running examples with HTTP"
echo
export PGCQRS_SERVICE_TRANSPORT="memory"
export ENV="system_test.memory"
run_all

echo
echo "Running examples with HTTP"
echo
export PGCQRS_SERVICE_TRANSPORT="http"
export ENV="system_test.http"
(
  export PGCQRS_SERVICE_URL=$PGCQRS_SERVICE_URL_HTTP
  run_all no_watch
)

echo
echo "Running examples with gRPC"
echo
(
: "${PGCQRS_SERVICE_URL_GRPC:=localhost:9001}"
export PGCQRS_SERVICE_URL="$PGCQRS_SERVICE_URL_GRPC"
export PGCQRS_SERVICE_TRANSPORT="grpc"
run_all no_watch
)

echo
echo "Success!"
echo