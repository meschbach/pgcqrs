#!/bin/bash

set -xe

go test ./pkg/... ./internal/...
go build ./cmd/migrator
go build ./cmd/service
go build ./cmd/pgcqrs
