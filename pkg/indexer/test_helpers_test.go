package indexer

import (
	"context"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
)

type mockTransport struct {
	locks map[string]bool
}

func newMockTransport() *mockTransport {
	return &mockTransport{locks: make(map[string]bool)}
}

func (m *mockTransport) TryAcquire(_ context.Context, domain, stream, consumer, holder string, _ time.Duration) (*v1.LockResult, error) {
	key := domain + "/" + stream + "/" + consumer
	if m.locks[key] {
		return &v1.LockResult{Acquired: false, HeldBy: "other-holder", Position: -1}, nil
	}
	m.locks[key] = true
	return &v1.LockResult{Acquired: true, HeldBy: holder, Position: 0}, nil
}

func (m *mockTransport) Release(_ context.Context, domain, stream, consumer, _ string) error {
	key := domain + "/" + stream + "/" + consumer
	delete(m.locks, key)
	return nil
}

func (m *mockTransport) GetPosition(_ context.Context, _, _, _ string) (position int64, ok bool, err error) {
	return 0, false, nil
}

func (m *mockTransport) HeartbeatWithPosition(_ context.Context, _, _, _, _ string, _ int64) error {
	return nil
}

func (m *mockTransport) OnLockRelease(_ func(context.Context, v1.LockReleasedEvent) error) func() {
	return func() {}
}

type mockLock struct {
	transport *mockTransport
	domain    string
	stream    string
	consumer  string
	holder    string
}

func (l *mockLock) Heartbeat(ctx context.Context, position int64) error {
	return l.transport.HeartbeatWithPosition(ctx, l.domain, l.stream, l.consumer, l.holder, position)
}

func (l *mockLock) Release(ctx context.Context) error {
	return l.transport.Release(ctx, l.domain, l.stream, l.consumer, l.holder)
}

type mockWire struct {
	transport *mockTransport
}

func newMockWire(t *mockTransport) *mockWire {
	return &mockWire{transport: t}
}

func (w *mockWire) WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*mockLock, int64, time.Duration, error) {
	result, err := w.transport.TryAcquire(ctx, domain, stream, consumer, holder, ttl)
	if err != nil {
		return nil, 0, 0, err
	}
	if !result.Acquired {
		return nil, 0, 0, context.DeadlineExceeded
	}
	heartbeatInterval := time.Duration(float64(ttl) * 0.9)
	return &mockLock{
		transport: w.transport,
		domain:    domain,
		stream:    stream,
		consumer:  consumer,
		holder:    holder,
	}, result.Position, heartbeatInterval, nil
}

type testIndexer struct {
	stream v1.StreamTransport
}

func newMockIndexer(stream v1.StreamTransport) *testIndexer {
	return &testIndexer{stream: stream}
}

func (m *testIndexer) Query() *query2.Query {
	if m.stream == nil {
		return query2.NewQuery(nil)
	}
	return query2.NewQuery(m.stream)
}
