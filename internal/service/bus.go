// Package service provides the core CQRS service implementation.
package service

import (
	"context"
	"encoding/json"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
	"go.opentelemetry.io/otel/codes"
)

// EventStorageEvent represents an event stored in the system.
type EventStorageEvent struct {
	Domain string
	Stream string
	ID     int64
	Kind   string
	Body   json.RawMessage
}

// LockReleasedEvent represents a consumer lock being released.
type LockReleasedEvent struct {
	Domain   string
	Stream   string
	Consumer string
	Holder   string
}

type bus struct {
	onEventStorage *emitter.MutexDispatcher[EventStorageEvent]
	onLockRelease  *emitter.MutexDispatcher[LockReleasedEvent]
}

func newBus() *bus {
	return &bus{
		onEventStorage: emitter.NewMutexDispatcher[EventStorageEvent](),
		onLockRelease:  emitter.NewMutexDispatcher[LockReleasedEvent](),
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

func (s *bus) dispatchOnLockReleased(parent context.Context, domain, stream, consumer, holder string) {
	ctx, span := tracer.Start(parent, "service.dispatchOnLockReleased")
	defer span.End()

	err := s.onLockRelease.Emit(ctx, LockReleasedEvent{
		Domain:   domain,
		Stream:   stream,
		Consumer: consumer,
		Holder:   holder,
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		span.AddEvent("failure in dispatchOnLockReleased")
	}
}
