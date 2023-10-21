package batchr2

import (
	"context"
	"encoding/json"
	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKindSearch(t *testing.T) {
	t.Run("Given a single kind with multiple records", func(t *testing.T) {
		kind := faker.Name()

		h := setupHarness()
		t.Cleanup(func() {
			h.done()
		})

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
			q := query2.NewQuery(h.stream)
			found := 0
			var seen []int64
			verifier := func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage) error {
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
