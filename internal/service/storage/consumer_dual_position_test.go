package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_PositionDualRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()

	t.Run("GetPositionReadsPreMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 100, NOW())`, streamID, "legacy-consumer")
		require.NoError(t, err)

		pos, found, err := store.GetPosition(ctx, domain, stream, "legacy-consumer")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(100), pos)
	})

	t.Run("GetPositionReadsPostMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, "modern-consumer", 200)
		require.NoError(t, err)

		pos, found, err := store.GetPosition(ctx, domain, stream, "modern-consumer")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(200), pos)
	})

	t.Run("GetPositionReturnsNotFoundForUnknownConsumer", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		pos, found, err := store.GetPosition(ctx, domain, stream, "nonexistent")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, int64(0), pos)
	})

	t.Run("ListConsumersMergesBothPaths", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 10, NOW())`, streamID, "legacy-only")
		require.NoError(t, err)

		_, err = store.SetPosition(ctx, domain, stream, "modern-only", 20)
		require.NoError(t, err)

		consumers, err := store.ListConsumers(ctx, domain, stream)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"legacy-only", "modern-only"}, consumers)
	})

	t.Run("DeletePositionWorksWithPreMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 10, NOW())`, streamID, "to-delete")
		require.NoError(t, err)

		err = store.DeletePosition(ctx, domain, stream, "to-delete")
		require.NoError(t, err)

		_, found, err := store.GetPosition(ctx, domain, stream, "to-delete")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("DeletePositionWorksWithPostMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, "to-delete-modern", 10)
		require.NoError(t, err)

		err = store.DeletePosition(ctx, domain, stream, "to-delete-modern")
		require.NoError(t, err)

		_, found, err := store.GetPosition(ctx, domain, stream, "to-delete-modern")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("SetPositionPromotesLegacyRowToDualWrite", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 10, NOW())`, streamID, "promoted")
		require.NoError(t, err)

		var consumerIDBefore *int64
		err = pool.QueryRow(ctx, `
			SELECT consumer_id FROM consumer_positions
			WHERE stream_id = $1 AND consumer = $2`, streamID, "promoted").Scan(&consumerIDBefore)
		require.NoError(t, err)
		assert.Nil(t, consumerIDBefore, "pre-migration row should have NULL consumer_id")

		_, err = store.SetPosition(ctx, domain, stream, "promoted", 20)
		require.NoError(t, err)

		var consumerIDAfter *int64
		err = pool.QueryRow(ctx, `
			SELECT consumer_id FROM consumer_positions
			WHERE stream_id = $1 AND consumer = $2`, streamID, "promoted").Scan(&consumerIDAfter)
		require.NoError(t, err)
		assert.NotNil(t, consumerIDAfter, "after SetPosition, consumer_id should be populated")
	})
}
