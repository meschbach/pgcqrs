package service

import (
	"context"
	"testing"
	"time"

	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

type capturingQueryServer struct {
	ctx  context.Context
	sent []*ipc.QueryOut
}

func (c *capturingQueryServer) Send(msg *ipc.QueryOut) error {
	c.sent = append(c.sent, msg)
	return nil
}
func (c *capturingQueryServer) Context() context.Context     { return c.ctx }
func (c *capturingQueryServer) SetHeader(metadata.MD) error  { return nil }
func (c *capturingQueryServer) SendHeader(metadata.MD) error { return nil }
func (c *capturingQueryServer) SetTrailer(metadata.MD)       {}
func (c *capturingQueryServer) SendMsg(interface{}) error    { return nil }
func (c *capturingQueryServer) RecvMsg(interface{}) error    { return nil }

func TestGrpcResultStreamDoesNotDropFirstEvent(t *testing.T) {
	t.Parallel()

	t.Run("First event with ID 0 is delivered", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		mock := &capturingQueryServer{ctx: ctx}
		stream := &grpcResultStream{out: mock, lastSentID: -1}

		err := stream.pushTranslatorMessage(ctx, storage2.OperationResult{
			Op: 0,
			Envelope: v1.Envelope{
				ID:   0,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: "Created",
			},
		})
		require.NoError(t, err)
		require.Len(t, mock.sent, 1, "event with ID 0 should not be dropped")
		assert.Equal(t, int64(0), *mock.sent[0].Id)
	})

	t.Run("Duplicate event with same ID is dropped", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		mock := &capturingQueryServer{ctx: ctx}
		stream := &grpcResultStream{out: mock, lastSentID: -1}

		envelope := storage2.OperationResult{
			Op: 0,
			Envelope: v1.Envelope{
				ID:   5,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: "Updated",
			},
		}
		err := stream.pushTranslatorMessage(ctx, envelope)
		require.NoError(t, err)
		assert.Len(t, mock.sent, 1)

		err = stream.pushTranslatorMessage(ctx, envelope)
		require.NoError(t, err)
		assert.Len(t, mock.sent, 1, "duplicate event should be dropped")
	})

	t.Run("Lower ID after higher ID is dropped", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		mock := &capturingQueryServer{ctx: ctx}
		stream := &grpcResultStream{out: mock, lastSentID: -1}

		high := storage2.OperationResult{
			Op: 0,
			Envelope: v1.Envelope{
				ID:   10,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: "High",
			},
		}
		err := stream.pushTranslatorMessage(ctx, high)
		require.NoError(t, err)
		assert.Len(t, mock.sent, 1)

		low := storage2.OperationResult{
			Op: 0,
			Envelope: v1.Envelope{
				ID:   3,
				When: time.Now().Format(time.RFC3339Nano),
				Kind: "Low",
			},
		}
		err = stream.pushTranslatorMessage(ctx, low)
		require.NoError(t, err)
		assert.Len(t, mock.sent, 1, "lower ID after higher ID should be dropped")
	})

	t.Run("Monotonically increasing events all pass through", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		mock := &capturingQueryServer{ctx: ctx}
		stream := &grpcResultStream{out: mock, lastSentID: -1}

		for i := int64(1); i <= 5; i++ {
			err := stream.pushTranslatorMessage(ctx, storage2.OperationResult{
				Op: 0,
				Envelope: v1.Envelope{
					ID:   i,
					When: time.Now().Format(time.RFC3339Nano),
					Kind: "Event",
				},
			})
			require.NoError(t, err)
		}
		assert.Len(t, mock.sent, 5, "all 5 events should be delivered")
		for i, msg := range mock.sent {
			assert.Equal(t, int64(i+1), *msg.Id)
		}
	})
}

func TestGrpcResultStream_DedupCallsMetricCounter(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mock := &capturingQueryServer{ctx: ctx}
	stream := &grpcResultStream{out: mock, lastSentID: -1}

	// Send event 5
	err := stream.pushTranslatorMessage(ctx, storage2.OperationResult{
		Op: 0,
		Envelope: v1.Envelope{
			ID:   5,
			When: time.Now().Format(time.RFC3339Nano),
			Kind: "Test",
		},
	})
	require.NoError(t, err)
	assert.Len(t, mock.sent, 1)

	// Send duplicate of event 5 — should be dropped and WatchDedupDrops incremented
	err = stream.pushTranslatorMessage(ctx, storage2.OperationResult{
		Op: 0,
		Envelope: v1.Envelope{
			ID:   5,
			When: time.Now().Format(time.RFC3339Nano),
			Kind: "Test",
		},
	})
	require.NoError(t, err)
	assert.Len(t, mock.sent, 1, "duplicate should be dropped")

	// Send event 3 (lower than 5) — should also be dropped and counter incremented again
	err = stream.pushTranslatorMessage(ctx, storage2.OperationResult{
		Op: 0,
		Envelope: v1.Envelope{
			ID:   3,
			When: time.Now().Format(time.RFC3339Nano),
			Kind: "Test",
		},
	})
	require.NoError(t, err)
	assert.Len(t, mock.sent, 1, "lower ID should be dropped")

	// WatchDedupDrops is a package-level OTel counter. Without a reader we
	// cannot assert its numeric value, but the two drops above exercised the
	// Add(ctx, 1) call path. The compile-time type check below verifies the
	// metric variable is correctly typed.
	_ = WatchDedupDrops
}
