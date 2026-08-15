//go:build testcontainers_pg

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// mockLockGrantedStream implements grpc.ServerStreamingServer[ipc.LockGranted]
type mockLockGrantedStream struct {
	grpc.ServerStream
	ctx  context.Context
	mu   sync.Mutex
	sent []*ipc.LockGranted
}

func (m *mockLockGrantedStream) Send(msg *ipc.LockGranted) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockLockGrantedStream) Context() context.Context {
	return m.ctx
}

func (m *mockLockGrantedStream) Snapshot() []*ipc.LockGranted {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ipc.LockGranted, len(m.sent))
	copy(out, m.sent)
	return out
}

func createStreamForLockTest(ctx context.Context, t *testing.T, pool *pgxpool.Pool, domain, stream string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO events_stream (app, stream)
		VALUES ($1, $2)
		ON CONFLICT (app, stream) DO NOTHING`, domain, stream)
	require.NoError(t, err)
}

func TestGrpcConsumerLock_WaitForLock_AcquiresImmediately(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	// Ensure stream exists
	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	req := &ipc.WaitForLockRequest{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer:   consumer,
		Holder:     holder,
		TtlSeconds: 30,
	}

	streamMock := &mockLockGrantedStream{ctx: ctx}
	err := lockService.WaitForLock(req, streamMock)
	require.NoError(t, err)

	sent := streamMock.Snapshot()
	require.Len(t, sent, 1)
	assert.Greater(t, sent[0].HeartbeatIntervalMs, int64(0))
	assert.Greater(t, sent[0].DeadlineMs, int64(0))
	assert.Equal(t, int64(0), sent[0].Position, "fresh consumer should have position 0")
}

func TestGrpcConsumerLock_WaitForLock_BlocksUntilReleased(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder1 := faker.Word()
	holder2 := faker.Word()

	// Ensure stream exists
	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	// holder1 acquires the lock
	req1 := &ipc.WaitForLockRequest{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer:   consumer,
		Holder:     holder1,
		TtlSeconds: 30,
	}
	stream1 := &mockLockGrantedStream{ctx: ctx}
	err := lockService.WaitForLock(req1, stream1)
	require.NoError(t, err)
	require.Len(t, stream1.Snapshot(), 1)

	// holder2 tries to acquire - should block
	done := make(chan error, 1)
	stream2 := &mockLockGrantedStream{ctx: ctx}
	go func() {
		req2 := &ipc.WaitForLockRequest{
			Events: &ipc.DomainStream{
				Domain: domain,
				Stream: stream,
			},
			Consumer:   consumer,
			Holder:     holder2,
			TtlSeconds: 30,
		}
		done <- lockService.WaitForLock(req2, stream2)
	}()

	// Wait a bit to ensure holder2 is blocked
	time.Sleep(100 * time.Millisecond)

	// Release holder1's lock
	_, err = lockService.Release(ctx, &ipc.ReleaseIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer: consumer,
		Holder:   holder1,
	})
	require.NoError(t, err)

	// holder2 should now have acquired the lock
	select {
	case err := <-done:
		require.NoError(t, err)
		sent := stream2.Snapshot()
		require.Len(t, sent, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("holder2 did not acquire lock after holder1 released")
	}
}

func TestGrpcConsumerLock_LockLost_PushOnRelease(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	// Ensure stream exists
	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	// Acquire the lock
	_, err := lockService.TryAcquire(ctx, &ipc.TryAcquireIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer:   consumer,
		Holder:     holder,
		TtlSeconds: 30,
	})
	require.NoError(t, err)

	// Subscribe to lock release events
	releaseCh := make(chan LockReleasedEvent, 1)
	sub := bus.onLockRelease.OnE(func(_ context.Context, evt LockReleasedEvent) error {
		select {
		case releaseCh <- evt:
		default:
		}
		return nil
	})
	defer bus.onLockRelease.Off(sub)

	// Release the lock
	_, err = lockService.Release(ctx, &ipc.ReleaseIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer: consumer,
		Holder:   holder,
	})
	require.NoError(t, err)

	// Verify we received the release event
	select {
	case evt := <-releaseCh:
		assert.Equal(t, domain, evt.Domain)
		assert.Equal(t, stream, evt.Stream)
		assert.Equal(t, consumer, evt.Consumer)
		assert.Equal(t, holder, evt.Holder)
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive lock release event")
	}
}

func TestGrpcConsumerLock_TryAcquire_ReturnsPosition(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	// Set a position before acquiring
	_, err := lockService.consumerStore.SetPosition(ctx, domain, stream, consumer, 42)
	require.NoError(t, err)

	// Acquire and verify position is returned
	out, err := lockService.TryAcquire(ctx, &ipc.TryAcquireIn{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer:   consumer,
		Holder:     holder,
		TtlSeconds: 30,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.True(t, out.Acquired)
	assert.Equal(t, int64(42), out.Position, "TryAcquire should return stored position")
}

func TestGrpcConsumerLock_TryAcquire_ConflictReturnsSentinel(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder1 := faker.Word()
	holder2 := faker.Word()

	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	// holder1 acquires
	_, err := lockService.TryAcquire(ctx, &ipc.TryAcquireIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer, Holder: holder1, TtlSeconds: 30,
	})
	require.NoError(t, err)

	// holder2 conflicts
	out, err := lockService.TryAcquire(ctx, &ipc.TryAcquireIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer, Holder: holder2, TtlSeconds: 30,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.False(t, out.Acquired)
	assert.Equal(t, int64(-1), out.Position, "conflict should return sentinel -1")
}

func TestGrpcConsumerLock_WaitForLock_ReturnsStoredPosition(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	// Set position via heartbeat (requires lock held)
	acquireOut, err := lockService.TryAcquire(ctx, &ipc.TryAcquireIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer, Holder: holder, TtlSeconds: 30,
	})
	require.NoError(t, err)
	require.True(t, acquireOut.Acquired)

	err = lockService.consumerStore.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 99)
	require.NoError(t, err)

	// Release and re-acquire via WaitForLock
	_, err = lockService.Release(ctx, &ipc.ReleaseIn{
		Events: &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer: consumer, Holder: holder,
	})
	require.NoError(t, err)

	streamMock := &mockLockGrantedStream{ctx: ctx}
	err = lockService.WaitForLock(&ipc.WaitForLockRequest{
		Events:     &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer:   consumer,
		Holder:     holder,
		TtlSeconds: 30,
	}, streamMock)
	require.NoError(t, err)

	sent := streamMock.Snapshot()
	require.Len(t, sent, 1)
	assert.Equal(t, int64(99), sent[0].Position, "WaitForLock should return stored position")
}

func TestGrpcConsumerLock_WaitForLock_BlocksUntilReleased_PropagatesPosition(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder1 := faker.Word()
	holder2 := faker.Word()

	createStreamForLockTest(ctx, t, pool, domain, stream)

	bus := newBus()
	lockService := &grpcConsumerLock{
		consumerStore: storage2.NewConsumerStore(pool),
		bus:           bus,
	}

	// holder1 acquires and heartbeats a position
	stream1 := &mockLockGrantedStream{ctx: ctx}
	err := lockService.WaitForLock(&ipc.WaitForLockRequest{
		Events:     &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer:   consumer,
		Holder:     holder1,
		TtlSeconds: 30,
	}, stream1)
	require.NoError(t, err)

	err = lockService.consumerStore.HeartbeatWithPosition(ctx, domain, stream, consumer, holder1, 55)
	require.NoError(t, err)

	// holder2 blocks
	done := make(chan error, 1)
	stream2 := &mockLockGrantedStream{ctx: ctx}
	go func() {
		done <- lockService.WaitForLock(&ipc.WaitForLockRequest{
			Events:     &ipc.DomainStream{Domain: domain, Stream: stream},
			Consumer:   consumer,
			Holder:     holder2,
			TtlSeconds: 30,
		}, stream2)
	}()

	time.Sleep(100 * time.Millisecond)

	// holder1 releases
	_, err = lockService.Release(ctx, &ipc.ReleaseIn{
		Events:     &ipc.DomainStream{Domain: domain, Stream: stream},
		Consumer:   consumer,
		Holder:     holder1,
	})
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
		sent := stream2.Snapshot()
		require.Len(t, sent, 1)
		assert.Equal(t, int64(55), sent[0].Position, "holder2 should receive holder1's last position")
	case <-time.After(5 * time.Second):
		t.Fatal("holder2 did not acquire lock after holder1 released")
	}
}
