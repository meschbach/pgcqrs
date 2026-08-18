package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type memoryOp struct {
	done    chan interface{}
	command memoryCommand
}

type memoryCommand interface {
	perform(m *memory)
}

type memory struct {
	input         chan memoryOp
	domains       map[string]*memoryDomain
	positions     map[string]map[string]int64
	locks         map[string]map[string]map[string]*memoryLockState
	now           func() time.Time
	onLockRelease *emitter.MutexDispatcher[LockReleasedEvent]
	stopCh        chan struct{}
}

// ViewConnectivity reports that projections are served in-process.
func (m *memory) ViewConnectivity() ViewConnectivity {
	return ViewConnectivity{Memory: true}
}

type memoryLockState struct {
	holder         string
	acquiredAt     time.Time
	heartbeatAt    time.Time
	ttl            time.Duration
	guaranteeUntil time.Time
	heldUntil      time.Time
	expiryTimer    *time.Timer
	generation     int64
}

type memoryDomain struct {
	name    string
	streams map[string]*memoryStream
}

type memoryStream struct {
	name        string
	packets     []memoryPacket
	onAddPacket *emitter.MutexDispatcher[int64]
}

type memoryPacket struct {
	when time.Time
	kind string
	data []byte
}

type lockExpiryOp struct {
	domain     string
	stream     string
	consumer   string
	generation int64
}

func (op *lockExpiryOp) perform(m *memory) {
	existing := m.lockState(op.domain, op.stream, op.consumer)
	if existing == nil {
		return
	}
	// A heartbeat renewed the lock since this timer was scheduled.
	// The new timer will handle expiry; refuse to delete the fresh lock.
	if existing.generation != op.generation {
		return
	}
	// Timer-driven expiry runs outside any request context, so a failing
	// subscriber notification cannot be propagated. The lock is removed
	// regardless so waiters retry acquisition on the next cycle.
	//nolint:forbidigo // no request context exists for a timer-driven expiry
	if err := m.onLockRelease.Emit(context.Background(), LockReleasedEvent{
		Domain:   op.domain,
		Stream:   op.stream,
		Consumer: op.consumer,
		Holder:   existing.holder,
	}); err != nil {
		return
	}
	m.deleteLock(op.domain, op.stream, op.consumer)
}

func (m *memory) runService() {
	for {
		select {
		case op := <-m.input:
			op.command.perform(m)
			close(op.done)
		case <-m.stopCh:
			return
		}
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

// NewMemoryTransport creates a new in-memory Transport.
func NewMemoryTransport() Transport {
	m := &memory{
		input:         make(chan memoryOp, 32),
		domains:       make(map[string]*memoryDomain),
		positions:     make(map[string]map[string]int64),
		locks:         make(map[string]map[string]map[string]*memoryLockState),
		now:           time.Now,
		onLockRelease: emitter.NewMutexDispatcher[LockReleasedEvent](),
		stopCh:        make(chan struct{}),
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

func (m *memory) EnsureStream(ctx context.Context, domain, stream string) error {
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
				name:        stream,
				packets:     nil,
				onAddPacket: emitter.NewMutexDispatcher[int64](),
			}
		}
	}})
}

// lockState returns the lock state for a consumer, or nil if the consumer does
// not hold a lock for the given domain and stream.
func (m *memory) lockState(domain, stream, consumer string) *memoryLockState {
	streams, ok := m.locks[domain]
	if !ok {
		return nil
	}
	consumers, ok := streams[stream]
	if !ok {
		return nil
	}
	return consumers[consumer]
}

// setLock stores the lock state for a consumer, creating intermediate maps as
// needed.
func (m *memory) setLock(domain, stream, consumer string, state *memoryLockState) {
	streams, ok := m.locks[domain]
	if !ok {
		streams = make(map[string]map[string]*memoryLockState)
		m.locks[domain] = streams
	}
	consumers, ok := streams[stream]
	if !ok {
		consumers = make(map[string]*memoryLockState)
		streams[stream] = consumers
	}
	consumers[consumer] = state
}

