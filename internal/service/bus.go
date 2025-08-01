package service

import (
	"context"
	"encoding/json"
	"github.com/meschbach/go-junk-bucket/pkg/emitter"
	"go.opentelemetry.io/otel/codes"
)

type EventStorageEvent struct {
	Domain string
	Stream string
	ID     int64
	Kind   string
	Body   json.RawMessage
}

type bus struct {
	onEventStorage *emitter.MutexDispatcher[EventStorageEvent]
}

func newBus() *bus {
	return &bus{
		onEventStorage: emitter.NewMutexDispatcher[EventStorageEvent](),
	}
}

func (s *bus) dispatchOnEventStored(parent context.Context, domain, stream string, id int64, kind string, body json.RawMessage) {
	ctx, span := tracer.Start(parent, "service.dispatchOnEventStored")
	defer span.End()

	err := s.onEventStorage.Emit(ctx, EventStorageEvent{
		Domain: domain,
		Stream: stream,
		ID:     id,
		Kind:   kind,
		Body:   body,
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		span.AddEvent("failure in dispatchOnEventStored")
	}
}
