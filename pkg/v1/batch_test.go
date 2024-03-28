package v1

import (
	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestWireBatchR2Request(t *testing.T) {
	t.Run("Given an empty request", func(t *testing.T) {
		w := &WireBatchR2Request{}
		assert.True(t, w.Empty(), "empty batch should report itself empty")
	})

	t.Run("Given a batch working with IDs", func(t *testing.T) {
		w := &WireBatchR2Request{
			OnID: []WireBatchR2IDQuery{
				{Op: 0, ID: 0},
			},
		}
		assert.False(t, w.Empty(), "batch with IDs should not be empty")
	})

	t.Run("Given a batch working with kinds", func(t *testing.T) {
		w := &WireBatchR2Request{
			OnKinds: []WireBatchR2KindQuery{
				{Kind: faker.Word()},
			},
		}
		assert.False(t, w.Empty(), "batch with IDs should not be empty")
	})

	t.Run("Given a batch with both IDs and kinds", func(t *testing.T) {
		w := &WireBatchR2Request{
			OnKinds: []WireBatchR2KindQuery{
				{Kind: faker.Word()},
			},
			OnID: []WireBatchR2IDQuery{
				{Op: 0, ID: 0},
			},
		}
		assert.False(t, w.Empty(), "batch with all selectors should not be empty")
	})
}
