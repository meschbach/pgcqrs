//go:build testcontainers_pg

package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllStreamEvents_ReturnsAll(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kindA := "all_a_" + faker.Word()
	kindB := "all_b_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kindA, map[string]string{"n": "1"})
	id2 := insertEvent(ctx, t, pool, app, stream, kindB, map[string]string{"n": "2"})
	id3 := insertEvent(ctx, t, pool, app, stream, kindA, map[string]string{"n": "3"})

	op := &AllStreamEvents{Domain: app, Stream: stream, Op: 0}
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id, new(interface{}), new(interface{}), new(interface{}), new(interface{})))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{id1, id2, id3}, ids)
}

func TestAllStreamEvents_AfterID(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "allcursor_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "1"})
	id2 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "2"})
	id3 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "3"})

	op := &AllStreamEvents{Domain: app, Stream: stream, Op: 0, AfterID: id1}
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id, new(interface{}), new(interface{}), new(interface{}), new(interface{})))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{id2, id3}, ids)
}

func TestAllStreamEvents_Empty(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()

	op := &AllStreamEvents{Domain: app, Stream: stream, Op: 0}
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	require.NoError(t, rows.Err())
	assert.Zero(t, count)
}
