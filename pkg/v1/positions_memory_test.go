package v1

import (
	"context"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryTransportSetPosition(t *testing.T) {
	t.Parallel()
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		consumer := faker.Name()
		event1 := h.stream.MustSubmit(ctx, faker.Word(), &PutEvent{Value: faker.Word()})
		event2 := h.stream.MustSubmit(ctx, faker.Word(), &PutEvent{Value: faker.Word()})

		// Set position to first event
		result, err := h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer, event1.ID)
		require.NoError(t, err)
		assert.Nil(t, result.PreviousEventID)
		assert.Equal(t, event1.ID, result.CurrentEventID)

		// Verify position was set
		pos, found, err := h.system.Transport.GetPosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, event1.ID, pos)

		// Update to second event
		result, err = h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer, event2.ID)
		require.NoError(t, err)
		assert.Equal(t, event1.ID, *result.PreviousEventID)
		assert.Equal(t, event2.ID, result.CurrentEventID)

		// Verify updated position
		pos, found, err = h.system.Transport.GetPosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, event2.ID, pos)

		// Attempt to go backwards should fail
		_, err = h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer, event1.ID)
		assert.Error(t, err)
	})
}

func TestMemoryTransportGetPosition(t *testing.T) {
	t.Parallel()
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		consumer := faker.Name()

		// Initially no position
		pos, found, err := h.system.Transport.GetPosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Zero(t, pos)

		// Set position
		event := h.stream.MustSubmit(ctx, faker.Word(), &PutEvent{Value: faker.Word()})
		_, err = h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer, event.ID)
		require.NoError(t, err)

		// Now should be found
		pos, found, err = h.system.Transport.GetPosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, event.ID, pos)
	})
}

func TestMemoryTransportListConsumers(t *testing.T) {
	t.Parallel()
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		// Initially no consumers
		consumers, err := h.system.Transport.ListConsumers(ctx, h.appName, h.streamName)
		require.NoError(t, err)
		assert.Empty(t, consumers)

		// Create two consumers
		consumer1 := faker.Name()
		consumer2 := faker.Name()
		event := h.stream.MustSubmit(ctx, faker.Word(), &PutEvent{Value: faker.Word()})

		_, err = h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer1, event.ID)
		require.NoError(t, err)
		_, err = h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer2, event.ID)
		require.NoError(t, err)

		// List should show both
		consumers, err = h.system.Transport.ListConsumers(ctx, h.appName, h.streamName)
		require.NoError(t, err)
		assert.Len(t, consumers, 2)
		assert.Contains(t, consumers, consumer1)
		assert.Contains(t, consumers, consumer2)
	})
}

func TestMemoryTransportDeletePosition(t *testing.T) {
	t.Parallel()
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		consumer := faker.Name()
		event := h.stream.MustSubmit(ctx, faker.Word(), &PutEvent{Value: faker.Word()})

		// Set position
		_, err := h.system.Transport.SetPosition(ctx, h.appName, h.streamName, consumer, event.ID)
		require.NoError(t, err)

		// Verify it exists
		_, found, err := h.system.Transport.GetPosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
		assert.True(t, found)

		// Delete position
		err = h.system.Transport.DeletePosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)

		// Verify it's gone
		_, found, err = h.system.Transport.GetPosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
		assert.False(t, found)

		// Delete again should be no-op (not error)
		err = h.system.Transport.DeletePosition(ctx, h.appName, h.streamName, consumer)
		require.NoError(t, err)
	})
}
