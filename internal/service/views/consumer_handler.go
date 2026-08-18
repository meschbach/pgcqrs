// Package views provides gRPC handlers for the ViewProjectionConsumer service.
package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
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

// GetEntity retrieves a projected entity by kind and key, resolving version
// constraints. UntilVersion waits server-side via the notifier registry.
func (h *ConsumerHandler) GetEntity(ctx context.Context, req *vgrpc.GetEntityRequest) (*vgrpc.GetEntityResponse, error) {
	if len(req.Key) == 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "key must have at least 1 part")
	}
	id := views.NewProjectionIdentity(req.Domain, req.Stream, req.Projection)
	entity, err := h.store.Get(ctx, id, req.Kind, views.NewKey(req.Key...))
	if err != nil {
		return nil, err
	}

	entity, status, err := h.resolveGet(ctx, req, id, entity)
	if err != nil {
		return nil, err
	}
	return buildGetResponse(entity, status)
}

func (h *ConsumerHandler) resolveGet(ctx context.Context, req *vgrpc.GetEntityRequest, id views.ProjectionIdentity, entity *views.Entity) (*views.Entity, views.Status, error) {
	if constraint := req.GetUntilVersion(); constraint != nil {
		return h.resolveUntilVersion(ctx, req, id, entity, constraint)
	}

	if constraint := req.GetAfter(); constraint != nil {
		if entity == nil {
			return nil, views.StatusNotFound, nil
		}
		if entity.Version < constraint.Version {
			return entity, views.StatusStale, nil
		}
		return entity, views.StatusOK, nil
	}

	if entity == nil {
		return nil, views.StatusNotFound, nil
	}
	return entity, views.StatusOK, nil
}

func (h *ConsumerHandler) resolveUntilVersion(ctx context.Context, req *vgrpc.GetEntityRequest, id views.ProjectionIdentity, entity *views.Entity, constraint *vgrpc.UntilVersionConstraint) (*views.Entity, views.Status, error) {
	if entity != nil && entity.Version >= constraint.Version {
		return entity, views.StatusOK, nil
	}

	streamID, err := h.store.ResolveStreamID(ctx, req.Domain, req.Stream)
	if err != nil {
		return nil, 0, err
	}

	wctx, cancel := context.WithTimeout(ctx, time.Duration(constraint.TimeoutMs)*time.Millisecond)
	reached, err := h.registry.WaitForVersion(wctx, streamID, req.Projection, constraint.Version)
	cancel()
	if !reached {
		return h.untilTimeoutResult(ctx, req, id, err)
	}

	current, err := h.store.Get(ctx, id, req.Kind, views.NewKey(req.Key...))
	if err != nil {
		return nil, 0, err
	}
	if current == nil {
		return nil, views.StatusNotFound, nil
	}
	return current, views.StatusOK, nil
}

// untilTimeoutResult resolves the case where the wait ended without reaching the
// target version. A wait that only exhausted its own deadline returns the current
// entity with StatusTimeout; any other end maps to the wait error.
func (h *ConsumerHandler) untilTimeoutResult(ctx context.Context, req *vgrpc.GetEntityRequest, id views.ProjectionIdentity, waitErr error) (*views.Entity, views.Status, error) {
	if !errors.Is(waitErr, context.DeadlineExceeded) || ctx.Err() != nil {
		return nil, 0, waitErr
	}
	current, err := h.store.Get(ctx, id, req.Kind, views.NewKey(req.Key...))
	if err != nil {
		return nil, 0, err
	}
	return current, views.StatusTimeout, nil
}

// buildGetResponse maps a domain entity and status to the wire response.
func buildGetResponse(entity *views.Entity, status views.Status) (*vgrpc.GetEntityResponse, error) {
	protoStatus, err := protoFromStatus(status)
	if err != nil {
		return nil, err
	}
	resp := &vgrpc.GetEntityResponse{Status: protoStatus}
	if entity != nil {
		resp.Entity = &vgrpc.Entity{
			Kind:    entity.Kind,
			Key:     entity.Key.Parts(),
			Value:   entity.Value,
			Version: entity.Version,
		}
	}
	return resp, nil
}

// protoFromStatus maps the domain Status to the wire GetStatus enum.
func protoFromStatus(s views.Status) (vgrpc.GetStatus, error) {
	switch s {
	case views.StatusOK:
		return vgrpc.GetStatus_GET_STATUS_OK, nil
	case views.StatusNotFound:
		return vgrpc.GetStatus_GET_STATUS_NOT_FOUND, nil
	case views.StatusStale:
		return vgrpc.GetStatus_GET_STATUS_STALE, nil
	case views.StatusTimeout:
		return vgrpc.GetStatus_GET_STATUS_TIMEOUT, nil
	default:
		return vgrpc.GetStatus(0), fmt.Errorf("unknown get status: %d", s)
	}
}

// Version returns the current projection version.
func (h *ConsumerHandler) Version(ctx context.Context, req *vgrpc.VersionRequest) (*vgrpc.VersionResponse, error) {
	id := views.NewProjectionIdentity(req.Domain, req.Stream, req.Projection)
	version, err := h.store.Version(ctx, id)
	if err != nil {
		return nil, err
	}
	return &vgrpc.VersionResponse{Version: version}, nil
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

	for change, err := range h.changeStream(ctx, streamID, req.Projection, afterVersion) {
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

func (h *ConsumerHandler) changeStream(ctx context.Context, streamID int64, projection string, afterVersion int64) iter.Seq2[views.Change, error] {
	return func(yield func(views.Change, error) bool) {
		ch := make(chan views.Change, 16)
		unsub := h.registry.OnChange(streamID, projection, func(_ context.Context, c views.Change) error {
			select {
			case ch <- c:
			default:
			}
			return nil
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
