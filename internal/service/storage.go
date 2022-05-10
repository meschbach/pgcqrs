package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/meschbach/pgcqrs/internal"
	"github.com/meschbach/pgcqrs/internal/junk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type storage struct {
	pg *pgxpool.Pool
}

func (s *storage) start(ctx context.Context, cfg internal.PGStorage) {
	pool, err := pgxpool.Connect(ctx, "postgres://"+cfg.DatabaseURL)
	junk.Must(err)
	s.pg = pool
}

type pgMeta struct {
	ID   int64
	When pgtype.Timestamptz
	Kind string
}

type EventLoader = func(interface{}) error
type OnEvent = func(meta pgMeta) error

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

	results, err := s.query(ctx, "SELECT id, when_occurred, kind FROM events WHERE stream_id = (SELECT id FROM events_stream WHERE app = $1 AND stream = $2) ORDER BY when_occurred ASC", app, stream)
	if err != nil {
		return err
	}
	defer results.Close()

	for results.Next() {
		var meta pgMeta
		if err := results.Scan(&meta.ID, &meta.When, &meta.Kind); err != nil {
			return err
		}
		if err := onEvent(meta); err != nil {
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

	results, err := s.query(ctx, "INSERT INTO events(kind, event, stream_id) SELECT $1, $2, s.id as stream_id FROM events_stream s WHERE app = $3 AND stream = $4 RETURNING id", kind, body, app, stream)
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

func (s *storage) store(ctx context.Context, app, stream, kind string, body []byte) int64 {
	id, err := s.unsafeStore(ctx, app, stream, kind, body)
	junk.Must(err)
	return id
}

type noSuchIDError struct {
	app    string
	stream string
	id     int64
}

func (n *noSuchIDError) Error() string {
	return fmt.Sprintf("ID %d does not exist for app %q and stream %q", n.id, n.app, n.stream)
}

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
