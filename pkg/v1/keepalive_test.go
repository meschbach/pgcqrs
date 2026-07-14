package v1

import (
	"context"
	"fmt"
	"testing"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type mockBidiStream struct {
	sentMsgs []*ipc.KeepAliveClientMessage
	recvMsgs []*ipc.KeepAliveServerMessage
	recvIdx  int
	recvErrs []error
	ctx      context.Context
}

func (m *mockBidiStream) Send(msg *ipc.KeepAliveClientMessage) error {
	m.sentMsgs = append(m.sentMsgs, msg)
	return nil
}

func (m *mockBidiStream) Recv() (*ipc.KeepAliveServerMessage, error) {
	if m.recvIdx >= len(m.recvMsgs) {
		if m.recvIdx < len(m.recvErrs) {
			err := m.recvErrs[m.recvIdx]
			m.recvIdx++
			return nil, err
		}
		return nil, fmt.Errorf("no more messages")
	}
	msg := m.recvMsgs[m.recvIdx]
	if m.recvIdx < len(m.recvErrs) && m.recvErrs[m.recvIdx] != nil {
		err := m.recvErrs[m.recvIdx]
		m.recvIdx++
		return nil, err
	}
	m.recvIdx++
	return msg, nil
}

func (m *mockBidiStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockBidiStream) Trailer() metadata.MD         { return nil }
func (m *mockBidiStream) CloseSend() error             { return nil }
func (m *mockBidiStream) Context() context.Context     { return m.ctx }
func (m *mockBidiStream) SendMsg(_ any) error          { return nil }
func (m *mockBidiStream) RecvMsg(_ any) error          { return nil }

var _ grpc.BidiStreamingClient[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage] = &mockBidiStream{}

func newTestKeepAlive(t *testing.T, msgs []*ipc.KeepAliveServerMessage) (*KeepAlive, *mockBidiStream) {
	t.Helper()
	mock := &mockBidiStream{
		ctx:      t.Context(),
		recvMsgs: msgs,
		recvErrs: []error{nil},
	}
	ka := &KeepAlive{
		stream:   mock,
		domain:   "test-domain",
		streamN:  "test-stream",
		consumer: "test-consumer",
		holder:   "test-holder",
	}
	return ka, mock
}

func TestKeepAliveHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("RenewedReturnsNil", func(t *testing.T) {
		t.Parallel()
		ka, mock := newTestKeepAlive(t, []*ipc.KeepAliveServerMessage{
			{
				Message: &ipc.KeepAliveServerMessage_LockStatus{
					LockStatus: &ipc.KeepAliveLockStatus{
						Locked: true,
						Reason: ipc.LockStatusReason_RENEWED,
					},
				},
			},
		})

		err := ka.Heartbeat(t.Context(), 42)
		require.NoError(t, err)
		require.Len(t, mock.sentMsgs, 1)
		hb := mock.sentMsgs[0].GetHeartbeat()
		require.NotNil(t, hb)
		assert.Equal(t, int64(42), hb.Position)
	})

	t.Run("ConflictReturnsError", func(t *testing.T) {
		t.Parallel()
		target := int64(10)
		current := int64(20)
		ka, _ := newTestKeepAlive(t, []*ipc.KeepAliveServerMessage{
			{
				Message: &ipc.KeepAliveServerMessage_LockStatus{
					LockStatus: &ipc.KeepAliveLockStatus{
						Locked:         false,
						Reason:         ipc.LockStatusReason_CONFLICT,
						TargetVersion:  &target,
						CurrentVersion: &current,
					},
				},
			},
		})

		err := ka.Heartbeat(t.Context(), 10)
		require.Error(t, err)
		var conflict *HeartbeatConflictError
		require.ErrorAs(t, err, &conflict)
		assert.Equal(t, int64(10), conflict.TargetVersion)
		assert.Equal(t, int64(20), conflict.CurrentVersion)
	})

	for _, tc := range []struct {
		name      string
		reason    ipc.LockStatusReason
		assertErr func(t *testing.T, err error)
	}{
		{
			"ExpiredReturnsError",
			ipc.LockStatusReason_EXPIRED,
			func(t *testing.T, err error) {
				var expiredErr *LockExpiredError
				require.ErrorAs(t, err, &expiredErr)
			},
		},
		{
			"StolenReturnsError",
			ipc.LockStatusReason_STOLEN,
			func(t *testing.T, err error) {
				var lockErr *LockNotHeldError
				require.ErrorAs(t, err, &lockErr)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ka, _ := newTestKeepAlive(t, []*ipc.KeepAliveServerMessage{
				{
					Message: &ipc.KeepAliveServerMessage_LockStatus{
						LockStatus: &ipc.KeepAliveLockStatus{
							Locked: false,
							Reason: tc.reason,
						},
					},
				},
			})

			err := ka.Heartbeat(t.Context(), 10)
			require.Error(t, err)
			tc.assertErr(t, err)
		})
	}
}

func TestKeepAliveRelease(t *testing.T) {
	t.Parallel()

	t.Run("AckOkReturnsNil", func(t *testing.T) {
		t.Parallel()
		ka, mock := newTestKeepAlive(t, []*ipc.KeepAliveServerMessage{
			{
				Message: &ipc.KeepAliveServerMessage_ReleaseAck{
					ReleaseAck: &ipc.KeepAliveReleaseAck{Ok: true},
				},
			},
		})

		err := ka.Release(t.Context())
		require.NoError(t, err)
		require.Len(t, mock.sentMsgs, 1)
		assert.NotNil(t, mock.sentMsgs[0].GetReleaseRequest())
	})

	t.Run("AckNotOkReturnsError", func(t *testing.T) {
		t.Parallel()
		ka, _ := newTestKeepAlive(t, []*ipc.KeepAliveServerMessage{
			{
				Message: &ipc.KeepAliveServerMessage_ReleaseAck{
					ReleaseAck: &ipc.KeepAliveReleaseAck{Ok: false},
				},
			},
		})

		err := ka.Release(t.Context())
		require.Error(t, err)
	})
}