// deleteLock removes the lock state for a consumer, pruning empty stream and
// domain entries.
func (m *memory) deleteLock(domain, stream, consumer string) {
	streams, ok := m.locks[domain]
	if !ok {
		return
	}
	consumers, ok := streams[stream]
	if !ok {
		return
	}
	delete(consumers, consumer)
	if len(consumers) == 0 {
		delete(streams, stream)
	}
}

func (m *memory) checkLock(domain, stream string, lock *Lock) error {
	if lock == nil {
		return nil
	}
	state := m.lockState(domain, stream, lock.Consumer)
	if state == nil {
		return &LockNotHeldError{
			Consumer: lock.Consumer,
			Holder:   lock.Holder,
			Domain:   domain,
			Stream:   stream,
		}
	}
	if m.now().After(state.heldUntil) {
		return &LockNotHeldError{
			Consumer: lock.Consumer,
			Holder:   lock.Holder,
			Domain:   domain,
			Stream:   stream,
		}
	}
	if state.holder != lock.Holder {
		return &LockNotHeldError{
			Consumer: lock.Consumer,
			Holder:   lock.Holder,
			Domain:   domain,
			Stream:   stream,
		}
	}
	return nil
}

func (m *memory) Submit(ctx context.Context, domain, stream, kind string, event interface{}, opts ...Option) (*Submitted, error) {
	cfg := &submitConfig{}
	for _, opt := range opts {
		opt.apply(cfg)
	}

	bytes, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	var out int64
	var lockErr error
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		lockErr = m.checkLock(domain, stream, cfg.lock)
		if lockErr != nil {
			return
		}

		stream := m.domains[domain].streams[stream]

		out = int64(len(stream.packets))
		packet := memoryPacket{
			kind: kind,
			when: time.Now(),
			data: bytes,
		}
		stream.packets = append(stream.packets, packet)
		_ = stream.onAddPacket.Emit(context.Background(), out)
	}}); err != nil {
		return nil, err
	}

	if lockErr != nil {
		return nil, lockErr
	}

	return &Submitted{
		ID: out,
	}, nil
}

func (m *memory) GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error {
	var data []byte
	missing := true // prevents a crash
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		d, ok := m.domains[domain]
		if !ok {
			return
		}
		s, ok := d.streams[stream]
		if !ok {
			return
		}
		if len(s.packets) <= int(id) {
			return
		}
		event := s.packets[id]
		data = event.data
		missing = false
	}}); err != nil {
		return err
	}
	if missing {
		return nil
	}
	return json.Unmarshal(data, event)
}

func (m *memory) GetEnvelope(ctx context.Context, domain, stream string, id int64) (Envelope, error) {
	var envelope Envelope
	var found bool
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		d, ok := m.domains[domain]
		if !ok {
			return
		}
		s, ok := d.streams[stream]
		if !ok {
			return
		}
		if len(s.packets) <= int(id) {
			return
		}
		p := s.packets[id]
		envelope = Envelope{
			ID:   id,
			When: FormatEnvelopeWhen(p.when),
			Kind: p.kind,
		}
		found = true
	}}); err != nil {
		return Envelope{}, err
	}
	if !found {
		return Envelope{}, nil
	}
	return envelope, nil
}

