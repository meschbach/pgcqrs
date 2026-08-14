package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_TryAcquire_CleansExpiredLocks(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()

	pool := WithDatabaseConnection(t)
	store := NewConsumerStore(pool)
	createStreamForTest(ctx, t, pool, domain, stream)

	consumerID, err := store.resolveConsumerName(ctx, "expired-consumer")
	require.NoError(t, err)

	streamID := resolveStreamID(ctx, t, pool, domain, stream)

	for i := 0; i < 5; i++ {
		cID, err := store.resolveConsumerName(ctx, fmt.Sprintf("expired-consumer-%d", i))
		require.NoError(t, err)
		insertExpiredLock(ctx, t, pool, streamID, cID, fmt.Sprintf("holder-%d", i))
	}
	_ = consumerID

	result, _, err := store.TryAcquire(ctx, domain, stream, "new-consumer", "new-holder", 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Acquired)

	var expiredCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM consumer_locks cl
		JOIN consumer_names cn ON cl.consumer_id = cn.id
		WHERE cl.stream_id = $1 AND cn.name LIKE 'expired-consumer%'`,
		streamID).Scan(&expiredCount)
	require.NoError(t, err)
	assert.Equal(t, 0, expiredCount)
}

func TestConsumerStore_TryAcquire_ConcurrentAcquisition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()
	consumer := faker.Word()

	pool := WithDatabaseConnection(t)
	store := NewConsumerStore(pool)
	createStreamForTest(ctx, t, pool, domain, stream)

	const numGoroutines = 10
	results := make(chan *v1.LockResult, numGoroutines)
	errors := make(chan error, numGoroutines)
	start := make(chan struct{})

	for i := range numGoroutines {
		go func(holderID int) {
			<-start
			result, _, err := store.TryAcquire(ctx, domain, stream, consumer, fmt.Sprintf("holder-%d", holderID), 30*time.Second)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i)
	}

	close(start)

	acquired := 0
	conflicts := 0
	for range numGoroutines {
		select {
		case result := <-results:
			if result.Acquired {
				acquired++
			} else {
				conflicts++
			}
		case err := <-errors:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	assert.Equal(t, 1, acquired, "exactly one goroutine should acquire the lock")
	assert.Equal(t, numGoroutines-1, conflicts, "all other goroutines should get conflicts")
}
