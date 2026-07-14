package v1

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

var truthful = true
var falsy = false
var yes = &truthful
var no = &falsy

// GrpcAdapter implements Transport using gRPC.
type GrpcAdapter struct {
	commands  ipc.CommandClient
	queries   ipc.QueryClient
	positions ipc.ConsumerPositionClient
	locks     ipc.ConsumerLockClient
}

// NewGRPCTransport creates a new GrpcAdapter.
func NewGRPCTransport(url string) (*GrpcAdapter, error) {
	var creds credentials.TransportCredentials
	if caFile, has := os.LookupEnv("PGCQRS_GRPC_CA"); has {
		// todo: figure out a better mechanism for secure transport
		certPath, err := filepath.Abs(caFile)
		if err != nil {
			return nil, err
		}
		// linting has been disabled as it flags the variable passed in via the variable as being a security flaw
		// in reality this should only be controlled by an operator.
		// nolint
		cert, err := os.ReadFile(certPath)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cert) {
			return nil, fmt.Errorf("unable to add certificate to pool")
		}
		config := &tls.Config{
			RootCAs: pool,
		}
		creds = credentials.NewTLS(config)
	} else {
		creds = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(creds), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, err
	}
	commands := ipc.NewCommandClient(conn)
	queries := ipc.NewQueryClient(conn)
	positions := ipc.NewConsumerPositionClient(conn)
	locks := ipc.NewConsumerLockClient(conn)
	return &GrpcAdapter{
		commands,
		queries,
		positions,
		locks,
	}, nil
}

// EnsureStream ensures the given stream exists via gRPC.
func (g *GrpcAdapter) EnsureStream(ctx context.Context, domain, stream string) error {
	_, err := g.commands.CreateStream(ctx, &ipc.CreateStreamIn{Target: &ipc.DomainStream{
		Domain: domain,
		Stream: stream,
	}})
	if err != nil {
		return err
	}
	return nil
}

// Submit sends an event via gRPC.
func (g *GrpcAdapter) Submit(ctx context.Context, domain, stream, kind string, event interface{}, opts ...Option) (*Submitted, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	cfg := &submitConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}

	submitIn := &ipc.SubmitIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Kind: kind,
		Body: body,
	}

	if cfg.lock != nil {
		submitIn.Lock = &ipc.Lock{
			Consumer: cfg.lock.Consumer,
			Holder:   cfg.lock.Holder,
		}
	}

	result, err := g.commands.Submit(ctx, submitIn)
	if err != nil {
		return nil, err
	}
	return &Submitted{ID: result.Id}, nil
}

// GetEvent retrieves a specific event via gRPC.
func (g *GrpcAdapter) GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error {
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

// AllEnvelopes returns all event envelopes via gRPC.
func (g *GrpcAdapter) AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error) {
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
			}
			return output, err
		}
		// Still wondering when this might happen if err == nil
		if out == nil {
			continue
		}
		//todo: really a protocol error, but be graceful on what we accept in?
		if out.Op != op {
			continue
		}
		output = append(output, Envelope{
			ID:   out.Envelope.Id,
			When: FormatEnvelopeWhen(out.Envelope.When.AsTime()),
			Kind: out.Envelope.Kind,
		})
	}
	return output, nil
}

// Query performs a query via gRPC.
func (g *GrpcAdapter) Query(ctx context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error {
	q, filtered := g.buildQueryRequest(domain, stream, query)

	result, err := g.queries.Query(ctx, q)
	if err != nil {
		return err
	}
	out.Filtered = filtered
	out.SubsetMatch = filtered
	return g.receiveQueryResults(result, out)
}

func (g *GrpcAdapter) buildQueryRequest(domain, stream string, query WireQuery) (*ipc.QueryIn, bool) {
	sendBack := &ipc.ResultInclude{
		Envelope: yes,
		Body:     no,
	}
	targetOpID := int64(42)

	q := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
	}
	filtered := true
	for index, onKind := range query.KindConstraint {
		// Not dealing with specific property equality right now
		if onKind.Eq != nil {
			filtered = false
			constraint := &ipc.OnKindClause{
				Kind:  onKind.Kind,
				AllOp: &targetOpID,
			}
			q.OnKind = append(q.OnKind, constraint)
			continue
		}

		constraint := g.buildKindConstraint(onKind, index, targetOpID, sendBack)
		q.OnKind = append(q.OnKind, constraint)

		if onKind.MatchSubset == nil && len(onKind.Eq) == 0 {
			constraint.AllOp = &targetOpID
		}
	}
	return q, filtered
}