func (m *memory) AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error) {
	var envelopes []Envelope
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		event := m.domains[domain].streams[stream].packets
		for id, e := range event {
			envelopes = append(envelopes, Envelope{
				ID:   int64(id),
				When: FormatEnvelopeWhen(e.when),
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
		for name := range m.domains {
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
		if query.AfterID != nil && e.ID <= *query.AfterID {
			continue
		}
		if err := m.processEnvelopeForKinds(parent, domain, stream, query, e, out); err != nil {
			return err
		}
		if err := m.processEnvelopeForIDs(parent, domain, stream, query, e, out); err != nil {
			return err
		}
	}
	return nil
}

func (m *memory) processEnvelopeForKinds(parent context.Context, domain, stream string, query *WireBatchR2Request, e Envelope, out *WireBatchR2Result) error {
	for _, onKind := range query.OnKinds {
		if onKind.Kind != e.Kind {
			continue
		}
		if err := m.matchKindClause(parent, domain, stream, e, onKind, out); err != nil {
			return err
		}
	}
	return nil
}

func (m *memory) matchKindClause(parent context.Context, domain, stream string, e Envelope, onKind WireBatchR2KindQuery, out *WireBatchR2Result) error {
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
	return nil
}

func (m *memory) processEnvelopeForIDs(parent context.Context, domain, stream string, query *WireBatchR2Request, e Envelope, out *WireBatchR2Result) error {
	for _, onID := range query.OnID {
		if e.ID != onID.ID {
			continue
		}
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
	return nil
}

func (m *memory) Watch(ctx context.Context, query *ipc.QueryIn) (WatchInternal, error) {
	if err := ValidateQuery(query); err != nil {
		return nil, err
	}
	pendingEvents := make(chan int64, 128)

	initSetup := &errgroup.Group{}
	// Registers a listener for on added packets
	initSetup.Go(func() error {
		return m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
			stream := m.domains[query.Events.Domain].streams[query.Events.Stream]
			stream.onAddPacket.OnE(func(_ context.Context, id int64) error {
				select {
				case pendingEvents <- id:
				default:
					// Buffer full — consumer is stalled. Close to
					// signal the watch loop to stop cleanly rather
					// than blocking the memory service goroutine.
					close(pendingEvents)
				}
				return nil
			})
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
		lastEvent: -1,
	}, nil
}

type memoryWatch struct {
	// required
	core          *memory
	domain        string
	stream        string
	pendingEvents <-chan int64
	filter        *queryInFilter

	// pending holds events that have been enqueued but not yet returned by Tick.
	pending []*ipc.QueryOut

	// lastEvent is the highest event ID that has been enqueued. It is used
	// exclusively by enqueue() for deduplication: any envelope with an ID
	// less than or equal to lastEvent is silently skipped. This ensures
	// events are delivered exactly once even if processInitEvents or
	// processNewEvent processes overlapping envelopes.
	lastEvent int64

	// initDone tracks whether processInitEvents has run to completion at
	// least once. Before initDone is true, Tick calls processInitEvents
	// to catch up on events that existed before Watch was created. Once
	// initDone is set, processInitEvents is skipped on every subsequent
	// Tick, and only the pendingEvents channel (fed by onAddPacket
	// listeners registered in Watch) delivers new events.
	//
	// This flag decouples the init-phase gate from the dedup key
	// (lastEvent), avoiding the off-by-one that would occur if
	// lastEvent were compared against a count-based boundary.
	initDone bool
}

// enqueue adds an event to the pending queue, but only if its ID is
// greater than lastEvent. This deduplication guard ensures each event
// is delivered exactly once across both the init phase (processInitEvents)
// and the live phase (processNewEvent via pendingEvents channel). The
// caller is responsible for not calling enqueue with stale envelopes.
func (m *memoryWatch) enqueue(op int64, envelope Envelope, message json.RawMessage) {
	if envelope.ID <= m.lastEvent {
		return
	}
	m.lastEvent = envelope.ID

	whenTime, err := envelope.InternalizeWhen()
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

	if err := m.processInitEvents(ctx); err != nil {
		return nil, err
	}

	return m.waitForEvents(ctx)
}

// processInitEvents processes events that existed when Watch was created.
// On the first call it reads all envelopes from the stream, applies the
// query filter, and enqueues any that pass. Once complete it sets
// initDone to true so subsequent Ticks skip this function entirely.
// The enqueue() call handles deduplication via lastEvent, so overlapping
// envelopes are silently skipped.
func (m *memoryWatch) processInitEvents(ctx context.Context) error {
	if m.initDone {
		return nil
	}

	all, err := m.core.AllEnvelopes(ctx, m.domain, m.stream)
	if err != nil {
		return err
	}

	for _, e := range all {
		if err := m.filter.filter(ctx, e, func(op int64, envelope Envelope, message json.RawMessage) {
			m.enqueue(op, envelope, message)
		}); err != nil {
			return err
		}
	}
	m.initDone = true
	return nil
}

func (m *memoryWatch) waitForEvents(ctx context.Context) (*ipc.QueryOut, error) {
	for {
		if e := m.pop(); e != nil {
			return e, nil
		}

		select {
		case id := <-m.pendingEvents:
			if err := m.processNewEvent(ctx, id); err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// processNewEvent handles a single event arriving via the onAddPacket
// listener after Watch was created. It applies the query filter and
// enqueues matching events. Deduplication is handled by enqueue().
func (m *memoryWatch) processNewEvent(ctx context.Context, id int64) error {
	envelope, err := m.core.GetEnvelope(ctx, m.domain, m.stream, id)
	if err != nil {
		return err
	}
	if envelope.ID == 0 && id != 0 {
		return nil
	}
	if filterErr := m.filter.filter(ctx, envelope, func(op int64, envelope Envelope, message json.RawMessage) {
		m.enqueue(op, envelope, message)
	}); filterErr != nil {
		return filterErr
	}
	return nil
}

func (m *memory) positionKey(domain, stream, consumer string) string {
	return domain + "/" + stream + "/" + consumer
}

func (m *memory) SetPosition(ctx context.Context, domain, stream, consumer string, eventID int64) (*SetPositionResult, error) {
	var prevEventID *int64
	var setErr error
	err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		key := m.positionKey(domain, stream, consumer)
		if streamPositions, ok := m.positions[key]; ok {
			if currentID, exists := streamPositions[consumer]; exists {
				if eventID < currentID {
					setErr = fmt.Errorf("cannot set position backwards")
					return
				}
				prevEventID = &currentID
			}
		}
		if _, ok := m.positions[key]; !ok {
			m.positions[key] = make(map[string]int64)
		}
		m.positions[key][consumer] = eventID
	}})
	if err != nil {
		return nil, err
	}
	if setErr != nil {
		return nil, setErr
	}
	return &SetPositionResult{PreviousEventID: prevEventID, CurrentEventID: eventID}, nil
}

func (m *memory) GetPosition(ctx context.Context, domain, stream, consumer string) (eventID int64, found bool, err error) {
	err = m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		key := m.positionKey(domain, stream, consumer)
		if streamPositions, ok := m.positions[key]; ok {
			if id, ok := streamPositions[consumer]; ok {
				eventID = id
				found = true
			}
		}
	}})
	return eventID, found, err
}

func (m *memory) ListConsumers(ctx context.Context, domain, stream string) ([]string, error) {
	var consumers []string
	err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		prefix := domain + "/" + stream + "/"
		for key := range m.positions {
			if len(key) > len(prefix) && key[:len(prefix)] == prefix {
				consumer := key[len(prefix):]
				consumers = append(consumers, consumer)
			}
		}
	}})
	return consumers, err
}

