// Package views provides gRPC handlers for the ViewProjectionConsumer service.
package views

import (
	"context"
	"encoding/json"
	"iter"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
)

// ConsumerHandler implements the ViewProjectionConsumer gRPC service.
type ConsumerHandler struct {
	vgrpc.UnimplementedViewProjectionConsumerServer
	store     *views.PGStore
	registry  *NotifierRegistry
	positions interface {
		GetPosition(ctx context.Context, domain, stream, consumer string) (int64, bool, error)
	}
}

// NewConsumerHandler creates a new ViewProjectionConsumer handler.
func NewConsumerHandler(store *views.PGStore, registry *NotifierRegistry, positions interface {
	GetPosition(ctx context.Context, domain, stream, consumer string) (int64, bool, error)
}) *ConsumerHandler {
	return &ConsumerHandler{
		store:     store,
		registry:  registry,
		positions: positions,
	}
}

// GetEntity retrieves a projected entity by kind and key.
func (h *ConsumerHandler) GetEntity(ctx context.Context, req *vgrpc.GetEntityRequest) (*vgrpc.GetEntityResponse, error) {
	id := views.NewProjectionIdentity(req.Domain, req.Stream, req.Projection)
	entity, err := h.store.Get(ctx, id, req.Kind, views.NewKey(req.Key...))
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return &vgrpc.GetEntityResponse{Status: 1}, nil // StatusNotFound
	}

	resp := &vgrpc.GetEntityResponse{
		Entity: &vgrpc.Entity{
			Kind:    entity.Kind,
			Key:     entity.Key.Parts(),
			Value:   entity.Value,
			Version: entity.Version,
		},
		Status: 0, // StatusOK
	}

	// Handle version constraints
	if constraint := req.GetUntilVersion(); constraint != nil {
		if entity.Version >= constraint.Version {
			return resp, nil
		}
		return h.waitForVersion(ctx, req, id, constraint, resp)
	}

	if constraint := req.GetAfter(); constraint != nil {
		if entity.Version < constraint.Version {
			resp.Status = 2 // StatusStale
		}
	}

	return resp, nil
}

func (h *ConsumerHandler) waitForVersion(ctx context.Context, req *vgrpc.GetEntityRequest, id views.ProjectionIdentity, constraint *vgrpc.UntilVersionConstraint, resp *vgrpc.GetEntityResponse) (*vgrpc.GetEntityResponse, error) {
	streamID, err := h.store.ResolveStreamID(ctx, req.Domain, req.Stream)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(constraint.TimeoutMs) * time.Millisecond
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ch := make(chan views.Change, 16)
	unsub := h.registry.OnChange(streamID, func(c views.Change) {
		select {
		case ch <- c:
		default:
		}
	})
	defer unsub()

	for {
		select {
		case <-timer.C:
			resp.Status = 3 // StatusTimeout
			return resp, nil
		case c := <-ch:
			if c.Version >= constraint.Version {
				return h.refetchEntity(ctx, id, req, resp)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (h *ConsumerHandler) refetchEntity(ctx context.Context, id views.ProjectionIdentity, req *vgrpc.GetEntityRequest, resp *vgrpc.GetEntityResponse) (*vgrpc.GetEntityResponse, error) {
	entity, err := h.store.Get(ctx, id, req.Kind, views.NewKey(req.Key...))
	if err != nil {
		return nil, err
	}
	if entity != nil {
		resp.Entity = &vgrpc.Entity{
			Kind:    entity.Kind,
			Key:     entity.Key.Parts(),
			Value:   entity.Value,
			Version: entity.Version,
		}
	}
	resp.Status = 0 // StatusOK
	return resp, nil
}

// GetVersion returns the current projection version.
func (h *ConsumerHandler) GetVersion(ctx context.Context, req *vgrpc.GetVersionRequest) (*vgrpc.GetVersionResponse, error) {
	id := views.NewProjectionIdentity(req.Domain, req.Stream, req.Projection)
	version, err := h.store.GetVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	return &vgrpc.GetVersionResponse{Version: version}, nil
}

// WatchChanges streams projection changes to the client.
func (h *ConsumerHandler) WatchChanges(req *vgrpc.WatchChangesRequest, stream vgrpc.ViewProjectionConsumer_WatchChangesServer) error {
	ctx := stream.Context()
	var afterVersion int64
	if constraint := req.GetAfterVersion(); constraint != nil {
		afterVersion = constraint.Version
	}

	streamID, err := h.store.ResolveStreamID(ctx, req.Domain, req.Stream)
	if err != nil {
		return err
	}

	for change, err := range h.changeStream(ctx, streamID, afterVersion) {
		if err != nil {
			return err
		}
		resp, err := h.buildWatchResponse(change)
		if err != nil {
			return err
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

func (h *ConsumerHandler) changeStream(ctx context.Context, streamID, afterVersion int64) iter.Seq2[views.Change, error] {
	return func(yield func(views.Change, error) bool) {
		ch := make(chan views.Change, 16)
		unsub := h.registry.OnChange(streamID, func(c views.Change) {
			select {
			case ch <- c:
			default:
			}
		})
		defer unsub()

		for {
			select {
			case <-ctx.Done():
				yield(views.Change{}, ctx.Err())
				return
			case c := <-ch:
				if c.Version <= afterVersion {
					continue
				}
				if !yield(c, nil) {
					return
				}
			}
		}
	}
}

func (h *ConsumerHandler) buildWatchResponse(c views.Change) (*vgrpc.WatchChangesResponse, error) {
	resp := &vgrpc.WatchChangesResponse{
		Version: c.Version,
	}
	for _, u := range c.Upserts {
		var data []byte
		if bytes, ok := u.Value.([]byte); ok {
			data = bytes
		} else {
			var err error
			data, err = json.Marshal(u.Value)
			if err != nil {
				return nil, err
			}
		}
		resp.Upserts = append(resp.Upserts, &vgrpc.Upsert{
			Kind:  u.Kind,
			Key:   u.Key.Parts(),
			Value: data,
		})
	}
	for _, d := range c.Deletes {
		resp.Deletes = append(resp.Deletes, &vgrpc.Delete{
			Kind: d.Kind,
			Key:  d.Key.Parts(),
		})
	}
	return resp, nil
}
