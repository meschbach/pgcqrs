//go:build testcontainers_pg

package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryStream_UnionAllComposition(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kindA := "union_a_" + faker.Word()
	kindB := "union_b_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kindA, map[string]string{"n": "1"})
	id2 := insertEvent(ctx, t, pool, app, stream, kindB, map[string]string{"n": "2"})
	id3 := insertEvent(ctx, t, pool, app, stream, kindA, map[string]string{"n": "3"})

	repo := RepositoryWithPool(pool)
	ops := []Operation{
		&EachKind{App: app, Stream: stream, Op: 0, Kind: kindA},
		&EachKind{App: app, Stream: stream, Op: 1, Kind: kindB},
	}

	var results []OperationResult
	for result, err := range repo.Stream(ctx, ops) {
		require.NoError(t, err)
		results = append(results, result)
	}

	require.Len(t, results, 3)
	assert.Equal(t, id1, results[0].Envelope.ID)
	assert.Equal(t, id2, results[1].Envelope.ID)
	assert.Equal(t, id3, results[2].Envelope.ID)
}

func TestRepositoryStream_Ordering(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "order_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "first"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "second"})
	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "third"})

	repo := RepositoryWithPool(pool)
	ops := []Operation{
		&EachKind{App: app, Stream: stream, Op: 0, Kind: kind},
	}

	var results []OperationResult
	for result, err := range repo.Stream(ctx, ops) {
		require.NoError(t, err)
		results = append(results, result)
	}

	require.Len(t, results, 3)
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i].Envelope.When, results[i-1].Envelope.When,
			"events should be ordered by when_occurred ASC")
	}
}

func TestRepositoryStream_EmptyOps(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	repo := RepositoryWithPool(pool)
	for _, err := range repo.Stream(ctx, []Operation{}) {
		require.Error(t, err)
	}
}
