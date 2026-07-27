package systest

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	holder1 := faker.Word()
	holder2 := faker.Word()

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

func TestGRPCKeepAliveFirstHeartbeatMismatchedHolderReturnsStolen(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	harness := setupHarnessT(t)

	ctx := harness.ctx
	mem, ok := harness.transport.(*v1.GrpcAdapter)
	require.True(t, ok)
	consumer := faker.Word()
	realHolder := faker.Word()
	wrongHolder := faker.Word()

	_, err := mem.TryAcquire(ctx, harness.appName, harness.streamName, consumer, realHolder, 30*time.Second)
	require.NoError(t, err)

	ka, err := mem.NewKeepAlive(ctx, harness.appName, harness.streamName, consumer, wrongHolder)
	require.NoError(t, err)

	err = ka.Heartbeat(ctx, 1)
	require.Error(t, err)
	var lockErr *v1.LockNotHeldError
	require.ErrorAs(t, err, &lockErr)
}
