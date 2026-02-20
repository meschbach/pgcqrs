package query2

import (
	"context"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

type Query struct {
	stream v1.StreamTransport
	kinds  []*KindClause
	ids    []*IDClause
}

func NewQuery(stream v1.StreamTransport) *Query {
	return &Query{stream: stream}
}

func (q *Query) OnKind(kind string) *KindClause {
	c := &KindClause{kind: kind}
	q.kinds = append(q.kinds, c)
	return c
}

func (q *Query) OnID(id int64) *IDClause {
	i := &IDClause{id: id}
	q.ids = append(q.ids, i)
	return i
}

type handlers struct {
	registered []v1.OnStreamQueryResult
}

func (h *handlers) register(processor v1.OnStreamQueryResult) int {
	id := len(h.registered)
	h.registered = append(h.registered, processor)
	return id
}

// registerOptional is a convenience to optionally register a handler.  If the processor is nil then nil is returned,
// otherwise the ID of the operation is returned.
func (h *handlers) registerOptional(processor v1.OnStreamQueryResult) *int {
	if processor == nil {
		return nil
	}
	id := h.register(processor)
	return &id
}

// StreamBatch issues the given query as a batch request to the underlying stream.  Meaning the interaction with the
// underlying data store happens in a single request.  Results processing will occur in a stream like semantic.
func (q *Query) StreamBatch(ctx context.Context) error {
	handlers, request, err := q.buildBatchRequest(ctx)
	if err != nil {
		return err
	}

	if request.Empty() {
		return nil
	}

	reply, err := q.stream.QueryBatchR2(ctx, request)
	if err != nil {
		return err
	}
	return q.processBatchResults(ctx, handlers, reply)
}

func (q *Query) buildBatchRequest(ctx context.Context) (*handlers, *v1.WireBatchR2Request, error) {
	handlers := &handlers{}
	request := &v1.WireBatchR2Request{}

	for _, c := range q.kinds {
		if err := c.prepareRequest(ctx, request, handlers); err != nil {
			return nil, nil, err
		}
	}
	for _, i := range q.ids {
		if err := i.prepareRequest(ctx, request, handlers); err != nil {
			return nil, nil, err
		}
	}
	return handlers, request, nil
}

func (q *Query) processBatchResults(ctx context.Context, handlers *handlers, reply *v1.WireBatchR2Result) error {
	for _, result := range reply.Results {
		handler := handlers.registered[result.Op]
		if err := handler(ctx, result.Envelope, result.Event); err != nil {
			return err
		}
	}
	return nil
}

// Watch performs the initial matching query, then will continue to watch for matching changes to be dispatched.
//
// Return values:
// * pump is the resulting pump to push matching elements from the remote service.
// * setup is non-nil if the system fails the initial query or setup of the vision
func (q *Query) Watch(ctx context.Context) (pump *Watch, setup error) {
	dispatcher := &handlers{}

	request := &ipc.QueryIn{}
	for _, c := range q.kinds {
		if err := c.prepareQuery(ctx, request, dispatcher); err != nil {
			return nil, err
		}
	}

	// Short circuit the request when there are no usable query elements
	// if request.Empty() {
	//	return nil
	// }

	wirePump, err := q.stream.Watch(ctx, request)
	if err != nil {
		return nil, err
	}
	pump = &Watch{
		handlers: dispatcher,
		wirePump: wirePump,
	}
	return pump, nil
}
