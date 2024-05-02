package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"google.golang.org/grpc"
	"io"
	"time"
)

var truthful = true
var falsy = false
var yes = &truthful
var no = &falsy

type grpcAdapter struct {
	commands ipc.CommandClient
	queries  ipc.QueryClient
}

func newGrpcAdapter(url string) (*grpcAdapter, error) {
	//todo: figure out a better mechanism for secure transport
	conn, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	commands := ipc.NewCommandClient(conn)
	queries := ipc.NewQueryClient(conn)
	return &grpcAdapter{
		commands,
		queries,
	}, nil
}

func (g *grpcAdapter) EnsureStream(ctx context.Context, domain string, stream string) error {
	_, err := g.commands.CreateStream(ctx, &ipc.CreateStreamIn{Target: &ipc.DomainStream{
		Domain: domain,
		Stream: stream,
	}})
	if err != nil {
		return err
	}
	return nil
}

func (g *grpcAdapter) Submit(ctx context.Context, domain, stream, kind string, event interface{}) (*Submitted, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	result, err := g.commands.Submit(ctx, &ipc.SubmitIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Kind: kind,
		Body: body,
	})
	if err != nil {
		return nil, err
	}
	return &Submitted{ID: result.Id}, nil
}

func (g *grpcAdapter) GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error {
	result, err := g.queries.Get(ctx, &ipc.GetIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Id: id,
	})
	if err != nil {
		return err
	}
	body := result.Payload
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	return nil
}

func (g *grpcAdapter) AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error) {
	op := int64(42)
	result, err := g.queries.Query(ctx, &ipc.QueryIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		OnEach: &ipc.OnEachEvent{
			Op: op,
			Style: &ipc.ResultInclude{
				Envelope: yes,
				Body:     no,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var output []Envelope
	for {
		out, err := result.Recv()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return output, err
			}
		}
		//still wondering when this might happen if err == nil
		if out == nil {
			continue
		}
		//todo: really a protocol error, but be graceful on what we accept in?
		if out.Op != op {
			continue
		}
		output = append(output, Envelope{
			ID:   out.Envelope.Id,
			When: out.Envelope.When.String(),
			Kind: out.Envelope.Kind,
		})
	}
	return output, nil
}

func (g *grpcAdapter) Query(ctx context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error {
	sendBack := &ipc.ResultInclude{
		Envelope: yes,
		Body:     no,
	}

	q := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
	}
	filtered := true
	for index, onKind := range query.KindConstraint {
		//not dealing with specific property equality right now
		if onKind.Eq != nil {
			filtered = false
			continue
		}

		constraint := &ipc.OnKindClause{
			Kind: onKind.Kind,
		}
		q.OnKind = append(q.OnKind, constraint)
		if onKind.MatchSubset != nil {
			constraint.Subsets = append(constraint.Subsets, &ipc.OnKindSubsetMatch{
				Match: onKind.MatchSubset,
				Op:    int64(index),
				Style: sendBack,
			})
		}
	}

	result, err := g.queries.Query(ctx, q)
	if err != nil {
		return err
	}
	out.Filtered = filtered
	out.SubsetMatch = true
	for {
		event, err := result.Recv()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}
		out.Matching = append(out.Matching, Envelope{
			ID:   event.Envelope.Id,
			When: event.Envelope.When.String(),
			Kind: event.Envelope.Kind,
		})
	}
	return nil
}

func (g *grpcAdapter) QueryBatch(ctx context.Context, domain, stream string, query WireQuery, out *WireBatchResults) error {
	fmt.Printf("Query %#v\n", query)
	op := int64(42)
	in := &ipc.QueryIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
	}
	for _, k := range query.KindConstraint {
		clause := &ipc.OnKindClause{
			Kind: k.Kind,
		}
		if k.MatchSubset == nil {
			clause.AllOp = &op
		} else {
			clause.Subsets = append(clause.Subsets, &ipc.OnKindSubsetMatch{
				Match: k.MatchSubset,
				Op:    op,
			})
		}

		in.OnKind = append(in.OnKind, clause)
	}

	result, err := g.queries.Query(ctx, in)
	if err != nil {
		return err
	}
	for {
		element, err := result.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			} else {
				return err
			}
		}

		out.Page = append(out.Page, WireBatchResultPair{
			Meta: Envelope{
				ID:   *element.Id,
				When: element.Envelope.When.AsTime().Format(time.RFC3339Nano),
				Kind: element.Envelope.Kind,
			},
			Data: element.Body,
		})
	}
}

func grpcMaybeWireOp(maybeOp *int) *int64 {
	if maybeOp == nil {
		return nil
	}
	deReferenced := *maybeOp
	extended := int64(deReferenced)
	return &extended
}

func (g *grpcAdapter) QueryBatchR2(ctx context.Context, domain, stream string, batch *WireBatchR2Request, out *WireBatchR2Result) error {
	in := &ipc.QueryIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
	}
	if batch.Empty() {
		return nil
	}

	for _, k := range batch.OnKinds {
		kindClause := &ipc.OnKindClause{
			Kind: k.Kind,
		}
		if k.All != nil {
			kindClause.AllOp = grpcMaybeWireOp(k.All)
		}
		for _, match := range k.Match {
			subset := &ipc.OnKindSubsetMatch{
				Match: match.Subset,
				Op:    int64(match.Op),
			}
			kindClause.Subsets = append(kindClause.Subsets, subset)
		}
		in.OnKind = append(in.OnKind, kindClause)
	}
	for _, i := range batch.OnID {
		in.OnID = append(in.OnID, &ipc.OnIDClause{
			Id: i.ID,
			Op: int64(i.Op),
		})
	}

	result, err := g.queries.Query(ctx, in)
	if err != nil {
		return err
	}
	for {
		element, err := result.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			} else {
				return err
			}
		}

		out.Results = append(out.Results, WireBatchR2Dispatch{
			Envelope: Envelope{
				ID:   element.Envelope.Id,
				When: element.Envelope.When.AsTime().Format(time.RFC3339Nano),
				Kind: element.Envelope.Kind,
			},
			Event: element.Body,
			Op:    int(element.Op),
		})
	}
}

func (g *grpcAdapter) Meta(ctx context.Context) (WireMetaV1, error) {
	result := WireMetaV1{}

	domains := make(map[string]*WireMetaDomainV1)

	out, err := g.queries.ListStreams(ctx, &ipc.ListStreamsIn{})
	if err != nil {
		return result, err
	}

	for _, stream := range out.Target {
		d, has := domains[stream.Domain]
		if !has {
			d = &WireMetaDomainV1{
				Name: stream.Domain,
			}
			domains[stream.Domain] = d
		}
		d.Streams = append(d.Streams, stream.Stream)
	}

	for _, d := range domains {
		result.Domains = append(result.Domains, *d)
	}
	return result, nil
}
