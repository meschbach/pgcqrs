package v1

import (
	"testing"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateQuery(t *testing.T) {
	t.Parallel()

	t.Run("empty query returns EmptyQueryError", func(t *testing.T) {
		t.Parallel()
		q := &ipc.QueryIn{
			Events: &ipc.DomainStream{Domain: "test", Stream: "test"},
		}
		err := ValidateQuery(q)
		require.Error(t, err)
		var emptyErr *EmptyQueryError
		require.ErrorAs(t, err, &emptyErr)
		assert.Contains(t, err.Error(), "no selection clause")
	})

	t.Run("query with OnKind is valid", func(t *testing.T) {
		t.Parallel()
		q := &ipc.QueryIn{
			Events: &ipc.DomainStream{Domain: "test", Stream: "test"},
			OnKind: []*ipc.OnKindClause{{Kind: "test-kind"}},
		}
		err := ValidateQuery(q)
		require.NoError(t, err)
	})

	t.Run("query with OnID is valid", func(t *testing.T) {
		t.Parallel()
		q := &ipc.QueryIn{
			Events: &ipc.DomainStream{Domain: "test", Stream: "test"},
			OnID:   []*ipc.OnIDClause{{Id: 1}},
		}
		err := ValidateQuery(q)
		require.NoError(t, err)
	})

	t.Run("query with OnEach is valid", func(t *testing.T) {
		t.Parallel()
		q := &ipc.QueryIn{
			Events: &ipc.DomainStream{Domain: "test", Stream: "test"},
			OnEach: &ipc.OnEachEvent{},
		}
		err := ValidateQuery(q)
		require.NoError(t, err)
	})
}

func TestMemoryWatch_EmptyQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	m := &memory{
		input:   make(chan memoryOp, 32),
		domains: make(map[string]*memoryDomain),
	}
	go m.runService()

	q := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: "test", Stream: "test"},
	}
	watch, err := m.Watch(ctx, q)
	require.Error(t, err)
	assert.Nil(t, watch)
	var emptyErr *EmptyQueryError
	require.ErrorAs(t, err, &emptyErr)
}
