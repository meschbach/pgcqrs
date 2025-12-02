package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type memoryOp struct {
	done    chan interface{}
	command memoryCommand
}

type memoryCommand interface {
	perform(m *memory)
}

type memory struct {
	input   chan memoryOp
	domains map[string]*memoryDomain
}

type memoryDomain struct {
	name    string
	streams map[string]*memoryStream
}

type memoryStream struct {
	name    string
	packets []memoryPacket
	//todo: convert to SyncEmitter
	onAddPacket []func(int64)
}

type memoryPacket struct {
	when time.Time
	kind string
	data []byte
}

func (m *memory) runService() {
	for op := range m.input {
		op.command.perform(m)
		close(op.done)
	}
}

func (m *memory) simulateNetwork(ctx context.Context, cmd memoryCommand) error {
	done := make(chan interface{})
	m.input <- memoryOp{
		done:    done,
		command: cmd,
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func NewMemoryTransport() Transport {
	m := &memory{
		input:   make(chan memoryOp, 32),
		domains: make(map[string]*memoryDomain),
	}
	go m.runService()
	return m
}

type memoryFuncOp struct {
	do func(m *memory)
}

func (m *memoryFuncOp) perform(sys *memory) {
	m.do(sys)
}

func (m *memory) EnsureStream(ctx context.Context, domain string, stream string) error {
	return m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		if _, hasDomain := m.domains[domain]; !hasDomain {
			m.domains[domain] = &memoryDomain{
				name:    domain,
				streams: make(map[string]*memoryStream),
			}
		}
		domain := m.domains[domain]

		if _, hasStream := domain.streams[stream]; !hasStream {
			domain.streams[stream] = &memoryStream{
				name:    stream,
				packets: nil,
			}
		}
	}})
}

func (m *memory) Submit(ctx context.Context, domain, stream, kind string, event interface{}) (*Submitted, error) {
	bytes, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	//
	var out int64
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		stream := m.domains[domain].streams[stream]

		out = int64(len(stream.packets))
		packet := memoryPacket{
			kind: kind,
			when: time.Now(),
			data: bytes,
		}
		stream.packets = append(stream.packets, packet)
		for _, onAdd := range stream.onAddPacket {
			onAdd(out)
		}
	}}); err != nil {
		return nil, nil
	}

	return &Submitted{
		ID: out,
	}, nil
}

func (m *memory) GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error {
	var data []byte
	missing := true //prevents a crash
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		if d, ok := m.domains[domain]; !ok {
			return
		} else {
			if s, ok := d.streams[stream]; !ok {
				return
			} else {
				if len(s.packets) <= int(id) {
					return
				}
				event := s.packets[id]
				data = event.data
				missing = false
			}
		}
	}}); err != nil {
		return err
	}
	if missing {
		return nil
	}
	return json.Unmarshal(data, event)
}

func (m *memory) AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error) {
	var envelopes []Envelope
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		event := m.domains[domain].streams[stream].packets
		for id, e := range event {
			envelopes = append(envelopes, Envelope{
				ID:   int64(id),
				When: e.when.Format(time.StampMilli),
				Kind: e.kind,
			})
		}
	}}); err != nil {
		return nil, err
	}
	return envelopes, nil
}

type memoryFilterLoader struct {
	m      *memory
	domain string
	stream string
}

func (m *memoryFilterLoader) Get(ctx context.Context, id int64, payload interface{}) error {
	return m.m.GetEvent(ctx, m.domain, m.stream, id, payload)
}

