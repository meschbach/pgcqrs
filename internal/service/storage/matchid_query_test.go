//go:build testcontainers_pg

package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchID_Found(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "idtest_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"x": "1"})
	id2 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"x": "2"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"x": "3"})

	op := WithMatchID(app, stream, id2, 0)
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	var foundID int64
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&foundID, new(interface{}), new(interface{}), new(interface{}), new(interface{})))
	assert.Equal(t, id2, foundID)
	assert.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestMatchID_NotFound(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()

	op := WithMatchID(app, stream, 999999, 0)
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	assert.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestMatchID_IgnoresAfterID(t *testing.T) {
	t.Parallel()

	op := &matchID{app: "a", stream: "s", id: 42, op: 0}
	op.UpdateAfterID(100)
	assert.Equal(t, int64(42), op.id)
}
