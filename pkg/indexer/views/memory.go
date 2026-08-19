package views

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// MemoryStore is an in-memory Store for unit testing.
type MemoryStore struct {
	mu       sync.RWMutex
	entities map[string]*Entity // key: "kind:key1" or "kind:key1:key2"
	version  int64
}

// NewMemoryStore creates a new in-memory Store for testing.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entities: make(map[string]*Entity),
	}
}

func entityKey(kind string, key Key) string {
	parts := key.Parts()
	switch len(parts) {
	case 1:
		return fmt.Sprintf("%s:%s", kind, parts[0])
	case 2:
		return fmt.Sprintf("%s:%s:%s", kind, parts[0], parts[1])
	default:
		return kind
	}
}

// Get retrieves an entity by kind and key.
func (m *MemoryStore) Get(_ context.Context, kind string, key Key) (*Entity, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	k := entityKey(kind, key)
	e, ok := m.entities[k]
	if !ok {
		return nil, nil
	}
	return e, nil
}

// Persist applies mutations atomically and returns a Change describing what was
// written. A nil result is treated as an empty result: the projection version
// still advances to eventID.
func (m *MemoryStore) Persist(_ context.Context, result *ReduceResult, eventID int64) (*Change, error) {
	if err := validateReduceResult(result); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if result == nil {
		result = &ReduceResult{}
	}

	change := &Change{
		Version: eventID,
	}

	for _, u := range result.Upserts {
		data, err := json.Marshal(u.Value)
		if err != nil {
			return nil, fmt.Errorf("marshal upsert value: %w", err)
		}
		k := entityKey(u.Kind, u.Key)
		m.entities[k] = &Entity{
			Kind:    u.Kind,
			Key:     u.Key,
			Value:   data,
			Version: eventID,
		}
		change.Upserts = append(change.Upserts, u)
	}

	for _, d := range result.Deletes {
		k := entityKey(d.Kind, d.Key)
		delete(m.entities, k)
		change.Deletes = append(change.Deletes, d)
	}

	m.version = eventID
	return change, nil
}

func validateReduceResult(result *ReduceResult) error {
	if result == nil {
		return nil
	}
	for _, u := range result.Upserts {
		if err := validateKey(u.Key); err != nil {
			return err
		}
	}
	for _, d := range result.Deletes {
		if err := validateKey(d.Key); err != nil {
			return err
		}
	}
	return nil
}

// Version returns the last processed event ID.
func (m *MemoryStore) Version() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.version
}
