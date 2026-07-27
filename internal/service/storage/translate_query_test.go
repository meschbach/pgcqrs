//go:build testcontainers_pg

package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateQuery_Execute(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "tq_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"k": "v1"})
	id2 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"k": "v2"})

	query := v1.WireQuery{
		KindConstraint: []v1.KindConstraint{
			{Kind: kind},
		},
	}
	sql := TranslateQuery(app, stream, query, false)

	rows, err := pool.Query(ctx, sql.DML, sql.Args...)
	require.NoError(t, err)
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id, new(interface{}), new(interface{})))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{id1, id2}, ids)
}

func TestTranslateQuery_KindEq(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "eqtest_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"status": "active"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"status": "inactive"})

	query := v1.WireQuery{
		KindConstraint: []v1.KindConstraint{
			{
				Kind: kind,
				Eq: []v1.WireMatcherV1{
					{
						Property: []string{"status"},
						Value:    []string{"active"},
					},
				},
			},
		},
	}
	sql := TranslateQuery(app, stream, query, true)

	rows, err := pool.Query(ctx, sql.DML, sql.Args...)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 1, count)
}

func TestTranslateQuery_KindMatchSubset(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "subtest_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "red", "size": "large"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"color": "blue", "size": "small"})

	query := v1.WireQuery{
		KindConstraint: []v1.KindConstraint{
			{
				Kind:        kind,
				MatchSubset: []byte(`{"color":"red"}`),
			},
		},
	}
	sql := TranslateQuery(app, stream, query, false)

	rows, err := pool.Query(ctx, sql.DML, sql.Args...)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 1, count)
}
