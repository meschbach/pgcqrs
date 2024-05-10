#!/bin/bash

set -xe


compile() {
    local name=$1
    local arch=$2
    local cmd=$3
    CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o $name $cmd
}

compile_and_archive() {
  local name=$1
  local arch=$2
  local cmd=$3
  compile $name $arch $cmd
  tar czvf $name_$arch.tgz $name_$arch
}

for arch in amd64 arm64
do
  for os in linux darwin
  do
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o service ./cmd/service
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o pgcqrs ./cmd/pgcqrs
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o migrator ./cmd/migrator
    tar zcvf pgcqrs_$os_$arch.tgz service pgcqrs migrator migrations/
  done
done
