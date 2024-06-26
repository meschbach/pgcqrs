package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// storage is a wrapper over the Postgres document repository
// Deprecated Move to storage.Repository
type storage struct {
	pg *pgxpool.Pool
}

type pgMeta struct {
	ID   int64
	When pgtype.Timestamptz
	Kind string
}

type EventLoader = func(interface{}) error
type OnEvent = func(ctx context.Context, meta pgMeta, event json.RawMessage) error

func (s *storage) query(parent context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	ctx, span := tracer.Start(parent, sql, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	results, err := s.pg.Query(ctx, sql, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return results, err
}

func (s *storage) replayMeta(parent context.Context, app, stream string, onEvent OnEvent) error {
	ctx, span := tracer.Start(parent, "replayMeta")
	defer span.End()

	results, err := s.query(ctx, "SELECT e.id, when_occurred, k.kind FROM events e INNER JOIN events_kind k ON e.kind_id = k.id WHERE stream_id = (SELECT id FROM events_stream WHERE app = $1 AND stream = $2) ORDER BY when_occurred ASC", app, stream)
	if err != nil {
		return err
	}
	defer results.Close()

	for results.Next() {
		var meta pgMeta
		if err := results.Scan(&meta.ID, &meta.When, &meta.Kind); err != nil {
			return err
		}
		if err := onEvent(ctx, meta, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *storage) unsafeStore(parent context.Context, app, stream, kind string, body []byte) (int64, error) {
	ctx, span := tracer.Start(parent, "unsafeStore",
		trace.WithAttributes(attribute.String("pg-cqrs.app", app),
			attribute.String("pg-cqrs.stream", stream)))
	defer span.End()

	//kind upsert
	//TODO: Find a better way to upsert in single round trip
	r, err := s.query(ctx, `INSERT INTO events_kind(kind) VALUES ($1) ON CONFLICT DO NOTHING`, kind)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "query error")
		return -1, err
	}
	r.Close()

	sql := `
INSERT INTO events(kind_id, event, stream_id)
SELECT (SELECT k.id FROM events_kind k WHERE k.kind = $1), $2, (SELECT s.id FROM events_stream as s WHERE s.app = $3 AND s.stream = $4)
RETURNING id
`
	results, err := s.query(ctx, sql, kind, body, app, stream)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "query error")
		return -1, err
	}
	defer results.Close()
	if !results.Next() {
		span.SetStatus(codes.Error, "expected at least one row, got none")
		return -1, errors.New("programmatic error: query should have returned result")
	}
	var out int64
	if err := results.Scan(&out); err != nil {
		return -1, err
	}
	if results.Next() {
		span.SetStatus(codes.Error, "multiple rows resulted.  expected only 1.")
		return -1, errors.New("programmatic error: query has more than one row")
	}
	return out, nil
}

type noSuchIDError struct {
	app    string
	stream string
	id     int64
}

func (n *noSuchIDError) Error() string {
	return fmt.Sprintf("ID %d does not exist for app %q and stream %q", n.id, n.app, n.stream)
}

// fetchPayload retrieves the bytes of the requested event identified by {app,stream,id}.  if no such event could be
// found then noSuchIDError will be returned
func (s *storage) fetchPayload(parent context.Context, app, stream string, id int64) ([]byte, error) {
	ctx, span := tracer.Start(parent, "fetchPayload")
	defer span.End()

	results, err := s.query(ctx, "SELECT event FROM events WHERE id = $1 AND stream_id = (SELECT id FROM events_stream WHERE app = $2 AND stream = $3) ORDER BY when_occurred ASC", id, app, stream)
	if err != nil {
		return nil, err
	}
	defer results.Close()

	if !results.Next() {
		return nil, &noSuchIDError{
			app:    app,
			stream: stream,
			id:     id,
		}
	}
	var out []byte
	return out, results.Scan(&out)
}

func (s *storage) ensureStream(parent context.Context, app, stream string) error {
	ctx, span := tracer.Start(parent, "ensureStream")
	defer span.End()

	results, err := s.pg.Query(ctx, "INSERT INTO events_stream(app,stream) VALUES($1,$2) ON CONFLICT DO NOTHING", app, stream)
	if err != nil {
		return err
	}
	defer results.Close()

	if results.Next() {
		return errors.New("unexpected PG result")
	}
	return nil
}

func (s *storage) applyQuery(parent context.Context, app, stream string, params v1.WireQuery, extractEvent bool, event OnEvent) error {
	ctx, span := tracer.Start(parent, "applyQuery")
	defer span.End()
	query := storage2.TranslateQuery(app, stream, params, extractEvent)
	span.AddEvent("translated")

	span.SetAttributes(attribute.String("pg.query", query.DML))
	span.AddEvent("querying")
	rows, err := s.pg.Query(ctx, query.DML, query.Args...)
	if err != nil {
		span.SetStatus(codes.Error, "query")
		span.RecordError(err)
		return err
	}
	defer rows.Close()
	span.AddEvent("has-results")

	//convert query results to
	count, err := s.dispatchRowMeta(ctx, rows, extractEvent, event)
	if err != nil {
		span.RecordError(err, trace.WithAttributes(attribute.Int("at-row", count)))
		span.SetStatus(codes.Error, "dispatching rows")
		return err
	}
	span.AddEvent("dispatched", trace.WithAttributes(attribute.Int("rows", count)))
	return nil
}

// dispatchRowMeta converts a database row for event metadata into a pgMeta object
func (s *storage) dispatchRowMeta(ctx context.Context, results pgx.Rows, extractEvent bool, onEvent OnEvent) (int, error) {
	count := 0
	for results.Next() {
		count++
		var meta pgMeta
		var event json.RawMessage = nil
		if extractEvent {
			if err := results.Scan(&meta.ID, &meta.When, &meta.Kind, &event); err != nil {
				return count, err
			}
		} else {
			if err := results.Scan(&meta.ID, &meta.When, &meta.Kind); err != nil {
				return count, err
			}
		}
		if err := onEvent(ctx, meta, event); err != nil {
			return count, err
		}
	}
	if err := results.Err(); err != nil {
		return count, err
	}
	return count, nil
}