func (m *memory) DeletePosition(ctx context.Context, domain, stream, consumer string) error {
	return m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		key := m.positionKey(domain, stream, consumer)
		if streamPositions, ok := m.positions[key]; ok {
			delete(streamPositions, consumer)
			if len(streamPositions) == 0 {
				delete(m.positions, key)
			}
		}
	}})
}

func (m *memory) cleanExpiredLocks(domain, stream, excludeConsumer string, now time.Time) []LockReleasedEvent {
	var events []LockReleasedEvent
	deleted := 0
	for consumer, l := range m.locks[domain][stream] {
		if consumer == excludeConsumer {
			continue
		}
		if deleted >= 128 {
			break
		}
		if now.After(l.heldUntil) {
			events = append(events, LockReleasedEvent{
				Domain:   domain,
				Stream:   stream,
				Consumer: consumer,
				Holder:   l.holder,
			})
			if l.expiryTimer != nil {
				l.expiryTimer.Stop()
			}
			m.deleteLock(domain, stream, consumer)
			deleted++
		}
	}
	return events
}

func (m *memory) TryAcquire(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (out *LockResult, retErr error) {
	ctx, span := tracer.Start(ctx, "memory.TryAcquire", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
		attribute.String("consumer-lock.holder", holder),
		attribute.Float64("consumer-lock.ttl", ttl.Seconds()),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		if out != nil {
			span.SetAttributes(attribute.Bool("consumer-lock.acquired", out.Acquired))
		}
		span.End()
	}()

	if ttl < LockMinimumTTL {
		return nil, &TTLTooLowError{Provided: ttl, Minimum: LockMinimumTTL}
	}

	var result *LockResult
	var expiredEvents []LockReleasedEvent
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		result, expiredEvents = m.tryAcquireInternal(domain, stream, consumer, holder, ttl)
	}}); err != nil {
		return nil, err
	}
	for _, evt := range expiredEvents {
		retErr = errors.Join(retErr, m.onLockRelease.Emit(ctx, evt))
	}
	return result, retErr
}

