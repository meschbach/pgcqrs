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

func TestKindMatch(t *testing.T) {
	t.Parallel()
	t.Run("Given a single kind with multiple records", func(t *testing.T) {
		t.Parallel()
		kind := faker.Name()

		h, _, _ := setupHarnessT(t)

		genDoc(t, h, kind)
		middle, middleID := genDoc(t, h, kind)
		repeatedFirst, repeatedFirstID := genDoc(t, h, kind)
		repeatedLast, err := h.stream.Submit(h.ctx, kind, repeatedFirst)
		require.NoError(t, err)

		t.Run("When searching for a single document then it is returned", func(t *testing.T) {
			t.Parallel()
			q := query2.NewQuery(h.stream)
			found := 0
			verifier := v1.EntityFunc[nestedStringDocument](func(_ context.Context, e v1.Envelope, entity nestedStringDocument) {
				assert.Equal(t, middleID, e.ID)
				assert.Equal(t, middle.Root, entity.Root)
				if assert.NotNil(t, middle.Nested) {
					assert.Equal(t, middle.Nested.Value, entity.Nested.Value)
				}
				found++
			})
			q.OnKind(kind).Subset(nestedStringDocument{Root: middle.Root}).On(verifier)
			require.NoError(t, q.StreamBatch(h.ctx))
			assert.Equal(t, 1, found, "document was found wrong number of times")
		})

		t.Run("When multiple documents match, then all matched documents are returned", func(t *testing.T) {
			t.Parallel()
			q := query2.NewQuery(h.stream)
			var matched []matchedPair
			verifier := v1.EntityFunc[nestedStringDocument](func(_ context.Context, e v1.Envelope, entity nestedStringDocument) {
				matched = append(matched, matchedPair{
					envelope: e,
					entity:   entity,
				})
			})
			q.OnKind(kind).Subset(nestedStringDocument{Root: repeatedFirst.Root}).On(verifier)
			require.NoError(t, q.StreamBatch(h.ctx))
			if assert.Len(t, matched, 2) {
				assert.Equal(t, repeatedFirstID, matched[0].envelope.ID)
				assert.Equal(t, repeatedLast.ID, matched[1].envelope.ID)
			}
		})
		t.Run("When kind without documents is given a match of another document", func(t *testing.T) {
			t.Parallel()
			count := 0
			q := query2.NewQuery(h.stream)
			q.OnKind(faker.Name()).Subset(nestedStringDocument{Root: middle.Root}).On(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
				count++
				return nil
			})
			require.NoError(t, q.StreamBatch(h.ctx))
			assert.Equal(t, 0, count)
		})
	})

	t.Run("Given a repository with multiple document kinds stored", func(t *testing.T) {
		t.Parallel()
		h, _, _ := setupHarnessT(t)

		kind1 := faker.FirstName()
		doc1, doc1ID := genDoc(t, h, kind1)
		kind2 := faker.FirstName()
		doc2, doc2ID := genDoc(t, h, kind2)

		t.Run("When given two kinds matching a document then each document is supplied once", func(t *testing.T) {
			t.Parallel()
			q := query2.NewQuery(h.stream)
			documentCount := 0
			q.OnKind(kind1).
				Subset(nestedStringDocument{Root: doc1.Root}).
				On(v1.EntityFunc[nestedStringDocument](func(_ context.Context, e v1.Envelope, _ nestedStringDocument) {
					documentCount++
					assert.Equal(t, doc1ID, e.ID)
				}))
			q.OnKind(kind2).
				Subset(nestedStringDocument{Nested: &NestedString{Value: doc2.Nested.Value}}).
				On(v1.EntityFunc[nestedStringDocument](func(_ context.Context, e v1.Envelope, _ nestedStringDocument) {
					documentCount++
					assert.Equal(t, doc2ID, e.ID)
				}))
			require.NoError(t, q.StreamBatch(h.ctx))
			assert.Equal(t, 2, documentCount)
		})
	})
}
