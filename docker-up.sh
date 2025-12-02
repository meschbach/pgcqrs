#!/bin/bash

#!/bin/bash

set -xe

go test ./pkg/... ./internal/...
go build ./cmd/pgcqrs

mkdir -p build

export CGO_ENABLED=0
export GOOS=linux
archs="amd64 arm64"
for arch in $archs; do
  echo $arch
  GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o build/migrator_$arch ./cmd/migrator
  GOARCH=$arch go build -ldflags='-w -s -extldflags "-static"' -o build/service_$arch ./cmd/service
done

docker-compose up --build