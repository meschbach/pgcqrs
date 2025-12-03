#!/bin/bash

set -e

: ${TARGET_ARCHS:="amd64 arm64"}
: ${TARGET_OS:="linux darwin"}

function compile() {
  local name=$1 ;shift
  local arch=$1 ; shift
  local os=$1 ; shift
  local output="cmd/$name/${arch}_${os}"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o "$output" "./cmd/$name"
  cp "$output" "build/${arch}_${os}/$name"
}

for arch in ${TARGET_ARCHS}
do
  for os in ${TARGET_OS}
  do
    mkdir -p "build/${arch}_${os}"
    compile service "$arch" "$os"
    compile pgcqrs "$arch" "$os"
    compile migrator "$arch" "$os"
    (cd "build/${arch}_${os}"
    tar zcvf "../pgcqrs_${arch}_${os}.tgz" service pgcqrs migrator
    )
  done
done
