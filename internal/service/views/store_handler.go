package views

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
)

// StoreHandler implements the ViewProjectionStore gRPC service.
type StoreHandler struct {
	vgrpc.UnimplementedViewProjectionStoreServer
	store    *views.PGStore
	registry *NotifierRegistry
}

// NewStoreHandler creates a new ViewProjectionStore handler.
func NewStoreHandler(store *views.PGStore, registry *NotifierRegistry) *StoreHandler {
	return &StoreHandler{store: store, registry: registry}
}

// ApplyMutations applies upserts and deletes to a projection.
func (h *StoreHandler) ApplyMutations(ctx context.Context, req *vgrpc.ApplyMutationsRequest) (*vgrpc.ApplyMutationsResponse, error) {
	result := &views.ReduceResult{}
	for _, u := range req.Upserts {
		result.Upserts = append(result.Upserts, views.Upsert{
			Kind:  u.Kind,
			Key:   views.NewKey(u.Key...),
			Value: u.Value,
		})
	}
	for _, d := range req.Deletes {
		result.Deletes = append(result.Deletes, views.Delete{
			Kind: d.Kind,
			Key:  views.NewKey(d.Key...),
		})
	}

	id := views.NewProjectionIdentity(req.Domain, req.Stream, req.Projection)
	change, err := h.store.Persist(ctx, id, result, req.EventId)
	if err != nil {
		return nil, err
	}

	streamID, err := h.store.ResolveStreamID(ctx, req.Domain, req.Stream)
	if err != nil {
		return nil, err
	}
	if h.registry != nil {
		if err := h.registry.Notify(ctx, streamID, req.Projection, *change); err != nil {
			return nil, err
		}
	}

	return h.buildApplyResponse(change)
}

func (h *StoreHandler) buildApplyResponse(change *views.Change) (*vgrpc.ApplyMutationsResponse, error) {
	resp := &vgrpc.ApplyMutationsResponse{
		Version: change.Version,
	}
	for _, u := range change.Upserts {
		var data []byte
		if bytes, ok := u.Value.([]byte); ok {
			data = bytes
		} else {
			var err error
			data, err = json.Marshal(u.Value)
			if err != nil {
				return nil, fmt.Errorf("marshal upsert value: %w", err)
			}
		}
		resp.AppliedUpserts = append(resp.AppliedUpserts, &vgrpc.Upsert{
			Kind:  u.Kind,
			Key:   u.Key.Parts(),
			Value: data,
		})
	}
	for _, d := range change.Deletes {
		resp.AppliedDeletes = append(resp.AppliedDeletes, &vgrpc.Delete{
			Kind: d.Kind,
			Key:  d.Key.Parts(),
		})
	}
	return resp, nil
}

// GetEntity retrieves an entity by kind and key.
// This is the write-path service: it serves plain snapshot reads only and never
// waits on version. Version-constrained reads are served by ViewProjectionConsumer.
func (h *StoreHandler) GetEntity(ctx context.Context, req *vgrpc.StoreGetEntityRequest) (*vgrpc.GetEntityResponse, error) {
	id := views.NewProjectionIdentity(req.Domain, req.Stream, req.Projection)
	entity, err := h.store.Get(ctx, id, req.Kind, views.NewKey(req.Key...))
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return &vgrpc.GetEntityResponse{Status: vgrpc.GetStatus_GET_STATUS_NOT_FOUND}, nil
	}
	return &vgrpc.GetEntityResponse{
		Entity: &vgrpc.Entity{
			Kind:    entity.Kind,
			Key:     entity.Key.Parts(),
			Value:   entity.Value,
			Version: entity.Version,
		},
		Status: vgrpc.GetStatus_GET_STATUS_OK,
	}, nil
}
