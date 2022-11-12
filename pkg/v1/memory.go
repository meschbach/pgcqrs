package v1

import (
	"context"
	"encoding/json"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
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
		stream.packets = append(stream.packets, memoryPacket{
			kind: kind,
			when: time.Now(),
			data: bytes,
		})
	}}); err != nil {
		return nil, nil
	}

	return &Submitted{
		ID: out,
	}, nil
}

func (m *memory) GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error {
	var data []byte
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		event := m.domains[domain].streams[stream].packets[id]
		data = event.data
	}}); err != nil {
		return err
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
