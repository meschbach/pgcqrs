package service

import (
	"context"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type grpcResultStream struct {
	out        ipc.Query_QueryServer
	lastSeenID int64
}

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

func (g *grpcResultStream) pushTranslatorMessage(parent context.Context, r storage2.OperationResult) error {
	//
	// ignore events if we've already seen them
	//
	id := r.Envelope.ID
	if id <= g.lastSeenID {
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
