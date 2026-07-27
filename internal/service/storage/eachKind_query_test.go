//go:build testcontainers_pg

package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEachKind_BasicFilter(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kindA := "alpha_" + faker.Word()
	kindB := "beta_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kindA, map[string]string{"v": "a1"})
	insertEvent(ctx, t, pool, app, stream, kindB, map[string]string{"v": "b1"})
	insertEvent(ctx, t, pool, app, stream, kindA, map[string]string{"v": "a2"})

	op := &EachKind{App: app, Stream: stream, Op: 0, Kind: kindA}
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	var kinds []string
	for rows.Next() {
		var id int64
		var kind string
		require.NoError(t, rows.Scan(&id, new(interface{}), new(interface{}), new(interface{}), &kind))
		kinds = append(kinds, kind)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{kindA, kindA}, kinds)
}

func TestEachKind_AfterID(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "cursor_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]int{"n": 1})
	id2 := insertEvent(ctx, t, pool, app, stream, kind, map[string]int{"n": 2})
	id3 := insertEvent(ctx, t, pool, app, stream, kind, map[string]int{"n": 3})

	op := &EachKind{App: app, Stream: stream, Op: 0, Kind: kind, AfterID: id2}
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
	assert.Equal(t, []int64{id3}, ids)
}

func TestEachKind_NoMatch(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "only_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"v": "x"})

	op := &EachKind{App: app, Stream: stream, Op: 0, Kind: "nonexistent_" + faker.Word()}
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
