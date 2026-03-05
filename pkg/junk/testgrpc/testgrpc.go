// Package testgrpc provides gRPC testing helpers.
package testgrpc

import (
	"context"
	"testing"

	"github.com/meschbach/go-junk-bucket/pkg/stdgrpc/buffernet"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// InternalGRPConnection creates an in-memory gRPC connection for testing.
func InternalGRPConnection(ctx context.Context, t *testing.T, exportService func(server *grpc.Server)) *grpc.ClientConn {
	server := grpc.NewServer()
	exportService(server)

	grpcTransport := buffernet.New()
	_, onDoneListening := grpcTransport.ListenAsync(server)
	t.Cleanup(onDoneListening)

	wireClient, err := grpcTransport.Connect(ctx)
	require.NoError(t, err)
	return wireClient
}
