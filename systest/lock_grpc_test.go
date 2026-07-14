package systest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lockHarness struct {
	ctx        context.Context
	done       func()
	system     *v1.System
	appName    string
	streamName string
}

func setupLockHarnessT(t *testing.T) *lockHarness {
	t.Helper()
	if os.Getenv("PGCQRS_TEST_TRANSPORT") != "grpc" {
		t.Skip("Skipping gRPC integration test - PGCQRS_TEST_TRANSPORT not set to grpc")
	}

	ctx, done := context.WithCancel(t.Context())
	transport := os.Getenv("PGCQRS_TEST_TRANSPORT")
	url := os.Getenv("PGCQRS_TEST_URL")
	appBase := os.Getenv("PGCQRS_TEST_APP_BASE")

	appName := appBase + "-" + faker.Name()
	streamName := faker.Name()

	config := v1.Config{
		TransportType: transport,
		ServiceURL:    url,
	}
	system, err := config.SystemFromConfig()
	require.NoError(t, err)
	_, err = system.Stream(ctx, appName, streamName)
	require.NoError(t, err)

	out := &lockHarness{
		ctx:        ctx,
		done:       done,
		system:     system,
		appName:    appName,
		streamName: streamName,
	}
	t.Cleanup(done)
	return out
}

func TestGRPCLockAcquireAndRelease(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	transport := harness.system.Transport
	consumer := faker.Word()
	holder := faker.Word()

	result, err := transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Acquired)
	assert.Equal(t, holder, result.HeldBy)

	err = transport.Release(ctx, harness.appName, harness.streamName, consumer, holder)
	require.NoError(t, err)
}

func TestGRPCLockConflict(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	transport := harness.system.Transport
	consumer := faker.Word()
	holder1 := faker.Word()
	holder2 := faker.Word()

	_, err := transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder1, 30*time.Second)
	require.NoError(t, err)

	result, err := transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder2, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Acquired)
	assert.Equal(t, holder1, result.HeldBy)
}

func TestGRPCLockListLocks(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	transport := harness.system.Transport

	_, err := transport.TryAcquire(ctx, harness.appName, harness.streamName, "consumer-a", "holder-a", 30*time.Second)
	require.NoError(t, err)
	_, err = transport.TryAcquire(ctx, harness.appName, harness.streamName, "consumer-b", "holder-b", 30*time.Second)
	require.NoError(t, err)

	locks, err := transport.ListLocks(ctx, harness.appName, harness.streamName)
	require.NoError(t, err)
	assert.Len(t, locks, 2)
}

func TestGRPCHeartbeatWithPosition(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	transport := harness.system.Transport
	consumer := faker.Word()
	holder := faker.Word()

	_, err := transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	err = transport.HeartbeatWithPosition(ctx, harness.appName, harness.streamName, consumer, holder, 42)
	require.NoError(t, err)

	pos, found, err := transport.GetPosition(ctx, harness.appName, harness.streamName, consumer)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(42), pos)
}

func TestGRPCSubmitWithLock(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	transport := harness.system.Transport
	consumer := faker.Word()
	holder := faker.Word()

	_, err := transport.TryAcquire(ctx, harness.appName, harness.streamName, consumer, holder, 30*time.Second)
	require.NoError(t, err)

	lock := v1.NewLock(consumer, holder)
	result, err := transport.Submit(ctx, harness.appName, harness.streamName, "test-kind", map[string]string{"v": "1"}, lock)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGRPCSubmitWithExpiredLock(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	transport := harness.system.Transport
	consumer := faker.Word()
	holder := faker.Word()

	lock := v1.NewLock(consumer, holder)
	_, err := transport.Submit(ctx, harness.appName, harness.streamName, "test-kind", map[string]string{"v": "1"}, lock)
	require.Error(t, err)
}

func TestGRPCKeepAliveBidirectional(t *testing.T) {
	t.Parallel()

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	mem, ok := harness.system.Transport.(*v1.GrpcAdapter)
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

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	mem, ok := harness.system.Transport.(*v1.GrpcAdapter)
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

	harness := setupLockHarnessT(t)
	defer harness.done()

	ctx := harness.ctx
	mem, ok := harness.system.Transport.(*v1.GrpcAdapter)
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
