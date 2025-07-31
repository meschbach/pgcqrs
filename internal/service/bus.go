package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/meschbach/go-junk-bucket/pkg/fx"
	"go.opentelemetry.io/otel/codes"
	"sync"
)

type EventStorageEvent struct {
	Domain string
	Stream string
	ID     int64
	Kind   string
	Body   json.RawMessage
}

type OnEventStorage func(ctx context.Context, storage EventStorageEvent) error

type bus struct {
	onEventStorage *eventEmitter[OnEventStorage]
}

func newBus() *bus {
	return &bus{
		onEventStorage: newEventEmitter[OnEventStorage](),
	}
}

func (s *bus) dispatchOnEventStored(parent context.Context, domain, stream string, id int64, kind string, body json.RawMessage) {
	ctx, span := tracer.Start(parent, "service.dispatchOnEventStored")
	defer span.End()

	var err error
	s.onEventStorage.apply(func(e OnEventStorage) {
		err = errors.Join(err, e(ctx, EventStorageEvent{
			Domain: domain,
			Stream: stream,
			ID:     id,
			Kind:   kind,
			Body:   body,
		}))
	})
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		span.AddEvent("failure in dispatchOnEventStored")
	}
}

type eventListener[T any] struct {
	listener T
}

// todo: merge with the junk bucket implementation -- consider moving locking
type eventEmitter[T any] struct {
	state     *sync.Mutex
	listeners []*eventListener[T]
}

func newEventEmitter[T any]() *eventEmitter[T] {
	return &eventEmitter[T]{
		state: &sync.Mutex{},
	}
}

func (e *eventEmitter[T]) addListener(listener T) *eventListener[T] {
	e.state.Lock()
	defer e.state.Unlock()
	l := &eventListener[T]{listener}
	e.listeners = append(e.listeners, l)
	return l
}

func (e *eventEmitter[T]) removeListener(listener *eventListener[T]) {
	e.state.Lock()
	defer e.state.Unlock()

	e.listeners = fx.Filter(e.listeners, func(l *eventListener[T]) bool {
		return l == listener
	})
}

func (e *eventEmitter[T]) apply(f func(T)) {
	e.state.Lock()
	defer e.state.Unlock()
	for _, l := range e.listeners {
		f(l.listener)
	}
}
