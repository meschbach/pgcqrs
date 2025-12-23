#!/bin/bash

set -e

cmd="$1" ; shift || true
self_path=$(realpath $0)
self_dir=$(dirname $self_path)

########################################################################################################################
# Up
########################################################################################################################
function cmd_up() {
  #
  # Bring up development platform
  #
  $self_dir/deploy/docker-compose/platform.sh up

  #
  # Unit + Integration Testing
  #
  echo
  echo "Unit Testing"
  echo
  go test -count 1 ./pkg/... ./internal/...

  echo
  echo "Integration Testing"
  echo
  (
  export PGCQRS_STORAGE_POSTGRES_URL=integ_tests:integ-tests-password@localhost:16003/integ_db?sslmode=disable
  go run ./cmd/migrator primary
  go test -count 1 ./pkg/... ./internal/... -- -integration
  )

  #
  # Launch containers
  #
  TARGET_OS=linux ./release.sh
  $self_dir/deploy/docker-compose/dependencies.sh up
  docker-compose --file "$self_dir/docker-compose.yaml" --project-name pgcqrs up --remove-orphans --build --detach

  #
  # System Tests
  #
  (cd $self_dir
  export PGCQRS_SERVICE_URL_HTTP=http://localhost:26000
  export PGCQRS_SERVICE_URL_GRPC=localhost:26001
  export OTEL_EXPORTER_OTLP_ENDPOINT: "http://localhost:16001"
  export OTEL_EXPORTER: grpc
  ./run-examples.sh
  )

  #
  # Reattach logs
  #
  docker-compose --file "$self_dir/docker-compose.yaml" --project-name pgcqrs logs --follow
}

########################################################################################################################
# Command Processing
########################################################################################################################
function cmd_unknown_help() {
    echo "$cmd is an unknown subcommand"
    summary_help
}

function summary_help() {
  echo "$0 <sub-command>"
  echo "Where <sub-command> is one of:"
  echo "  up - containerize then runs the project"
}

case "$cmd" in
  "")
    summary_help
    ;;
  up)
    cmd_up
    ;;
  *)
    cmd_unknown_help
    ;;
esac
