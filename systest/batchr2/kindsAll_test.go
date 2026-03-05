package batchr2

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindSearch(t *testing.T) {
	t.Parallel()
	t.Run("Given a single kind with multiple records", func(t *testing.T) {
		t.Parallel()
		kind := faker.Name()

		h, _, _ := setupHarnessT(t)

		var ids []int64
		gen := func() {
			_, id := genDoc(t, h, kind)
			ids = append(ids, id)
		}
		gen()
		gen()
		gen()
		gen()

		t.Run("When looking for all of a kind, then they are returned", func(t *testing.T) {
			t.Parallel()
			q := query2.NewQuery(h.stream)
			found := 0
			var seen []int64
			verifier := func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
				seen = append(seen, e.ID)
				found++
				return nil
			}
			q.OnKind(kind).Each(verifier)
			require.NoError(t, q.StreamBatch(h.ctx))
			assert.Equal(t, 4, found, "document was found wrong number of times")
		})
	})
}
