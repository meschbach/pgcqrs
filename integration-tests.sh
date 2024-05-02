#!/bin/bash

set -xe
./service serve &
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT
# TODO: better wait mechanism to ensure the service has started.
sleep 1

export PGCQRS_TEST_TRANSPORT="memory"
export PGCQRS_TEST_URL="http://localhost:9000"
export PGCQRS_TEST_APP_BASE="systest-"
go test -count=1 --timeout 5s ./systest/...

export PGCQRS_TEST_URL="http://localhost:9000"
export PGCQRS_TEST_APP_BASE="systest-"
go test -count=1 --timeout 5s ./systest/...

#export PGCQRS_TEST_TRANSPORT="grpc"
#export PGCQRS_TEST_URL="localhost:9001"
#export PGCQRS_TEST_APP_BASE="systest-"
#go test -count=1 --timeout 5s ./systest/...
