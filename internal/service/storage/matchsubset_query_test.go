//go:build testcontainers_pg

package storage

import (
	"encoding/json"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchSubset_ContainsJSONB(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "subset_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "red", "size": "large"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "blue", "size": "small"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "red", "size": "small"})

	subset, err := json.Marshal(map[string]string{"color": "red"})
	require.NoError(t, err)
	op := &MatchSubset{App: app, Stream: stream, Op: 0, Kind: kind, Subset: subset}
	q := &SQLQuery{first: true}
	op.append(q)

	rows, err := pool.Query(ctx, q.DML, q.Args...)
	require.NoError(t, err)
	defer rows.Close()

	var matched []map[string]string
	for rows.Next() {
		var (
			id   int64
			kind string
			raw  json.RawMessage
		)
		require.NoError(t, rows.Scan(&id, new(interface{}), new(interface{}), &raw, &kind))
		var m map[string]string
		require.NoError(t, json.Unmarshal(raw, &m))
		matched = append(matched, m)
	}
	require.NoError(t, rows.Err())
	require.Len(t, matched, 2)
	assert.Equal(t, "red", matched[0]["color"])
	assert.Equal(t, "red", matched[1]["color"])
}

func TestMatchSubset_NestedObject(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "nested_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]interface{}{
		"user": map[string]string{"name": "alice", "role": "admin"},
	})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]interface{}{
		"user": map[string]string{"name": "bob", "role": "user"},
	})

	subset, err := json.Marshal(map[string]interface{}{
		"user": map[string]string{"role": "admin"},
	})
	require.NoError(t, err)
	op := &MatchSubset{App: app, Stream: stream, Op: 0, Kind: kind, Subset: subset}
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
	assert.Equal(t, 1, count)
}

func TestMatchSubset_NoMatch(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "nomatch_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "blue"})

	subset, err := json.Marshal(map[string]string{"color": "red"})
	require.NoError(t, err)
	op := &MatchSubset{App: app, Stream: stream, Op: 0, Kind: kind, Subset: subset}
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

func TestMatchSubset_AfterID(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "subcursor_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "red"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "blue"})
	id3 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "red"})

	subset, err := json.Marshal(map[string]string{"color": "red"})
	require.NoError(t, err)
	op := &MatchSubset{App: app, Stream: stream, Op: 0, Kind: kind, Subset: subset, AfterID: id1}
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
