package storage

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Repository wraps a Postgres database as a document repository
type Repository struct {
	pg *pgxpool.Pool
}

// RepositoryWithPool creates a new Repository using the given pgxpool.Pool.
func RepositoryWithPool(pg *pgxpool.Pool) *Repository {
	return &Repository{pg: pg}
}

// Operation represents a query operation.
type Operation interface {
	append(q *SQLQuery)
	UpdateAfterID(id int64)
}

// OperationResult represents the result of a query operation.
type OperationResult struct {
	Op int
	// Envelope contains the {ID,When}.  NOTE: No other fields are filled in
	Envelope v1.Envelope
	Event    json.RawMessage
}

// Stream returns an iterator over the query results. It builds and executes the
// SQL query eagerly, then yields rows lazily via the returned iterator. This
// eliminates goroutine/channel coordination and makes deadlocks impossible.
func (r *Repository) Stream(ctx context.Context, ops []Operation) iter.Seq2[OperationResult, error] {
	if len(ops) == 0 {
		return func(yield func(OperationResult, error) bool) {
			yield(OperationResult{}, errors.New("no target operations"))
		}
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

	queryCtx, span := tracer.Start(ctx, "query")
	span.SetAttributes(attribute.String("dml", query.DML))
	rows, err := r.pg.Query(queryCtx, query.DML, query.Args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "query failed")
		span.End()
		return func(yield func(OperationResult, error) bool) {
			yield(OperationResult{}, err)
		}
	}
	span.End()

	return func(yield func(OperationResult, error) bool) {
		defer rows.Close()
		index := 0
		for rows.Next() {
			var out OperationResult
			var when pgtype.Timestamptz
			if err := rows.Scan(&out.Envelope.ID, &when, &out.Op, &out.Event, &out.Envelope.Kind); err != nil {
				scanSpan := trace.SpanFromContext(ctx)
				scanSpan.SetStatus(codes.Error, "failed to scan")
				scanSpan.RecordError(err, trace.WithAttributes(attribute.Int("row", index)))
				yield(OperationResult{}, err)
				return
			}
			out.Envelope.When = v1.FormatEnvelopeWhen(when.Time)
			if !yield(out, nil) {
				return
			}
			index++
		}
	}
}
