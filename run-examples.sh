#!/bin/bash

function run_all() {
    local skip_watch=$1

    go run ./examples/simple
    go run ./examples/bykind
    go run ./examples/query
    go run ./examples/queryInt
    go run ./examples/queryBatch
    go run ./examples/query2
    if [ "$skip_watch" != "no_watch" ]; then
        go run ./examples/watch
    fi
    go run ./examples/readme
}

set -xe
export PGCQRS_SERVICE_TRANSPORT="memory"
run_all

export PGCQRS_SERVICE_TRANSPORT="http"
run_all no_watch

#export PGCQRS_SERVICE_URL="localhost:9001"
#export PGCQRS_SERVICE_TRANSPORT="grpc"
#go run ./examples/simple
#go run ./examples/bykind
#go run ./examples/query
#go run ./examples/queryInt
#go run ./examples/queryBatch
#go run ./examples/query2
#go run ./examples/watch
