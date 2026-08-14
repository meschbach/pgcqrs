package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is a PostgreSQL-backed Store for view projections.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore creates a new PostgreSQL-backed Store.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// ResolveStreamID returns the numeric ID for a domain/stream pair.
func (s *PGStore) ResolveStreamID(ctx context.Context, domain, stream string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM events_stream WHERE app = $1 AND stream = $2`,
		domain, stream).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, &StreamNotFoundError{Domain: domain, Stream: stream}
	}
	if err != nil {
		return 0, fmt.Errorf("resolve stream: %w", err)
	}
	return id, nil
}

func (s *PGStore) resolveProjectionName(ctx context.Context, tx pgx.Tx, streamID int64, id ProjectionIdentity) (int64, error) {
	var projID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO view_projection_names (stream_id, name) VALUES ($1, $2)
		ON CONFLICT (stream_id, name) DO NOTHING
		RETURNING id
	`, streamID, id.Projection).Scan(&projID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT id FROM view_projection_names
			WHERE stream_id = $1 AND name = $2
		`, streamID, id.Projection).Scan(&projID)
	}
	return projID, err
}

func (s *PGStore) resolveKindName(ctx context.Context, tx pgx.Tx, kind string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO view_projection_kinds (kind) VALUES ($1)
		ON CONFLICT (kind) DO NOTHING
		RETURNING id
	`, kind).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM view_projection_kinds WHERE kind = $1`, kind).Scan(&id)
	}
	return id, err
}

// Get retrieves an entity by kind and key.
func (s *PGStore) Get(ctx context.Context, id ProjectionIdentity, kind string, key Key) (*Entity, error) {
	streamID, err := s.ResolveStreamID(ctx, id.Domain, id.Stream)
	if err != nil {
		return nil, err
	}

	var projID, kindID int64
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM view_projection_names WHERE stream_id = $1 AND name = $2
	`, streamID, id.Projection).Scan(&projID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve projection: %w", err)
	}

	err = s.pool.QueryRow(ctx, `SELECT id FROM view_projection_kinds WHERE kind = $1`, kind).Scan(&kindID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve kind: %w", err)
	}

	parts := key.Parts()
	if len(parts) == 1 {
		return s.getSingle(ctx, projID, kindID, parts[0])
	}
	return s.getComposite(ctx, projID, kindID, parts[0], parts[1])
}

func (s *PGStore) getSingle(ctx context.Context, projID, kindID int64, k string) (*Entity, error) {
	var value []byte
	var version int64
	err := s.pool.QueryRow(ctx, `
		SELECT value, version FROM view_projection_entries_single
		WHERE projection_id = $1 AND kind_id = $2 AND key = $3
	`, projID, kindID, k).Scan(&value, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get single: %w", err)
	}
	return &Entity{Key: NewKey(k), Value: value, Version: version}, nil
}

func (s *PGStore) getComposite(ctx context.Context, projID, kindID int64, k1, k2 string) (*Entity, error) {
	var value []byte
	var version int64
	err := s.pool.QueryRow(ctx, `
		SELECT value, version FROM view_projection_entries_composite
		WHERE projection_id = $1 AND kind_id = $2 AND key1 = $3 AND key2 = $4
	`, projID, kindID, k1, k2).Scan(&value, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get composite: %w", err)
	}
	return &Entity{Key: NewKey(k1, k2), Value: value, Version: version}, nil
}