func (g *GrpcAdapter) buildKindConstraint(onKind KindConstraint, index int, _ int64, sendBack *ipc.ResultInclude) *ipc.OnKindClause {
	constraint := &ipc.OnKindClause{
		Kind: onKind.Kind,
	}
	if onKind.MatchSubset != nil {
		constraint.Subsets = append(constraint.Subsets, &ipc.OnKindSubsetMatch{
			Match: onKind.MatchSubset,
			Op:    int64(index),
			Style: sendBack,
		})
	}
	return constraint
}

func (g *GrpcAdapter) receiveQueryResults(result ipc.Query_QueryClient, out *WireQueryResult) error {
	for {
		event, err := result.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		out.Matching = append(out.Matching, Envelope{
			ID:   event.Envelope.Id,
			When: FormatEnvelopeWhen(event.Envelope.When.AsTime()),
			Kind: event.Envelope.Kind,
		})
	}
	return nil
}

// QueryBatch performs a batch query via gRPC.
func (g *GrpcAdapter) QueryBatch(ctx context.Context, domain, stream string, query WireQuery, out *WireBatchResults) error {
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
			}
			return err
		}

		out.Page = append(out.Page, WireBatchResultPair{
			Meta: Envelope{
				ID:   *element.Id,
				When: FormatEnvelopeWhen(element.Envelope.When.AsTime()),
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

// QueryBatchR2 performs an R2 batch query via gRPC.
func (g *GrpcAdapter) QueryBatchR2(ctx context.Context, domain, stream string, batch *WireBatchR2Request, out *WireBatchR2Result) error {
	in, err := g.buildBatchR2Request(domain, stream, batch)
	if err != nil {
		return err
	}

	result, err := g.queries.Query(ctx, in)
	if err != nil {
		return err
	}
	return g.receiveBatchR2Results(result, out)
}

func (g *GrpcAdapter) buildBatchR2Request(domain, stream string, batch *WireBatchR2Request) (*ipc.QueryIn, error) {
	if batch.Empty() {
		return nil, nil
	}

	in := &ipc.QueryIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
	}

	for _, k := range batch.OnKinds {
		kindClause := g.buildBatchR2KindClause(k)
		in.OnKind = append(in.OnKind, kindClause)
	}
	for _, i := range batch.OnID {
		in.OnID = append(in.OnID, &ipc.OnIDClause{
			Id: i.ID,
			Op: int64(i.Op),
		})
	}
	if batch.AfterID != nil {
		in.AfterID = batch.AfterID
	}
	return in, nil
}

func (g *GrpcAdapter) buildBatchR2KindClause(k WireBatchR2KindQuery) *ipc.OnKindClause {
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
	return kindClause
}

func (g *GrpcAdapter) receiveBatchR2Results(result ipc.Query_QueryClient, out *WireBatchR2Result) error {
	for {
		element, err := result.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		out.Results = append(out.Results, WireBatchR2Dispatch{
			Envelope: Envelope{
				ID:   element.Envelope.Id,
				When: FormatEnvelopeWhen(element.Envelope.When.AsTime()),
				Kind: element.Envelope.Kind,
			},
			Event: element.Body,
			Op:    int(element.Op),
		})
	}
}

// Meta retrieves metadata via gRPC.
func (g *GrpcAdapter) Meta(ctx context.Context) (WireMetaV1, error) {
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

// SetPosition sets the consumer's position in a stream via gRPC.
func (g *GrpcAdapter) SetPosition(ctx context.Context, domain, stream, consumer string, eventID int64) (*SetPositionResult, error) {
	resp, err := g.positions.SetPosition(ctx, &ipc.SetPositionIn{
		Events:   &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer,
		EventID:  eventID,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Ok {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var prevID *int64
	if resp.GetPreviousEventID() != 0 {
		prevID = &resp.PreviousEventID
	}
	return &SetPositionResult{
		PreviousEventID: prevID,
		CurrentEventID:  resp.GetCurrentEventID(),
	}, nil
}

// GetPosition gets the consumer's position in a stream via gRPC.
func (g *GrpcAdapter) GetPosition(ctx context.Context, domain, stream, consumer string) (eventID int64, found bool, err error) {
	resp, err := g.positions.GetPosition(ctx, &ipc.GetPositionIn{
		Events:   &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer,
	})
	if err != nil {
		return 0, false, err
	}
	return resp.EventID, resp.Found, nil
}

// ListConsumers lists all consumers for a stream via gRPC.
func (g *GrpcAdapter) ListConsumers(ctx context.Context, domain, stream string) ([]string, error) {
	resp, err := g.positions.ListConsumers(ctx, &ipc.ListConsumersIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
	})
	if err != nil {
		return nil, err
	}
	return resp.Consumers, nil
}

// DeletePosition deletes the consumer's position in a stream via gRPC.
func (g *GrpcAdapter) DeletePosition(ctx context.Context, domain, stream, consumer string) error {
	_, err := g.positions.DeletePosition(ctx, &ipc.DeletePositionIn{
		Events:   &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer,
	})
	return err
}

// Watch sets up a watch via gRPC.
func (g *GrpcAdapter) Watch(ctx context.Context, query *ipc.QueryIn) (WatchInternal, error) {
	stream, err := g.queries.Watch(ctx, query)
	if err != nil {
		return nil, err
	}
	return &grpcWatchPump{wire: stream}, nil
}

// TryAcquire attempts to acquire a consumer lock via gRPC.
func (g *GrpcAdapter) TryAcquire(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*LockResult, error) {
	resp, err := g.locks.TryAcquire(ctx, &ipc.TryAcquireIn{
		Events:     &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer:   consumer,
		Holder:     holder,
		TtlSeconds: int32(ttl.Seconds()),
	})
	if err != nil {
		return nil, err
	}
	result := &LockResult{
		Acquired: resp.Acquired,
		HeldBy:   resp.HeldBy,
	}
	if resp.GuaranteeUntil != nil {
		result.GuaranteeUntil = resp.GuaranteeUntil.AsTime()
	}
	if resp.HeldUntil != nil {
		result.HeldUntil = resp.HeldUntil.AsTime()
	}
	return result, nil
}

// Release releases a consumer lock via gRPC.
func (g *GrpcAdapter) Release(ctx context.Context, domain, stream, consumer, holder string) error {
	_, err := g.locks.Release(ctx, &ipc.ReleaseIn{
		Events:   &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer,
		Holder:   holder,
	})
	return err
}

// GetLock retrieves the state of a consumer lock via gRPC.
// Uses ListLocks and filters by consumer since the proto has no dedicated GetLock RPC.
func (g *GrpcAdapter) GetLock(ctx context.Context, domain, stream, consumer string) (*LockState, error) {
	locks, err := g.ListLocks(ctx, domain, stream)
	if err != nil {
		return nil, err
	}
	for i := range locks {
		if locks[i].Consumer == consumer {
			return &locks[i], nil
		}
	}
	return nil, nil
}

// ListLocks lists all active locks for a domain/stream pair via gRPC.
func (g *GrpcAdapter) ListLocks(ctx context.Context, domain, stream string) ([]LockState, error) {
	resp, err := g.locks.ListLocks(ctx, &ipc.ListLocksIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
	})
	if err != nil {
		return nil, err
	}
	result := make([]LockState, 0, len(resp.Locks))
	for _, ls := range resp.Locks {
		state := LockState{
			Consumer: ls.Consumer,
			Domain:   ls.Domain,
			Stream:   ls.Stream,
			Holder:   ls.Holder,
			TTL:      time.Duration(ls.Ttl) * time.Second,
		}
		if ls.AcquiredAt != nil {
			state.AcquiredAt = ls.AcquiredAt.AsTime()
		}
		if ls.HeartbeatAt != nil {
			state.HeartbeatAt = ls.HeartbeatAt.AsTime()
		}
		if ls.GuaranteeUntil != nil {
			state.GuaranteeUntil = ls.GuaranteeUntil.AsTime()
		}
		if ls.HeldUntil != nil {
			state.HeldUntil = ls.HeldUntil.AsTime()
		}
		result = append(result, state)
	}
	return result, nil
}

// HeartbeatWithPosition sends a heartbeat with the consumer's position via gRPC.
// This is a unary wrapper that opens a KeepAlive stream, sends one heartbeat, and closes.
func (g *GrpcAdapter) HeartbeatWithPosition(ctx context.Context, domain, stream, consumer, holder string, position int64) error {
	kaStream, err := g.locks.KeepAlive(ctx)
	if err != nil {
		return err
	}
	err = kaStream.Send(&ipc.KeepAliveClientMessage{
		Message: &ipc.KeepAliveClientMessage_Heartbeat{
			Heartbeat: &ipc.KeepAliveHeartbeat{
				Events:   &ipc.DomainStream{Domain: domain, Stream: stream},
				Consumer: consumer,
				Holder:   holder,
				Position: position,
			},
		},
	})
	if err != nil {
		return err
	}
	resp, err := kaStream.Recv()
	if err != nil {
		return err
	}
	return g.handleKeepAliveResponse(resp, consumer, domain, stream, holder)
}

func (g *GrpcAdapter) handleKeepAliveResponse(resp *ipc.KeepAliveServerMessage, consumer, domain, stream, holder string) error {
	lockStatus := resp.GetLockStatus()
	if lockStatus == nil {
		return nil
	}
	if lockStatus.Reason == ipc.LockStatusReason_CONFLICT {
		return g.newHeartbeatConflictError(lockStatus)
	}
	if lockStatus.Reason == ipc.LockStatusReason_EXPIRED {
		return &LockExpiredError{Consumer: consumer, Domain: domain, Stream: stream}
	}
	if lockStatus.Reason == ipc.LockStatusReason_STOLEN {
		return &LockNotHeldError{Consumer: consumer, Domain: domain, Stream: stream, Holder: holder}
	}
	return nil
}

func (g *GrpcAdapter) newHeartbeatConflictError(lockStatus *ipc.KeepAliveLockStatus) error {
	var targetVersion, currentVersion int64
	if lockStatus.TargetVersion != nil {
		targetVersion = *lockStatus.TargetVersion
	}
	if lockStatus.CurrentVersion != nil {
		currentVersion = *lockStatus.CurrentVersion
	}
	return &HeartbeatConflictError{
		TargetVersion:  targetVersion,
		CurrentVersion: currentVersion,
	}
}

// KeepAlive wraps a bidirectional gRPC KeepAlive stream for synchronous
// heartbeat-based lock renewal. The client owns the goroutine and drives
// heartbeat timing via context.WithTimeout.
type KeepAlive struct {
	stream   grpc.BidiStreamingClient[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage]
	domain   string
	streamN  string
	consumer string
	holder   string
}

// NewKeepAlive opens a bidirectional KeepAlive stream and returns a client-side
// wrapper for synchronous heartbeat and release operations.
func (g *GrpcAdapter) NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (*KeepAlive, error) {
	s, err := g.locks.KeepAlive(ctx)
	if err != nil {
		return nil, err
	}
	return &KeepAlive{
		stream:   s,
		domain:   domain,
		streamN:  stream,
		consumer: consumer,
		holder:   holder,
	}, nil
}

// Heartbeat sends a heartbeat with the consumer's current position over the
// bidirectional stream and waits for the server's lock status response.
// Returns nil on RENEWED, or an error if the reason is CONFLICT, EXPIRED, or STOLEN.
func (k *KeepAlive) Heartbeat(ctx context.Context, position int64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	err := k.stream.Send(&ipc.KeepAliveClientMessage{
		Message: &ipc.KeepAliveClientMessage_Heartbeat{
			Heartbeat: &ipc.KeepAliveHeartbeat{
				Events:   &ipc.DomainStream{Domain: k.domain, Stream: k.streamN},
				Consumer: k.consumer,
				Holder:   k.holder,
				Position: position,
			},
		},
	})
	if err != nil {
		return err
	}
	resp, err := k.stream.Recv()
	if err != nil {
		return err
	}
	return k.processLockStatus(resp)
}

func (k *KeepAlive) processLockStatus(resp *ipc.KeepAliveServerMessage) error {
	lockStatus := resp.GetLockStatus()
	if lockStatus == nil {
		return nil
	}
	switch lockStatus.Reason {
	case ipc.LockStatusReason_UNSPECIFIED, ipc.LockStatusReason_RENEWED:
		return nil
	case ipc.LockStatusReason_CONFLICT:
		var targetVersion, currentVersion int64
		if lockStatus.TargetVersion != nil {
			targetVersion = *lockStatus.TargetVersion
		}
		if lockStatus.CurrentVersion != nil {
			currentVersion = *lockStatus.CurrentVersion
		}
		return &HeartbeatConflictError{
			TargetVersion:  targetVersion,
			CurrentVersion: currentVersion,
		}
	case ipc.LockStatusReason_EXPIRED:
		return &LockExpiredError{
			Consumer: k.consumer,
			Domain:   k.domain,
			Stream:   k.streamN,
		}
	case ipc.LockStatusReason_STOLEN:
		return &LockNotHeldError{
			Consumer: k.consumer,
			Holder:   k.holder,
			Domain:   k.domain,
			Stream:   k.streamN,
		}
	}
	return nil
}

// Release sends a release request over the bidirectional stream and waits for
// the server's release acknowledgement. Returns nil on success, or an error if
// the ack indicates failure.
func (k *KeepAlive) Release(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	err := k.stream.Send(&ipc.KeepAliveClientMessage{
		Message: &ipc.KeepAliveClientMessage_ReleaseRequest{
			ReleaseRequest: &ipc.KeepAliveReleaseRequest{},
		},
	})
	if err != nil {
		return err
	}
	resp, err := k.stream.Recv()
	if err != nil {
		return err
	}
	ack := resp.GetReleaseAck()
	if ack == nil {
		return nil
	}
	if !ack.Ok {
		return fmt.Errorf("keepalive release rejected")
	}
	return nil
}

type grpcWatchPump struct {
	wire grpc.ServerStreamingClient[ipc.QueryOut]
}

func (g *grpcWatchPump) Tick(_ context.Context) (msg *ipc.QueryOut, err error) {
	msg, err = g.wire.Recv()
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
			return nil, context.Canceled
		}
	}
	return msg, err
}
