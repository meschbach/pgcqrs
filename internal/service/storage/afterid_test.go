package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateAfterID(t *testing.T) {
	t.Parallel()

	t.Run("EachKind", func(t *testing.T) {
		t.Parallel()
		op := &EachKind{App: "app", Stream: "stream", Op: 0, Kind: "kind", AfterID: 0}

		op.UpdateAfterID(42)
		assert.Equal(t, int64(42), op.AfterID)

		op.UpdateAfterID(99)
		assert.Equal(t, int64(99), op.AfterID)
	})

	t.Run("MatchSubset", func(t *testing.T) {
		t.Parallel()
		op := &MatchSubset{App: "app", Stream: "stream", Op: 0, Kind: "kind", AfterID: 0}

		op.UpdateAfterID(42)
		assert.Equal(t, int64(42), op.AfterID)
	})

	t.Run("AllStreamEvents", func(t *testing.T) {
		t.Parallel()
		op := &AllStreamEvents{Domain: "app", Stream: "stream", Op: 0, AfterID: 0}

		op.UpdateAfterID(42)
		assert.Equal(t, int64(42), op.AfterID)
	})

	t.Run("No aliasing between ops", func(t *testing.T) {
		t.Parallel()
		ek := &EachKind{App: "app", Stream: "stream", Op: 0, Kind: "kind", AfterID: 0}
		ms := &MatchSubset{App: "app", Stream: "stream", Op: 0, Kind: "kind", AfterID: 0}
		ae := &AllStreamEvents{Domain: "app", Stream: "stream", Op: 0, AfterID: 0}

		ek.UpdateAfterID(10)
		ms.UpdateAfterID(20)
		ae.UpdateAfterID(30)

		assert.Equal(t, int64(10), ek.AfterID)
		assert.Equal(t, int64(20), ms.AfterID)
		assert.Equal(t, int64(30), ae.AfterID)
	})
}

func TestAfterIDSentinel(t *testing.T) {
	t.Parallel()

	t.Run("ZeroAfterID produces no filter", func(t *testing.T) {
		t.Parallel()
		op := &EachKind{App: "app", Stream: "stream", Op: 0, Kind: "kind", AfterID: 0}
		q := &SQLQuery{first: true}
		op.append(q)

		assert.NotContains(t, q.DML, "e.id >")
	})

	t.Run("PositiveAfterID produces filter", func(t *testing.T) {
		t.Parallel()
		op := &EachKind{App: "app", Stream: "stream", Op: 0, Kind: "kind", AfterID: 5}
		q := &SQLQuery{first: true}
		op.append(q)

		assert.Contains(t, q.DML, "e.id >")
		require.Len(t, q.Args, 4) // app, stream, kind, afterID
		assert.Equal(t, int64(5), q.Args[3])
	})

	t.Run("MatchSubset zero afterID", func(t *testing.T) {
		t.Parallel()
		op := &MatchSubset{App: "app", Stream: "stream", Op: 0, Kind: "kind", Subset: []byte(`{}`), AfterID: 0}
		q := &SQLQuery{first: true}
		op.append(q)

		assert.NotContains(t, q.DML, "e.id >")
	})

	t.Run("MatchSubset positive afterID", func(t *testing.T) {
		t.Parallel()
		op := &MatchSubset{App: "app", Stream: "stream", Op: 0, Kind: "kind", Subset: []byte(`{}`), AfterID: 7}
		q := &SQLQuery{first: true}
		op.append(q)

		assert.Contains(t, q.DML, "e.id >")
	})

	t.Run("AllStreamEvents zero afterID", func(t *testing.T) {
		t.Parallel()
		op := &AllStreamEvents{Domain: "app", Stream: "stream", Op: 0, AfterID: 0}
		q := &SQLQuery{first: true}
		op.append(q)

		assert.NotContains(t, q.DML, "e.id >")
	})

	t.Run("AllStreamEvents positive afterID", func(t *testing.T) {
		t.Parallel()
		op := &AllStreamEvents{Domain: "app", Stream: "stream", Op: 0, AfterID: 3}
		q := &SQLQuery{first: true}
		op.append(q)

		assert.Contains(t, q.DML, "e.id >")
	})
}

func TestMatchIDIgnoreUpdateAfterID(t *testing.T) {
	t.Parallel()

	op := WithMatchID("app", "stream", 42, 0)
	q := &SQLQuery{first: true}
	op.append(q)

	assert.Contains(t, q.DML, "e.id =")
	assert.NotContains(t, q.DML, "e.id >")
}
