package views

import (
	"context"
	"fmt"
	"time"

	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RemoteReader is a Reader backed by the ViewProjectionConsumer gRPC service.
// All reads, including UntilVersion waits, are resolved server-side.
type RemoteReader struct {
	client vgrpc.ViewProjectionConsumerClient
	id     ProjectionIdentity
}

// NewRemoteReader creates a Reader backed by the ViewProjectionConsumer gRPC service.
func NewRemoteReader(conn *grpc.ClientConn, id ProjectionIdentity) *RemoteReader {
	return &RemoteReader{
		client: vgrpc.NewViewProjectionConsumerClient(conn),
		id:     id,
	}
}

// Get retrieves an entity by kind and key from the remote consumer service.
func (r *RemoteReader) Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, Status, error) {
	req := &vgrpc.GetEntityRequest{
		Projection: r.id.Projection,
		Domain:     r.id.Domain,
		Stream:     r.id.Stream,
		Consumer:   r.id.ConsumerName,
		Kind:       kind,
		Key:        key.Parts(),
	}

	cfg := &getOptions{}
	for _, opt := range opts {
		opt.applyGet(cfg)
	}

	switch {
	case cfg.untilVersion != nil:
		req.VersionConstraint = &vgrpc.GetEntityRequest_UntilVersion{
			UntilVersion: &vgrpc.UntilVersionConstraint{
				Version:   *cfg.untilVersion,
				TimeoutMs: cfg.untilTimeout.Milliseconds(),
			},
		}
	case cfg.afterVersion != nil:
		req.VersionConstraint = &vgrpc.GetEntityRequest_After{
			After: &vgrpc.AfterConstraint{Version: *cfg.afterVersion},
		}
	}

	resp, err := r.client.GetEntity(ctx, req)
	if err != nil {
		if status.Code(err) == codes.Canceled || status.Code(err) == codes.DeadlineExceeded {
			return nil, 0, ctx.Err()
		}
		return nil, 0, fmt.Errorf("get entity: %w", err)
	}

	result, err := statusFromProto(resp.Status)
	if err != nil {
		return nil, 0, err
	}
	return entityFromResponse(resp), result, nil
}

// Version returns the current projection version from the remote consumer service.
func (r *RemoteReader) Version(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := r.client.Version(ctx, &vgrpc.VersionRequest{
		Projection: r.id.Projection,
		Domain:     r.id.Domain,
		Stream:     r.id.Stream,
		Consumer:   r.id.ConsumerName,
	})
	if err != nil {
		return 0, fmt.Errorf("get version: %w", err)
	}
	return resp.Version, nil
}

// entityFromResponse converts a wire entity to a views entity, returning nil for absent entities.
func entityFromResponse(resp *vgrpc.GetEntityResponse) *Entity {
	if resp.Entity == nil {
		return nil
	}
	return &Entity{
		Kind:    resp.Entity.Kind,
		Key:     NewKey(resp.Entity.Key...),
		Value:   resp.Entity.Value,
		Version: resp.Entity.Version,
	}
}

// statusFromProto maps the wire GetStatus enum to the domain Status.
func statusFromProto(s vgrpc.GetStatus) (Status, error) {
	switch s {
	case vgrpc.GetStatus_GET_STATUS_OK:
		return StatusOK, nil
	case vgrpc.GetStatus_GET_STATUS_NOT_FOUND:
		return StatusNotFound, nil
	case vgrpc.GetStatus_GET_STATUS_STALE:
		return StatusStale, nil
	case vgrpc.GetStatus_GET_STATUS_TIMEOUT:
		return StatusTimeout, nil
	default:
		return 0, fmt.Errorf("unknown get status: %s", s)
	}
}
