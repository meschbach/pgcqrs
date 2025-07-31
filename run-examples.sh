#!/bin/bash

set -xe
export PGCQRS_SERVICE_TRANSPORT="memory"
go run ./examples/simple
go run ./examples/bykind
go run ./examples/query
go run ./examples/queryInt
go run ./examples/queryBatch
go run ./examples/query2
go run ./examples/watch

export PGCQRS_SERVICE_TRANSPORT="http"
go run ./examples/simple
go run ./examples/bykind
go run ./examples/query
go run ./examples/queryInt
go run ./examples/queryBatch

export PGCQRS_SERVICE_URL="localhost:9001"
export PGCQRS_SERVICE_TRANSPORT="grpc"
go run ./examples/simple
go run ./examples/bykind
go run ./examples/query
go run ./examples/queryInt
go run ./examples/queryBatch
go run ./examples/query2
go run ./examples/watch
