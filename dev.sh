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
  # Unit Testing
  #
  echo
  echo "Unit Testing"
  echo
  go test -count 1 ./pkg/... ./internal/...

  #
  # Storage integration tests (testcontainers - self-contained Postgres)
  #
  cmd_storage_integ

  #
  # Integration Testing (env-var-gated Postgres)
  #
  echo
  echo "Integration Testing"
  echo
  (
  export PGCQRS_STORAGE_POSTGRES_URL=integ_tests:integ-tests-password@localhost:16003/integ_db?sslmode=disable
  export PGCQRS_INTEG_POSTGRES_URL=postgres:password1234@localhost:16003/postgres?sslmode=disable
  go run ./cmd/migrator primary
  go test -timeout 10s -count 1 ./pkg/... ./internal/...
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
  cmd_examples
  cmd_integration

  #
  # Reattach logs
  #
  docker-compose --file "$self_dir/docker-compose.yaml" --project-name pgcqrs logs --follow
}

########################################################################################################################
# Services
########################################################################################################################
function cmd_services() {
  #
  # Rebuild and restart services only
  #
  echo
  echo "Building services"
  echo
  TARGET_OS=linux ./release.sh

  echo
  echo "Restarting containers"
  echo
  $self_dir/deploy/docker-compose/dependencies.sh up
  docker-compose --file "$self_dir/docker-compose.yaml" --project-name pgcqrs up --remove-orphans --build --detach
}

########################################################################################################################
# Examples
########################################################################################################################
function cmd_examples() {
  #
  # Run example drift detection only
  #
  (cd $self_dir
  export PGCQRS_SERVICE_URL_HTTP=http://localhost:26000
  export PGCQRS_SERVICE_URL_GRPC=localhost:26001
  export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:16001"
  export OTEL_EXPORTER=grpc

  echo
  echo "Running examples to detect API drift"
  echo
  ./run-examples.sh
  )
}

########################################################################################################################
# Integration
########################################################################################################################
function cmd_integration() {
  #
  # Run transport verification only
  #
  echo
  echo "Running integration tests across all transports"
  echo
  ./integration-tests.sh docked
}

########################################################################################################################
# Storage Integration (testcontainers)
########################################################################################################################
function cmd_storage_integ() {
  echo
  echo "Running storage integration tests (testcontainers)"
  echo
  go test -tags testcontainers_pg -timeout 30s -count 1 -v ./internal/service/storage/...
}

########################################################################################################################
# System Tests
########################################################################################################################
function cmd_system_tests() {
  cmd_examples
  cmd_integration
}

########################################################################################################################
# DB Rollback Integration
########################################################################################################################
function cmd_db_rollback_integ() {
  #
  # Rollback integration database by one migration
  #
  $self_dir/deploy/docker-compose/platform.sh up

  echo
  echo "Rolling back integration database"
  echo
  (
  export PGCQRS_STORAGE_POSTGRES_URL=integ_tests:integ-tests-password@localhost:16003/integ_db?sslmode=disable
  go run ./cmd/migrator down
  )
}

########################################################################################################################
# DB Rollback Dev
########################################################################################################################
function cmd_db_rollback_dev() {
  #
  # Rollback dev database by one migration
  #
  $self_dir/deploy/docker-compose/dependencies.sh up

  echo
  echo "Rolling back dev database"
  echo
  (
  export PGCQRS_STORAGE_POSTGRES_URL=pgcqrs:pgcqrs-password@localhost:26113/pgcqrs_pg18?sslmode=disable
  go run ./cmd/migrator down
  )
}

########################################################################################################################
# DB Version Integration
########################################################################################################################
function cmd_db_integ_version() {
  #
  # Report current migration version of integration database
  #
  $self_dir/deploy/docker-compose/platform.sh up

  echo
  echo "Integration database migration version"
  echo
  (
  export PGCQRS_STORAGE_POSTGRES_URL=integ_tests:integ-tests-password@localhost:16003/integ_db?sslmode=disable
  go run ./cmd/migrator version
  )
}

########################################################################################################################
# DB Version Dev
########################################################################################################################
function cmd_db_dev_version() {
  #
  # Report current migration version of dev database
  #
  $self_dir/deploy/docker-compose/dependencies.sh up

  echo
  echo "Dev database migration version"
  echo
  (
  export PGCQRS_STORAGE_POSTGRES_URL=pgcqrs:pgcqrs-password@localhost:26113/pgcqrs_pg18?sslmode=disable
  go run ./cmd/migrator version
  )
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
  echo "  up                - containerize then runs the project"
  echo "  services          - rebuild and restart containers only"
  echo "  storage-integ     - run storage integration tests (testcontainers)"
  echo "  examples          - run example drift detection"
  echo "  integration       - run transport verification tests"
  echo "  system_tests      - runs examples + integration tests"
  echo "  db-rollback-integ - rollback integration database by one migration"
  echo "  db-rollback-dev   - rollback dev database by one migration"
  echo "  db-integ-version  - report current migration version of integration database"
  echo "  db-dev-version    - report current migration version of dev database"
}

case "$cmd" in
  "")
    summary_help
    ;;
  up)
    cmd_up
    ;;
  services)
    cmd_services
    ;;
  examples)
    cmd_examples
    ;;
  integration)
    cmd_integration
    ;;
  storage-integ)
    cmd_storage_integ
    ;;
  system_tests)
    cmd_system_tests
    ;;
  db-rollback-integ)
    cmd_db_rollback_integ
    ;;
  db-rollback-dev)
    cmd_db_rollback_dev
    ;;
  db-integ-version)
    cmd_db_integ_version
    ;;
  db-dev-version)
    cmd_db_dev_version
    ;;
  *)
    cmd_unknown_help
    ;;
esac