func (m *memory) Query(ctx context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error {
	envelopes, err := m.AllEnvelopes(ctx, domain, stream)
	if err != nil {
		return err
	}

	out.Filtered = true
	out.SubsetMatch = true
	out.Matching = nil
	filterLoader := &memoryFilterLoader{
		m:      m,
		domain: domain,
		stream: stream,
	}
	for _, e := range envelopes {
		add, err := filter(ctx, filterLoader, query, e)
		if err != nil {
			return err
		}

		if add {
			out.Matching = append(out.Matching, e)
		}
	}
	return nil
}

func (m *memory) Meta(parent context.Context) (WireMetaV1, error) {
	var out WireMetaV1
	err := m.simulateNetwork(parent, &memoryFuncOp{func(m *memory) {
		for name, _ := range m.domains {
			out.Domains = append(out.Domains, WireMetaDomainV1{
				Name:    name,
				Streams: nil,
			})
		}
	}})
	return out, err
}

func (m *memory) QueryBatchR2(parent context.Context, domain, stream string, query *WireBatchR2Request, out *WireBatchR2Result) error {
	envelopes, err := m.AllEnvelopes(parent, domain, stream)
	if err != nil {
		return err
	}

	for _, e := range envelopes {
		for _, onKind := range query.OnKinds {
			if onKind.Kind == e.Kind {
				if onKind.All != nil {
					var data json.RawMessage
					if err := m.GetEvent(parent, domain, stream, e.ID, &data); err != nil {
						return err
					}
					out.Results = append(out.Results, WireBatchR2Dispatch{
						Envelope: e,
						Event:    data,
						Op:       *onKind.All,
					})
				}
				for _, match := range onKind.Match {
					var data json.RawMessage
					if err := m.GetEvent(parent, domain, stream, e.ID, &data); err != nil {
						return err
					}
					if local.JSONIsSubset(data, match.Subset) {
						out.Results = append(out.Results, WireBatchR2Dispatch{
							Envelope: e,
							Event:    data,
							Op:       match.Op,
						})
					}
				}
			}
		}
		for _, onID := range query.OnID {
			if e.ID == onID.ID {
				var data json.RawMessage
				if err := m.GetEvent(parent, domain, stream, e.ID, &data); err != nil {
					return err
				}
				out.Results = append(out.Results, WireBatchR2Dispatch{
					Envelope: e,
					Event:    data,
					Op:       onID.Op,
				})
			}
		}
	}
	return nil
}

func (m *memory) Watch(ctx context.Context, query *ipc.QueryIn) (WatchInternal, error) {
	pendingEvents := make(chan int64, 128)
	var initEventEnd int64

	initSetup := &errgroup.Group{}
	//registers a listener for on added packets
	initSetup.Go(func() error {
		return m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
			stream := m.domains[query.Events.Domain].streams[query.Events.Stream]
			stream.onAddPacket = append(stream.onAddPacket, func(id int64) {
				fmt.Printf("onAddPacket: %d\n", id)
				pendingEvents <- id
			})
		}})
	})

	initSetup.Go(func() error {
		return m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
			stream := m.domains[query.Events.Domain].streams[query.Events.Stream]
			initEventEnd = int64(len(stream.packets))
		}})
	})

	if err := initSetup.Wait(); err != nil {
		//todo: better cleanup -- this should cleanup in a more sane way
		close(pendingEvents)
		return nil, err
	}

	return &memoryWatch{
		core:          m,
		domain:        query.Events.Domain,
		stream:        query.Events.Stream,
		pendingEvents: pendingEvents,
		filter: &queryInFilter{
			core:  m,
			query: query,
		},
		initEventEnd: initEventEnd,
	}, nil
}

type memoryWatch struct {
	// required
	core          *memory
	domain        string
	stream        string
	pendingEvents <-chan int64
	filter        *queryInFilter

	//internal state
	pending      []*ipc.QueryOut
	lastEvent    int64
	initEventEnd int64
}

func (m *memoryWatch) enqueue(op int64, envelope Envelope, message json.RawMessage) {
	if envelope.ID < m.lastEvent {
		return
	}
	m.lastEvent = envelope.ID

	fmt.Printf("enqueuing: (op: %d, event: %d)\n", op, envelope.ID)
	whenTime, err := time.Parse(time.StampMilli, envelope.When)
	if err != nil {
		panic(err)
	}

	event := &ipc.QueryOut{
		Op: op,
		Id: &envelope.ID,
		Envelope: &ipc.MaterializedEnvelope{
			Id:   envelope.ID,
			When: timestamppb.New(whenTime),
			Kind: envelope.Kind,
		},
		Body: message,
	}
	m.pending = append(m.pending, event)
}

func (m *memoryWatch) pop() *ipc.QueryOut {
	if len(m.pending) == 0 {
		return nil
	}
	out := m.pending[0]
	m.pending = m.pending[1:]
	return out
}

func (m *memoryWatch) Tick(ctx context.Context) (*ipc.QueryOut, error) {
	if e := m.pop(); e != nil {
		return e, nil
	}

	if m.lastEvent < m.initEventEnd {
		all, err := m.core.AllEnvelopes(ctx, m.domain, m.stream)
		if err != nil {
			return nil, err
		}

		for _, e := range all {
			if err := m.filter.filter(ctx, e, func(op int64, envelope Envelope, message json.RawMessage) {
				m.enqueue(op, envelope, message)
			}); err != nil {
				return nil, err
			}
			m.lastEvent = e.ID
		}
		m.lastEvent = m.initEventEnd
	}

	for {
		if e := m.pop(); e != nil {
			fmt.Printf("\tPopped: %v\n", e)
			return e, nil
		}

		select {
		case id := <-m.pendingEvents:
			fmt.Printf("\tGot event: %d\n", id)
			envelopes, err := m.core.AllEnvelopes(ctx, m.domain, m.stream)
			if err != nil {
				fmt.Printf("\tget all failed: %d\n", id)
				return nil, err
			}
			envelope := envelopes[id]
			if err = m.filter.filter(ctx, envelope, func(op int64, envelope Envelope, message json.RawMessage) {
				fmt.Printf("\tmatch received: %d\n", id)
				m.enqueue(op, envelope, message)
			}); err != nil {
				fmt.Printf("\tfilter failed: %d\n", id)
				return nil, err
			}
			fmt.Printf("\tpost-filter: (count: %d)\n", len(m.pending))
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
