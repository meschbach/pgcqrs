package views

import (
	"context"
	"encoding/json"
	"fmt"

	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
	"google.golang.org/grpc"
)

// RemoteStore is backed by the ViewProjectionStore gRPC service in pgcqrs.
type RemoteStore struct {
	client vgrpc.ViewProjectionStoreClient
	id     ProjectionIdentity
}

// NewRemoteStore creates a Store backed by the ViewProjectionStore gRPC service.
func NewRemoteStore(conn *grpc.ClientConn, id ProjectionIdentity) *RemoteStore {
	return &RemoteStore{
		client: vgrpc.NewViewProjectionStoreClient(conn),
		id:     id,
	}
}

// Get retrieves an entity by kind and key from the remote store.
func (r *RemoteStore) Get(ctx context.Context, kind string, key Key) (*Entity, error) {
	resp, err := r.client.GetEntity(ctx, &vgrpc.StoreGetEntityRequest{
		Projection: r.id.Projection,
		Domain:     r.id.Domain,
		Stream:     r.id.Stream,
		Kind:       kind,
		Key:        key.Parts(),
	})
	if err != nil {
		return nil, fmt.Errorf("get entity: %w", err)
	}
	if resp.Entity == nil {
		return nil, nil
	}
	return &Entity{
		Kind:    resp.Entity.Kind,
		Key:     NewKey(resp.Entity.Key...),
		Value:   resp.Entity.Value,
		Version: resp.Entity.Version,
	}, nil
}

// Persist applies mutations atomically via the remote store. A nil result is
// treated as an empty result; the server still advances the projection version.
func (r *RemoteStore) Persist(ctx context.Context, result *ReduceResult, eventID int64) (*Change, error) {
	req := &vgrpc.ApplyMutationsRequest{
		Projection: r.id.Projection,
		Domain:     r.id.Domain,
		Stream:     r.id.Stream,
		EventId:    eventID,
	}

	if result != nil {
		for _, u := range result.Upserts {
			data, err := json.Marshal(u.Value)
			if err != nil {
				return nil, fmt.Errorf("marshal upsert value: %w", err)
			}
			req.Upserts = append(req.Upserts, &vgrpc.Upsert{
				Kind:  u.Kind,
				Key:   u.Key.Parts(),
				Value: data,
			})
		}

		for _, d := range result.Deletes {
			req.Deletes = append(req.Deletes, &vgrpc.Delete{
				Kind: d.Kind,
				Key:  d.Key.Parts(),
			})
		}
	}

	resp, err := r.client.ApplyMutations(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("apply mutations: %w", err)
	}

	change := &Change{
		Version: resp.Version,
	}
	for _, u := range resp.AppliedUpserts {
		change.Upserts = append(change.Upserts, Upsert{
			Kind:  u.Kind,
			Key:   NewKey(u.Key...),
			Value: json.RawMessage(u.Value),
		})
	}
	for _, d := range resp.AppliedDeletes {
		change.Deletes = append(change.Deletes, Delete{
			Kind: d.Kind,
			Key:  NewKey(d.Key...),
		})
	}
	return change, nil
}
