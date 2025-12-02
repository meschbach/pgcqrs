package service

import (
	"context"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

// grpcResultStream handles streaming query results over gRPC.
// It maintains the last seen operation ID to prevent duplicate events
// and translates storage operation results to gRPC messages.
type grpcResultStream struct {
	out        ipc.Query_QueryServer
	lastSeenID int64
}

// runTranslator processes operation results from the storage layer and translates them into gRPC messages.
// It continuously listens for new results until the context is cancelled or an error occurs.
// The results are received through onEachResult channel and any errors are sent to onDone channel.
func (g *grpcResultStream) runTranslator(parent context.Context, onEachResult <-chan storage2.OperationResult, onDone chan error) {
	defer close(onDone)

	ctx, span := tracer.Start(parent, "grpcResultStream.runTranslator")
	defer span.End()

	onError := func(err error) {
		onDone <- err
	}
	for {
		select {
		case <-ctx.Done():
			onError(ctx.Err())
		case r := <-onEachResult:
			if err := g.pushTranslatorMessage(ctx, r); err != nil {
				onError(err)
				return
			}
		}
	}
}

// pushTranslatorMessage converts a storage operation result into a gRPC message and sends it to the client.
// It skips messages with IDs lower than the last seen ID to prevent duplicates.
// Returns an error if the message conversion or sending fails.
func (g *grpcResultStream) pushTranslatorMessage(parent context.Context, r storage2.OperationResult) error {
	//
	// ignore events if we've already seen them
	//
	id := r.Envelope.ID
	if id < g.lastSeenID {
		return nil
	}
	g.lastSeenID = r.Envelope.ID

	//todo(optimization): we are translating from PG's time to a string to gRPC.  PG gives us a time.Time.
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

type grpcSink interface {
	send(ctx context.Context, msg *ipc.QueryOut) error
}

type versionFilter struct {
	lastSeen int64
	next     grpcSink
}

func (v *versionFilter) send(ctx context.Context, msg *ipc.QueryOut) error {
	if msg.Id == nil {
		return nil
	}
	if *msg.Id <= v.lastSeen {
		return nil
	}
	v.lastSeen = *msg.Id
	return v.next.send(ctx, msg)
}

type opSplitter struct {
	opPipeline map[int64]grpcSink
	onNew      func(int64) grpcSink
}

func newOpSplitter(onNew func(int64) grpcSink) *opSplitter {
	return &opSplitter{
		opPipeline: make(map[int64]grpcSink),
		onNew:      onNew,
	}
}

func (o *opSplitter) send(ctx context.Context, msg *ipc.QueryOut) error {
	op := msg.Op
	var sink grpcSink
	if storedSink, ok := o.opPipeline[op]; ok {
		sink = storedSink
	} else {
		sink = o.onNew(op)
		o.opPipeline[op] = sink
	}
	return sink.send(ctx, msg)
}
