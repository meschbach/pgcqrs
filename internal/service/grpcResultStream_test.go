package service

import (
	"context"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type capturingSink struct {
	given []*ipc.QueryOut
}

func (c *capturingSink) send(ctx context.Context, msg *ipc.QueryOut) error {
	c.given = append(c.given, msg)
	return nil
}

const fakeIDMax = 999999
const fakeIDMin = 0

func fakeInt64Range(min int64, max int64) int64 {
	//todo: resolve how to get the correct range
	value := faking.RandIntRange(int(min), int(max))
	return int64(value)
}

func fakeID() int64 {
	return fakeInt64Range(fakeIDMin, fakeIDMax)
}

func fakeIDAbove(value int64) int64 {
	return fakeInt64Range(value+1, fakeIDMax)
}

func fakeIDBelow(value int64) int64 {
	return fakeInt64Range(fakeIDMin, value-1)
}

func TestVersionFilter(t *testing.T) {
	t.Run("Given a new state", func(t *testing.T) {
		ctx := t.Context()
		capture := &capturingSink{}
		v := &versionFilter{
			lastSeen: int64(0),
			next:     capture,
		}

		firstID := fakeID()
		t.Run("When an event is first passed", func(t *testing.T) {
			err := v.send(ctx, &ipc.QueryOut{
				Id: &firstID,
			})
			require.NoError(t, err)

			//
			//then we should dispatch the event
			//
			if assert.Len(t, capture.given, 1) {
				assert.Equal(t, firstID, *capture.given[0].Id)
			}
		})

		t.Run("When an event is passed twice", func(t *testing.T) {
			err := v.send(ctx, &ipc.QueryOut{
				Id: &firstID,
			})
			require.NoError(t, err)

			//
			// then another event is not dispatched
			//
			if assert.Len(t, capture.given, 1) {
				assert.Equal(t, firstID, *capture.given[0].Id)
			}
		})

		t.Run("When an earlier event is passed", func(t *testing.T) {
			earlierID := fakeIDBelow(firstID)
			err := v.send(ctx, &ipc.QueryOut{
				Id: &earlierID,
			})
			require.NoError(t, err)

			//
			// then the event is not accepted
			//
			if assert.Len(t, capture.given, 1) {
				assert.Equal(t, firstID, *capture.given[0].Id)
			}
		})

		t.Run("When an later event is passed", func(t *testing.T) {
			laterID := fakeIDAbove(firstID)
			err := v.send(ctx, &ipc.QueryOut{
				Id: &laterID,
			})
			require.NoError(t, err)

			//
			// then the event is accepted
			//
			if assert.Len(t, capture.given, 2) {
				assert.Equal(t, firstID, *capture.given[0].Id)
				assert.Equal(t, laterID, *capture.given[1].Id)
			}
		})
	})
}

func TestOpSplitter(t *testing.T) {
	captures := make(map[int64]*capturingSink)

	splitter := newOpSplitter(func(i int64) grpcSink {
		if _, ok := captures[i]; ok {
			t.Fatalf("duplicate capture for %d", i)
		}
		newCapture := &capturingSink{}
		captures[i] = newCapture
		return newCapture
	})

	firstExampleOp := fakeInt64Range(0, 100)
	t.Run("When an unseen operation is passed in", func(t *testing.T) {
		//
		//
		//
		msg := &ipc.QueryOut{
			Op: firstExampleOp,
		}
		err := splitter.send(t.Context(), msg)
		require.NoError(t, err)

		//
		//
		//
		if assert.Len(t, captures, 1) {
			assert.Equal(t, firstExampleOp, captures[firstExampleOp].given[0].Op)
		}
	})

	t.Run("When a seen operation is passed in", func(t *testing.T) {
		//
		//
		//
		msg := &ipc.QueryOut{
			Op: firstExampleOp,
		}
		err := splitter.send(t.Context(), msg)
		require.NoError(t, err)

		//
		// then we do not have an additional pipeline created
		//
		if assert.Len(t, captures, 1) {
			assert.Equal(t, firstExampleOp, captures[firstExampleOp].given[0].Op)
		}
	})

	secondExampleOp := fakeInt64Range(100, 200)
	t.Run("When a seen operation is passed in", func(t *testing.T) {
		//
		//
		//
		msg := &ipc.QueryOut{
			Op: secondExampleOp,
		}
		err := splitter.send(t.Context(), msg)
		require.NoError(t, err)

		//
		// then a new pipeline is created
		//
		if assert.Len(t, captures, 2) {
			assert.Equal(t, secondExampleOp, captures[secondExampleOp].given[0].Op)
		}
	})
}
