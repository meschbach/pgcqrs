package v1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryWatchEnqueueDedup(t *testing.T) {
	t.Parallel()

	mw := &memoryWatch{
		lastEvent: -1,
		pending:   nil,
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := json.RawMessage(`{"data":"test"}`)

	mw.enqueue(0, Envelope{ID: 0, Kind: "test", When: now}, body)
	assert.Len(t, mw.pending, 1, "first enqueue should add event")
	assert.Equal(t, int64(0), mw.lastEvent)

	mw.enqueue(0, Envelope{ID: 0, Kind: "test", When: now}, body)
	assert.Len(t, mw.pending, 1, "duplicate enqueue with same ID should be skipped")
	assert.Equal(t, int64(0), mw.lastEvent)

	mw.enqueue(0, Envelope{ID: 0, Kind: "test", When: now}, body)
	assert.Len(t, mw.pending, 1, "another duplicate should still be skipped")

	mw.enqueue(2, Envelope{ID: 2, Kind: "test", When: now}, body)
	assert.Len(t, mw.pending, 2, "enqueue with higher ID should add event")
	assert.Equal(t, int64(2), mw.lastEvent)

	mw.enqueue(1, Envelope{ID: 1, Kind: "test", When: now}, body)
	assert.Len(t, mw.pending, 2, "enqueue with stale ID should be skipped")

	mw.enqueue(3, Envelope{ID: 3, Kind: "test", When: now}, body)
	assert.Len(t, mw.pending, 3, "enqueue with next ID should add event")
	assert.Equal(t, int64(3), mw.lastEvent)
}
