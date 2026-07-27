package storage

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	f "github.com/meschbach/pgcqrs/pkg/junk/faking"
	"github.com/stretchr/testify/require"
)

var domainUniqueness = f.NewUniqueKebab()

func resolveStreamID(ctx context.Context, t *testing.T, pool *pgxpool.Pool, domain, stream string) int64 {
	t.Helper()
	var streamID int64
	err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
	require.NoError(t, err)
	return streamID
}

func insertExpiredLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, streamID, consumerID int64, holder string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO consumer_locks (stream_id, consumer_id, holder, ttl, guarantee_until, held_until)
		VALUES ($1, $2, $3, '30s', NOW() - '10s'::interval, NOW() - '1s'::interval)
		ON CONFLICT (stream_id, consumer_id) DO UPDATE
		SET holder = EXCLUDED.holder, held_until = EXCLUDED.held_until`,
		streamID, consumerID, holder)
	require.NoError(t, err)
}
