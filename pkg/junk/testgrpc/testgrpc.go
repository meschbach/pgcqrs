package testgrpc

import (
	"context"
	"github.com/meschbach/go-junk-bucket/pkg/stdgrpc/buffernet"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"testing"
)

func InternalGRPConnection(t *testing.T, ctx context.Context, exportService func(server *grpc.Server)) *grpc.ClientConn {
	server := grpc.NewServer()
	exportService(server)

	grpcTransport := buffernet.New()
	_, onDoneListening := grpcTransport.ListenAsync(server)
	t.Cleanup(onDoneListening)

	wireClient, err := grpcTransport.Connect(ctx)
	require.NoError(t, err)
	return wireClient
}
