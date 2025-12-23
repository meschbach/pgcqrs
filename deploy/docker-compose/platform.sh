#!/bin/bash

self_path=$(realpath $0)
self_dir=$(dirname $self_path)
cmd="$1" ; shift || true

########################################################################################################################
# Up
########################################################################################################################
function cmd_up() {
  (cd $self_dir
  docker-compose --file "$self_dir/platform.yaml" --project-name pgcqrs_platform up --detach --remove-orphans
  )
}

########################################################################################################################
# Down
########################################################################################################################
function cmd_down() {
  (cd $self_dir
  docker-compose --file "$self_dir/platform.yaml" --project-name pgcqrs_platform down
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
    cmd_remove
    ;;
  *)
    cmd_unknown_help
    ;;
esac
