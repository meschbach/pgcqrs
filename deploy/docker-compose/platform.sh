#!/bin/bash

self_path=$(realpath $0)
self_dir=$(dirname $self_path)
cmd="$1" ; shift || true

########################################################################################################################
# Up
########################################################################################################################
function cmd_up() {
  network_count=$(docker network ls |grep pgcqrs_integration |wc -l)
  if [ $network_count = 1 ]; then
    version=$(docker network inspect 'pgcqrs_integration' --format='{{json .Labels}}' |jq -r '.["com.meschbach/version"]')
    if [ $version = 1 ]; then
      cmd_down
    fi
  fi
  (cd $self_dir
  docker-compose --file "$self_dir/platform.yaml" --project-name pgcqrs_platform up --detach --remove-orphans
  )
}

########################################################################################################################
# Down
########################################################################################################################
function cmd_down() {
  (cd $self_dir
  docker-compose --file "$self_dir/platform.yaml" --project-name pgcqrs_platform down --remove-orphans
  docker volume rm pgcqrs_platform_pgcqrs_integration_pg_data || echo "WARNING: pgcqrs_platform_pgcqrs_integration_pg_data was not removed"
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
  echo "  up - starts the compose project and migrates to the latest version"
  echo "  down - stops the project and reclaims the resources"
}

case "$cmd" in
  up)
    cmd_up
    ;;
  down)
    cmd_down
    ;;
  *)
    cmd_unknown_help
    ;;
esac