// tryAcquireInternal performs the acquisition logic on the memory service
// goroutine.
func (m *memory) tryAcquireInternal(domain, stream, consumer, holder string, ttl time.Duration) (*LockResult, []LockReleasedEvent) {
	now := m.now()

	if existing := m.lockState(domain, stream, consumer); existing != nil && !now.After(existing.heldUntil) && existing.holder != holder {
		return &LockResult{
			Acquired:       false,
			HeldBy:         existing.holder,
			GuaranteeUntil: existing.guaranteeUntil,
			HeldUntil:      existing.heldUntil,
			Position:       -1,
		}, nil
	}

	expiredEvents := m.cleanExpiredLocks(domain, stream, consumer, now)

	guaranteeUntil := now.Add(time.Duration(float64(ttl) * DefaultGuaranteeFraction))
	heldUntil := now.Add(ttl)

	newState := &memoryLockState{
		holder:         holder,
		acquiredAt:     now,
		heartbeatAt:    now,
		ttl:            ttl,
		guaranteeUntil: guaranteeUntil,
		heldUntil:      heldUntil,
		generation:     0,
	}
	newState.expiryTimer = time.AfterFunc(ttl, func() {
		m.input <- memoryOp{
			done:    make(chan interface{}),
			command: &lockExpiryOp{domain: domain, stream: stream, consumer: consumer, generation: 0},
		}
	})
	m.setLock(domain, stream, consumer, newState)

	var position int64
	if streamPositions, ok := m.positions[m.positionKey(domain, stream, consumer)]; ok {
		position = streamPositions[consumer]
	}

	return &LockResult{
		Acquired:       true,
		HeldBy:         holder,
		GuaranteeUntil: guaranteeUntil,
		HeldUntil:      heldUntil,
		Position:       position,
	}, expiredEvents
}

func (m *memory) Release(ctx context.Context, domain, stream, consumer, holder string) (retErr error) {
	ctx, span := tracer.Start(ctx, "memory.Release", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
		attribute.String("consumer-lock.holder", holder),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	var released bool
	var releaseErr error
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		released, releaseErr = m.releaseInternal(domain, stream, consumer, holder)
	}}); err != nil {
		return err
	}
	if releaseErr != nil {
		return releaseErr
	}
	if released {
		retErr = errors.Join(retErr, m.onLockRelease.Emit(ctx, LockReleasedEvent{
			Domain:   domain,
			Stream:   stream,
			Consumer: consumer,
			Holder:   holder,
		}))
	}
	return retErr
}

// releaseInternal performs the release logic on the memory service goroutine.
// Returns false when no active lock was held by the caller.
func (m *memory) releaseInternal(domain, stream, consumer, holder string) (bool, error) {
	state := m.lockState(domain, stream, consumer)
	if state == nil {
		return false, nil
	}
	if m.now().After(state.heldUntil) {
		return false, nil
	}
	if state.holder != holder {
		return false, &LockNotHeldError{
			Consumer: consumer,
			Holder:   holder,
			Domain:   domain,
			Stream:   stream,
		}
	}
	if state.expiryTimer != nil {
		state.expiryTimer.Stop()
	}
	m.deleteLock(domain, stream, consumer)
	return true, nil
}

func (m *memory) GetLock(ctx context.Context, domain, stream, consumer string) (out *LockState, found bool, retErr error) {
	ctx, span := tracer.Start(ctx, "memory.GetLock", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.SetAttributes(attribute.Bool("consumer-lock.found", out != nil))
		span.End()
	}()

	var state *LockState
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		existing := m.lockState(domain, stream, consumer)
		if existing == nil {
			return
		}
		if m.now().After(existing.heldUntil) {
			return
		}
		state = &LockState{
			Consumer:       consumer,
			Domain:         domain,
			Stream:         stream,
			Holder:         existing.holder,
			AcquiredAt:     existing.acquiredAt,
			HeartbeatAt:    existing.heartbeatAt,
			TTL:            existing.ttl,
			GuaranteeUntil: existing.guaranteeUntil,
			HeldUntil:      existing.heldUntil,
		}
	}}); err != nil {
		return nil, false, err
	}
	if state == nil {
		return nil, false, nil
	}
	return state, true, nil
}

