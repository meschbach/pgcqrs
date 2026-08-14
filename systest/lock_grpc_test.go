package systest

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestGRPCLockAcquireAndRelease(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	consumer := faker.Word()
	holder := faker.Word()

	result, err := harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Acquired)
	assert.Equal(t, holder, result.HeldBy)

	err = harness.transport.Release(ctx, harness.appName, harness.streamName, consumer, holder)
	require.NoError(t, err)
}

func TestGRPCLockConflict(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	consumer := faker.Word()
	holder1 := faker.Word() + "-1"
	holder2 := faker.Word() + "-2"

	_, err := harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder1, 30*time.Second)
	require.NoError(t, err)

	result, err := harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder2, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Acquired)
	assert.Equal(t, holder1, result.HeldBy)
}

func TestGRPCLockListLocks(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx

	_, err := harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, "consumer-a", "holder-a", 30*time.Second)
	require.NoError(t, err)
	_, err = harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, "consumer-b", "holder-b", 30*time.Second)
	require.NoError(t, err)

	locks, err := harness.transport.ListLocks(ctx, harness.appName, harness.streamName)
	require.NoError(t, err)
	assert.Len(t, locks, 2)
}

func TestGRPCHeartbeatWithPosition(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	consumer := faker.Word()
	holder := faker.Word()

	_, err := harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	err = harness.transport.HeartbeatWithPosition(ctx, harness.appName, harness.streamName, consumer, holder, 42)
	require.NoError(t, err)

	pos, found, err := harness.transport.GetPosition(ctx, harness.appName, harness.streamName, consumer)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(42), pos)
}

func TestGRPCSubmitWithLock(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	consumer := faker.Word()
	holder := faker.Word()

	_, err := harness.transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	lock := v1.NewLock(consumer, holder)
	result, err := harness.transport.Submit(ctx, harness.appName, harness.streamName, "test-kind", map[string]string{"v": "1"}, lock)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGRPCSubmitWithExpiredLock(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	consumer := faker.Word()
	holder := faker.Word()

	lock := v1.NewLock(consumer, holder)
	_, err := harness.transport.Submit(ctx, harness.appName, harness.streamName, "test-kind", map[string]string{"v": "1"}, lock)
	require.Error(t, err)
}

func TestGRPCKeepAliveBidirectional(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	holder := faker.Word()

	_, err := mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, holder)
	require.NoError(t, err)

	err = ka.Heartbeat(ctx, 10)
	require.NoError(t, err)

	pos, found, err := mem.GetPosition(ctx, harness.appName, harness.streamName, consumer)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(10), pos)

	err = ka.Release(ctx)
	require.NoError(t, err)
}

func TestGRPCKeepAliveFirstHeartbeatValidatesHolder(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	holder := faker.Word()

	_, err := mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, holder)
	require.NoError(t, err)

	err = ka.Heartbeat(ctx, 1)
	require.NoError(t, err)

	err = ka.Release(ctx)
	require.NoError(t, err)
}

// TestGRPCWaitForLock_BlocksUntilReleased verifies that WaitForLock blocks until
// the lock is released and that the second waiter then acquires it.
func TestGRPCWaitForLock_BlocksUntilReleased(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	consumer := faker.Word()
	holder1 := faker.Word() + "-1"
	holder2 := faker.Word() + "-2"

	// Create two completely independent connections
	conn1, err := grpc.NewClient(harness.serviceURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn1.Close()) })

	conn2, err := grpc.NewClient(harness.serviceURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn2.Close()) })

	wire1 := v1.NewGrpcWire(conn1)
	wire2 := v1.NewGrpcWire(conn2)

	// holder1 acquires the lock
	lock1, _, err := wire1.WaitForLock(harness.ctx, harness.appName, harness.streamName, consumer, holder1, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, lock1)

	// holder2 tries to acquire - should block
	done := make(chan error, 1)
	var lock2 *v1.KeepAlive
	go func() {
		var err error
		lock2, _, err = wire2.WaitForLock(harness.ctx, harness.appName, harness.streamName, consumer, holder2, 30*time.Second)
		done <- err
	}()

	// Give holder2 a moment to reach the waiting state
	time.Sleep(200 * time.Millisecond)

	// Release holder1's lock
	err = lock1.Release(harness.ctx)
	require.NoError(t, err)

	// holder2 should now have acquired the lock
	select {
	case err := <-done:
		require.NoError(t, err)
		require.NotNil(t, lock2)
		err = lock2.Release(harness.ctx)
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("holder2 did not acquire lock after holder1 released")
	}
}

