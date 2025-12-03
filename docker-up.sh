#!/bin/bash

#!/bin/bash

set -xe

go test ./pkg/... ./internal/...
go build ./cmd/pgcqrs

TARGET_OS=linux ./release.sh

docker-compose up --build