// Persist applies mutations atomically in a single transaction. A nil result is
// treated as an empty result: the projection version still advances to eventID
// via a consumer_positions row written in the same transaction.
func (s *PGStore) Persist(ctx context.Context, id ProjectionIdentity, result *ReduceResult, eventID int64) (*Change, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	change, err := s.applyAndRecord(ctx, tx, id, result, eventID)
	if err != nil {
		rollbackErr := tx.Rollback(ctx)
		return nil, errors.Join(err, rollbackErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return change, nil
}

func (s *PGStore) applyAndRecord(ctx context.Context, tx pgx.Tx, id ProjectionIdentity, result *ReduceResult, eventID int64) (*Change, error) {
	if result == nil {
		result = &ReduceResult{}
	}

	streamID, err := s.ResolveStreamID(ctx, id.Domain, id.Stream)
	if err != nil {
		return nil, err
	}

	projID, err := s.resolveProjectionName(ctx, tx, streamID, id)
	if err != nil {
		return nil, fmt.Errorf("resolve projection name: %w", err)
	}

	change, err := s.applyMutations(ctx, tx, projID, result, eventID)
	if err != nil {
		return nil, err
	}

	if err := s.writePosition(ctx, tx, streamID, id.Projection, eventID); err != nil {
		return nil, err
	}
	return change, nil
}

func (s *PGStore) writePosition(ctx context.Context, tx pgx.Tx, streamID int64, consumer string, eventID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (stream_id, consumer) DO UPDATE
		SET event_id = EXCLUDED.event_id,
		    updated_at = EXCLUDED.updated_at
		WHERE COALESCE(consumer_positions.event_id, 0) <= EXCLUDED.event_id
	`, streamID, consumer, eventID)
	if err != nil {
		return fmt.Errorf("update consumer position: %w", err)
	}
	return nil
}

func (s *PGStore) applyMutations(ctx context.Context, tx pgx.Tx, projID int64, result *ReduceResult, eventID int64) (*Change, error) {
	change := &Change{Version: eventID}

	for _, u := range result.Upserts {
		if err := s.applyUpsert(ctx, tx, projID, u, eventID); err != nil {
			return nil, err
		}
		change.Upserts = append(change.Upserts, u)
	}

	for _, d := range result.Deletes {
		if err := s.applyDelete(ctx, tx, projID, d); err != nil {
			return nil, err
		}
		change.Deletes = append(change.Deletes, d)
	}

	return change, nil
}

func (s *PGStore) applyUpsert(ctx context.Context, tx pgx.Tx, projID int64, u Upsert, eventID int64) error {
	data, err := marshalValue(u.Value)
	if err != nil {
		return fmt.Errorf("marshal upsert value: %w", err)
	}

	kindID, err := s.resolveKindName(ctx, tx, u.Kind)
	if err != nil {
		return fmt.Errorf("resolve kind: %w", err)
	}

	parts := u.Key.Parts()
	if len(parts) == 1 {
		_, err = tx.Exec(ctx, `
			INSERT INTO view_projection_entries_single (projection_id, kind_id, key, value, version, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			ON CONFLICT (projection_id, kind_id, key)
			DO UPDATE SET value = EXCLUDED.value, version = EXCLUDED.version, updated_at = NOW()
		`, projID, kindID, parts[0], data, eventID)
	} else {
		_, err = tx.Exec(ctx, `
			INSERT INTO view_projection_entries_composite (projection_id, kind_id, key1, key2, value, version, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (projection_id, kind_id, key1, key2)
			DO UPDATE SET value = EXCLUDED.value, version = EXCLUDED.version, updated_at = NOW()
		`, projID, kindID, parts[0], parts[1], data, eventID)
	}
	if err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	return nil
}

func (s *PGStore) applyDelete(ctx context.Context, tx pgx.Tx, projID int64, d Delete) error {
	kindID, err := s.resolveKindName(ctx, tx, d.Kind)
	if err != nil {
		return fmt.Errorf("resolve kind: %w", err)
	}

	parts := d.Key.Parts()
	if len(parts) == 1 {
		_, err = tx.Exec(ctx, `
			DELETE FROM view_projection_entries_single
			WHERE projection_id = $1 AND kind_id = $2 AND key = $3
		`, projID, kindID, parts[0])
	} else {
		_, err = tx.Exec(ctx, `
			DELETE FROM view_projection_entries_composite
			WHERE projection_id = $1 AND kind_id = $2 AND key1 = $3 AND key2 = $4
		`, projID, kindID, parts[0], parts[1])
	}
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

func marshalValue(value any) ([]byte, error) {
	if bytes, ok := value.([]byte); ok {
		return bytes, nil
	}
	return json.Marshal(value)
}

// Version returns the projection version from consumer_positions.
func (s *PGStore) Version(ctx context.Context, id ProjectionIdentity) (int64, error) {
	streamID, err := s.ResolveStreamID(ctx, id.Domain, id.Stream)
	if err != nil {
		return 0, err
	}

	var version int64
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(cp.event_id, 0)
		FROM view_projection_names vpn
		JOIN consumer_positions cp ON cp.stream_id = vpn.stream_id AND cp.consumer = vpn.name
		WHERE vpn.stream_id = $1 AND vpn.name = $2
	`, streamID, id.Projection).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get version: %w", err)
	}
	return version, nil
}