// TestKeepAlive_BindEstablishesContext verifies that the bind message establishes
// the lock context on the server so Release works without any heartbeat.
func TestKeepAlive_BindEstablishesContext(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	holder := faker.Word()

	_, err := mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, holder)
	require.NoError(t, err)
	require.NotNil(t, ka)

	lock, found, err := mem.GetLock(ctx, harness.appName, harness.streamName, consumer)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, holder, lock.Holder)

	err = ka.Release(ctx)
	require.NoError(t, err)
}

// TestKeepAlive_ReleaseWithoutHeartbeat is a regression test for the bug where
// Release() called before any heartbeat silently failed because the server had
// no lock context.
func TestKeepAlive_ReleaseWithoutHeartbeat(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	holder := faker.Word()

	_, err := mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, holder)
	require.NoError(t, err)

	// Release immediately without sending any heartbeat
	err = ka.Release(ctx)
	require.NoError(t, err)

	// The lock should actually be released
	_, found, err := mem.GetLock(ctx, harness.appName, harness.streamName, consumer)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestKeepAlive_BindValidatesLockExists verifies the server rejects a bind for a
// lock that does not exist.
func TestKeepAlive_BindValidatesLockExists(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	holder := faker.Word()

	// Never acquire the lock, so the bind must fail
	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, holder)
	require.Error(t, err)
	require.Nil(t, ka)
	var lockErr *v1.LockExpiredError
	require.ErrorAs(t, err, &lockErr)
}

// TestKeepAlive_UnauthorizedRelease_ReturnsError verifies that only the lock
// holder can bind and release a lock.
func TestKeepAlive_UnauthorizedRelease_ReturnsError(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	holder1 := faker.Word() + "-1"
	holder2 := faker.Word() + "-2"

	_, err := mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder1, 30*time.Second)
	require.NoError(t, err)

	// A different holder cannot bind, so the stream is never established
	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, holder2)
	require.Error(t, err)
	require.Nil(t, ka)
	var lockErr *v1.LockNotHeldError
	require.ErrorAs(t, err, &lockErr)
}

// TestKeepAlive_MessageBeforeBind_ReturnsError verifies that sending a message
// before the bind message is rejected and closes the stream.
func TestKeepAlive_MessageBeforeBind_ReturnsError(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	ctx := harness.ctx
	conn, err := grpc.NewClient(harness.serviceURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	lockClient := ipc.NewConsumerLockClient(conn)
	stream, err := lockClient.KeepAlive(ctx)
	require.NoError(t, err)

	// Send a heartbeat without first binding
	err = stream.Send(&ipc.KeepAliveClientMessage{
		Message: &ipc.KeepAliveClientMessage_Heartbeat{
			Heartbeat: &ipc.KeepAliveHeartbeat{
				Events:   &ipc.DomainStream{Domain: harness.appName, Stream: harness.streamName},
				Consumer: faker.Word(),
				Holder:   faker.Word(),
			},
		},
	})
	require.NoError(t, err)

	// The server should close the stream with an InvalidArgument error
	_, err = stream.Recv()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestKeepAlive_Rebind_ReturnsError verifies that a second bind message is
// rejected and closes the stream.
func TestKeepAlive_Rebind_ReturnsError(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)
	ctx := harness.ctx
	conn, err := grpc.NewClient(harness.serviceURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	consumer := faker.Word()
	holder := faker.Word()

	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	_, err = mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	lockClient := ipc.NewConsumerLockClient(conn)
	stream, err := lockClient.KeepAlive(ctx)
	require.NoError(t, err)

	sendBind := func() error {
		return stream.Send(&ipc.KeepAliveClientMessage{
			Message: &ipc.KeepAliveClientMessage_Bind{
				Bind: &ipc.KeepAliveBindRequest{
					Domain:   harness.appName,
					Stream:   harness.streamName,
					Consumer: consumer,
					Holder:   holder,
				},
			},
		})
	}

	// First bind should succeed
	err = sendBind()
	require.NoError(t, err)
	resp, err := stream.Recv()
	require.NoError(t, err)
	bindStatus := resp.GetLockStatus()
	require.NotNil(t, bindStatus)
	assert.True(t, bindStatus.Locked)

	// Second bind should be rejected
	err = sendBind()
	require.NoError(t, err)
	_, err = stream.Recv()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