func (m *memory) ListLocks(ctx context.Context, domain, stream string) (out []LockState, retErr error) {
	ctx, span := tracer.Start(ctx, "memory.ListLocks", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.SetAttributes(attribute.Int("consumer-lock.lock_count", len(out)))
		span.End()
	}()

	var states []LockState
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		now := m.now()
		for consumer, l := range m.locks[domain][stream] {
			if now.After(l.heldUntil) {
				continue
			}
			states = append(states, LockState{
				Consumer:       consumer,
				Domain:         domain,
				Stream:         stream,
				Holder:         l.holder,
				AcquiredAt:     l.acquiredAt,
				HeartbeatAt:    l.heartbeatAt,
				TTL:            l.ttl,
				GuaranteeUntil: l.guaranteeUntil,
				HeldUntil:      l.heldUntil,
			})
		}
	}}); err != nil {
		return nil, err
	}
	out = states
	return out, nil
}

func (m *memory) HeartbeatWithPosition(ctx context.Context, domain, stream, consumer, holder string, position int64) (retErr error) {
	ctx, span := tracer.Start(ctx, "memory.HeartbeatWithPosition", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", stream),
		attribute.String("consumer-lock.consumer", consumer),
		attribute.String("consumer-lock.holder", holder),
		attribute.Int64("consumer-lock.position", position),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	var hbErr error
	if err := m.simulateNetwork(ctx, &memoryFuncOp{func(m *memory) {
		hbErr = m.heartbeatLockAndPosition(domain, stream, consumer, holder, position)
	}}); err != nil {
		return err
	}
	return hbErr
}

func (m *memory) heartbeatLockAndPosition(domain, stream, consumer, holder string, position int64) error {
	now := m.now()

	existing := m.lockState(domain, stream, consumer)
	if existing == nil || now.After(existing.heldUntil) {
		return &LockExpiredError{
			Consumer: consumer,
			Domain:   domain,
			Stream:   stream,
		}
	}
	if existing.holder != holder {
		return &LockNotHeldError{
			Consumer: consumer,
			Holder:   holder,
			Domain:   domain,
			Stream:   stream,
		}
	}
	if err := m.checkPositionConflict(domain, stream, consumer, position); err != nil {
		return err
	}

	guaranteeUntil := now.Add(time.Duration(float64(existing.ttl) * DefaultGuaranteeFraction))
	heldUntil := now.Add(existing.ttl)

	existing.heartbeatAt = now
	existing.guaranteeUntil = guaranteeUntil
	existing.heldUntil = heldUntil
	existing.generation++

	// Reschedule the expiry timer
	if existing.expiryTimer != nil {
		existing.expiryTimer.Stop()
	}
	existing.expiryTimer = time.AfterFunc(existing.ttl, func() {
		m.input <- memoryOp{
			done:    make(chan interface{}),
			command: &lockExpiryOp{domain: domain, stream: stream, consumer: consumer, generation: existing.generation},
		}
	})

	posKey := m.positionKey(domain, stream, consumer)
	if _, ok := m.positions[posKey]; !ok {
		m.positions[posKey] = make(map[string]int64)
	}
	m.positions[posKey][consumer] = position
	return nil
}

// checkPositionConflict rejects heartbeats that move the consumer position
// backwards.
func (m *memory) checkPositionConflict(domain, stream, consumer string, position int64) error {
	streamPositions, ok := m.positions[m.positionKey(domain, stream, consumer)]
	if !ok {
		return nil
	}
	currentID, exists := streamPositions[consumer]
	if exists && position < currentID {
		return &HeartbeatConflictError{
			TargetVersion:  position,
			CurrentVersion: currentID,
		}
	}
	return nil
}

// OnLockRelease registers a callback to be invoked when a lock is released.
// The callback receives the lock release event.
func (m *memory) OnLockRelease(fn func(context.Context, LockReleasedEvent) error) func() {
	sub := m.onLockRelease.OnE(fn)
	return func() {
		m.onLockRelease.Off(sub)
	}
}

func (m *memory) Close() error {
	close(m.stopCh)
	return nil
}
