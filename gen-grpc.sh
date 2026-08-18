#!/bin/zsh

protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    ./pkg/ipc/query.proto \
    ./pkg/indexer/views/grpc/views.proto
git add pkg/ipc/query.pb.go pkg/indexer/views/grpc/views.pb.go pkg/indexer/views/grpc/views_grpc.pb.go
