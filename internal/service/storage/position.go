package storage

// Package storage provides database storage operations for the PGCQRS event store.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StreamNotFoundError is returned when a consumer attempts to set position on a non-existent stream.
type StreamNotFoundError struct {
	Domain string
	Stream string
}

func (e *StreamNotFoundError) Error() string {
	return "stream does not exist: " + e.Domain + "/" + e.Stream
}

// PositionStore provides storage operations for consumer positions in a stream.
type PositionStore struct {
	pg *pgxpool.Pool
}

// NewPositionStore creates a new PositionStore instance.
func NewPositionStore(pg *pgxpool.Pool) *PositionStore {
	return &PositionStore{pg: pg}
}

// SetPositionResult contains the result of a SetPosition operation.
type SetPositionResult struct {
	PreviousEventID *int64
	CurrentEventID  int64
}

// SetPosition records a consumer's position in a stream.
func (s *PositionStore) SetPosition(ctx context.Context, domain, stream, consumer string, eventID int64) (*SetPositionResult, error) {
	row := s.pg.QueryRow(ctx, `
		WITH es AS (
			SELECT id FROM events_stream WHERE app = $1 AND stream = $2 FOR SHARE
		),
		previous AS (
			SELECT cp.event_id as previous_event_id
			FROM consumer_positions cp
			JOIN es ON cp.stream_id = es.id
			WHERE cp.consumer = $3
		)
		INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
		SELECT es.id, $3, $4, NOW()
		FROM es
		CROSS JOIN (SELECT 1) AS stream_required
		WHERE NOT EXISTS (SELECT 1 FROM previous WHERE COALESCE(previous_event_id, 0) > $4)
		ON CONFLICT (stream_id, consumer) DO UPDATE 
		SET event_id = EXCLUDED.event_id, updated_at = EXCLUDED.updated_at
		WHERE COALESCE(consumer_positions.event_id, 0) <= EXCLUDED.event_id
		RETURNING 
			(SELECT previous_event_id FROM previous) as previous_event_id,
			$4 as current_event_id`, domain, stream, consumer, eventID)

	var result SetPositionResult
	err := row.Scan(&result.PreviousEventID, &result.CurrentEventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &StreamNotFoundError{Domain: domain, Stream: stream}
		}
		return nil, err
	}
	return &result, nil
}

// BackwardPositionError is returned when a consumer attempts to set position backwards.
type BackwardPositionError struct {
	Consumer  string
	Current   int64
	Requested int64
}

func (e *BackwardPositionError) Error() string {
	return "cannot set position backwards"
}

// GetPosition retrieves a consumer's current position in a stream.
func (s *PositionStore) GetPosition(ctx context.Context, domain, stream, consumer string) (eventID int64, found bool, err error) {
	err = s.pg.QueryRow(ctx, `
		SELECT cp.event_id 
		FROM consumer_positions cp
		JOIN events_stream es ON cp.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2 AND cp.consumer = $3`,
		domain, stream, consumer).Scan(&eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return eventID, true, nil
}

// ListConsumers returns all consumers that have a position in the specified stream.
func (s *PositionStore) ListConsumers(ctx context.Context, domain, stream string) ([]string, error) {
	rows, err := s.pg.Query(ctx, `
		SELECT cp.consumer 
		FROM consumer_positions cp
		JOIN events_stream es ON cp.stream_id = es.id
		WHERE es.app = $1 AND es.stream = $2`, domain, stream)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []string{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var consumers []string
	for rows.Next() {
		var consumer string
		if err := rows.Scan(&consumer); err != nil {
			return nil, err
		}
		consumers = append(consumers, consumer)
	}
	if consumers == nil {
		consumers = []string{}
	}
	return consumers, rows.Err()
}

// DeletePosition removes a consumer's position from a stream.
func (s *PositionStore) DeletePosition(ctx context.Context, domain, stream, consumer string) error {
	_, err := s.pg.Exec(ctx, `
		DELETE FROM consumer_positions 
		WHERE stream_id IN (
			SELECT es.id FROM events_stream es 
			WHERE es.app = $1 AND es.stream = $2
		) AND consumer = $3`, domain, stream, consumer)
	return err
}
