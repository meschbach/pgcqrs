package service

import (
	"context"
	"time"

	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// grpcResultStream handles streaming query results over gRPC.
// It maintains the last sent operation ID to prevent duplicate events
// and translates storage operation results to gRPC messages.
//
// lastSentID is a defense-in-depth dedup guard: the SQL AfterID filter
// should prevent duplicates, but this catches any that slip through.
// Initialized to -1 so the first event (IDs start at 1 in PostgreSQL)
// passes the <= guard.
type grpcResultStream struct {
	out        ipc.Query_QueryServer
	lastSentID int64
}

// pushTranslatorMessage converts a storage operation result into a gRPC message and sends it to the client.
// It skips messages with IDs lower than the last sent ID to prevent duplicates.
func (g *grpcResultStream) pushTranslatorMessage(ctx context.Context, r storage2.OperationResult) error {
	id := r.Envelope.ID
	if id <= g.lastSentID {
		WatchDedupDrops.Add(ctx, 1)
		return nil
	}
	g.lastSentID = r.Envelope.ID

	// TODO(optimization): we are translating from PG's time to a string to gRPC.  PG gives us a time.Time.
	whenTime, err := time.Parse(time.RFC3339Nano, r.Envelope.When)
	if err != nil {
		return err
	}
	if err := g.out.Send(&ipc.QueryOut{
		Op: int64(r.Op),
		Id: &r.Envelope.ID,
		Envelope: &ipc.MaterializedEnvelope{
			Id:   r.Envelope.ID,
			When: timestamppb.New(whenTime),
			Kind: r.Envelope.Kind,
		},
		Body: r.Event,
	}); err != nil {
		return err
	}
	return nil
}
