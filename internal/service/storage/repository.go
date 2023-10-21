package storage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"time"
)

// Repository wraps a Postgres database as a document repository
type Repository struct {
	pg *pgxpool.Pool
}

func RepositoryWithPool(pg *pgxpool.Pool) *Repository {
	return &Repository{pg: pg}
}

type Operation interface {
	append(q *SQLQuery)
}

type OperationResult struct {
	Op int
	// Envelope contains the {ID,When}.  NOTE: No other fields are filled in
	Envelope v1.Envelope
	Event    json.RawMessage
}

func (r *Repository) Stream(ctx context.Context, ops []Operation) (<-chan OperationResult, func(ctx context.Context) (int, error), error) {
	if len(ops) == 0 {
		return nil, nil, errors.New("no target operations")
	}
	query := &SQLQuery{}
	query.append("SELECT o.id, o.when_occurred, o.op, o.event, o.kind FROM (")
	first := true
	for _, op := range ops {
		if first {
			first = false
		} else {
			query.append("UNION ALL")
		}
		op.append(query)
	}
	query.append(") as o ORDER BY o.when_occurred ASC")

	//TODO: wish there was a way to wrap PG with otel
	queryCtx, span := tracer.Start(ctx, "query")
	span.SetAttributes(attribute.String("dml", query.DML))
	rows, err := r.pg.Query(queryCtx, query.DML, query.Args...)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "query failed")
		span.End()
		return nil, nil, err
	}
	span.End()

	sink := make(chan OperationResult)
	return sink, func(ctx context.Context) (int, error) {
		defer close(sink)
		index := 0
		for rows.Next() {
			var out OperationResult
			var when pgtype.Timestamptz
			if err := rows.Scan(&out.Envelope.ID, &when, &out.Op, &out.Event, &out.Envelope.Kind); err != nil {
				span := trace.SpanFromContext(ctx)
				span.SetStatus(codes.Error, "failed to scan")
				span.RecordError(err, trace.WithAttributes(attribute.Int("row", index)))
				return index, err
			}
			out.Envelope.When = when.Time.Format(time.RFC3339Nano)
			sink <- out
			index++
		}
		return index, nil
	}, nil
}
