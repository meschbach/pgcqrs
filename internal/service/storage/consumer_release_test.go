package storage

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_Release(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("ExplicitRelease", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		err = store.Release(ctx, domain, stream, consumer, holder)
		require.NoError(t, err)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})

	t.Run("IdempotentRelease", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		err := store.Release(ctx, domain, stream, consumer, holder)
		require.NoError(t, err)
	})

	t.Run("NonHolderRejection", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		otherHolder := faker.Word()
		err = store.Release(ctx, domain, stream, consumer, otherHolder)
		require.Error(t, err)
		var lockNotHeld *v1.LockNotHeldError
		require.ErrorAs(t, err, &lockNotHeld)
		assert.Equal(t, consumer, lockNotHeld.Consumer)
		assert.Equal(t, otherHolder, lockNotHeld.Holder)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, state)
		assert.Equal(t, holder, state.Holder)
	})
}
