package views

import (
	"context"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"google.golang.org/grpc"
)

// newGRPCClient builds a projection client backed by the remote gRPC services.
func newGRPCClient(ctx context.Context, conn *grpc.ClientConn, transport v1.Transport, proj *Projection, opts []ClientOption) (ProjectionClient, error) {
	id := NewProjectionIdentity(proj.domain, proj.stream, proj.consumerName)
	parts := clientParts[*v1.KeepAlive]{
		wire:  v1.NewGrpcWire(conn),
		store: NewRemoteStore(conn, id),
		buildReader: func(*Notifier) Reader {
			return NewRemoteReader(conn, id)
		},
	}
	return newClient(ctx, transport, proj, parts, opts)
}
