package views

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// waitForVersion waits up to timeout for the notifier to reach targetVersion.
func waitForVersion(t *testing.T, n *Notifier, target int64, timeout time.Duration) (bool, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	return n.WaitForVersion(ctx, target)
}

// TestWaitForVersionSatisfiedImmediately covers waits that return true without
// blocking because lastVersion already meets the target. The short timeout
// proves the wait does not block on a notification.
func TestWaitForVersionSatisfiedImmediately(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		emits  []int64
		target int64
	}{
		{"exact match", []int64{5}, 5},
		{"target below high-water mark", []int64{5}, 1},
		{"broadcast before subscribe", []int64{2}, 2},
		{"first change at version zero", []int64{0}, 0},
		{"tracks highest version", []int64{2, 5, 3}, 4},
		// A fresh notifier sits at lastVersion = -1, so the impossible target -1
		// (event IDs are non-negative) is satisfied immediately. This pins the
		// sentinel boundary that keeps a real version 0 distinct from "no events".
		{"negative target satisfies sentinel", nil, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := NewNotifier()
			for _, version := range tc.emits {
				require.NoError(t, n.Emit(t.Context(), Change{Version: version}))
			}

			reached, err := waitForVersion(t, n, tc.target, 50*time.Millisecond)
			require.NoError(t, err)
			require.True(t, reached)
		})
	}
}

// TestWaitForVersionEndsWhenContextEnds covers waits that end with
// (false, ctx.Err()) rather than reaching the target.
func TestWaitForVersionEndsWhenContextEnds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		target  int64
		makeCtx func(*testing.T) context.Context
		wantErr error
	}{
		{
			name:   "canceled before wait",
			target: 1,
			makeCtx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			wantErr: context.Canceled,
		},
		{
			name:   "deadline exceeded",
			target: 1,
			makeCtx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			// A fresh notifier has not broadcast anything; version 0 must not be
			// treated as "reached" (memory transport event IDs start at 0).
			name:   "fresh notifier never reaches version zero",
			target: 0,
			makeCtx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := NewNotifier()

			reached, err := n.WaitForVersion(tc.makeCtx(t), tc.target)
			require.False(t, reached)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestWaitForVersionWaitsForNotification verifies a wait is satisfied by a
// notification that arrives after the subscription is registered.
func TestWaitForVersionWaitsForNotification(t *testing.T) {
	t.Parallel()
	n := NewNotifier()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	started := make(chan struct{})
	done := make(chan bool, 1)
	errCh := make(chan error, 1)
	go func() {
		close(started)
		reached, err := n.WaitForVersion(ctx, 3)
		done <- reached
		errCh <- err
	}()
	<-started

	require.NoError(t, n.Emit(t.Context(), Change{Version: 1}))
	require.NoError(t, n.Emit(t.Context(), Change{Version: 3}))

	select {
	case <-done:
		require.NoError(t, <-errCh)
	case <-time.After(time.Second):
		require.Fail(t, "WaitForVersion did not return after target reached")
	}
}

// TestWaitForVersionDoesNotReturnBelowTarget verifies a wait keeps blocking on
// a notification below the target and only ends when ctx is canceled.
func TestWaitForVersionDoesNotReturnBelowTarget(t *testing.T) {
	t.Parallel()
	n := NewNotifier()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	type result struct {
		reached bool
		err     error
	}
	started := make(chan struct{})
	done := make(chan result, 1)
	go func() {
		close(started)
		reached, err := n.WaitForVersion(ctx, 3)
		done <- result{reached: reached, err: err}
	}()
	<-started

	require.NoError(t, n.Emit(t.Context(), Change{Version: 2}))

	select {
	case <-done:
		require.Fail(t, "WaitForVersion returned before reaching target")
		return
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	res := <-done
	require.False(t, res.reached)
	require.ErrorIs(t, res.err, context.Canceled)
}

// TestWaitForVersionMultipleWaiters verifies a single notification satisfies
// every registered waiter.
func TestWaitForVersionMultipleWaiters(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	type waiterResult struct {
		target  int64
		reached bool
		err     error
	}
	results := make(chan waiterResult, 2)
	for _, target := range []int64{3, 5} {
		go func(target int64) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			reached, err := n.WaitForVersion(ctx, target)
			results <- waiterResult{target: target, reached: reached, err: err}
		}(target)
	}

	require.NoError(t, n.Emit(t.Context(), Change{Version: 5}))

	for range []int64{3, 5} {
		select {
		case res := <-results:
			require.NoError(t, res.err)
			require.True(t, res.reached)
		case <-time.After(time.Second):
			require.Fail(t, "waiter did not return")
			return
		}
	}
}

// TestWaitForVersionConcurrentEmits verifies Emit is safe under concurrent
// producers and lastVersion always reaches the maximum emitted version.
func TestWaitForVersionConcurrentEmits(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	const numEmitters = 8
	const maxVersion = int64(numEmitters - 1)

	start := make(chan struct{})
	errs := make(chan error, numEmitters)
	var wg sync.WaitGroup
	wg.Add(numEmitters)
	for i := 0; i < numEmitters; i++ {
		go func(version int64) {
			defer wg.Done()
			<-start
			errs <- n.Emit(t.Context(), Change{Version: version})
		}(int64(i))
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	reached, err := waitForVersion(t, n, maxVersion, time.Second)
	require.NoError(t, err)
	require.True(t, reached)
}

// TestEmitReturnsCallbackErrors covers Emit's contract for failing callbacks:
// a single error propagates, several are joined.
func TestEmitReturnsCallbackErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		nCallbacks int
	}{
		{"single failing callback propagates error", 1},
		{"multiple failing callbacks join errors", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := NewNotifier()

			var sentinels []error
			for range tc.nCallbacks {
				sentinel := errors.New("callback failed")
				sentinels = append(sentinels, sentinel)
				n.OnChange(func(context.Context, Change) error { return sentinel })
			}

			err := n.Emit(t.Context(), Change{Version: 1})
			for _, sentinel := range sentinels {
				require.ErrorIs(t, err, sentinel)
			}
		})
	}
}

// TestEmitContainsPanic verifies a panicking callback is contained and reported
// as an error without aborting delivery to the remaining callbacks.
func TestEmitContainsPanic(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	var received []Change
	n.OnChange(func(_ context.Context, c Change) error {
		received = append(received, c)
		return nil
	})
	n.OnChange(func(context.Context, Change) error {
		panic("boom")
	})
	n.OnChange(func(_ context.Context, c Change) error {
		received = append(received, c)
		return nil
	})

	var err error
	require.NotPanics(t, func() {
		err = n.Emit(t.Context(), Change{Version: 1})
	})
	require.Error(t, err)
	require.Len(t, received, 2, "callbacks after the panicking one must still be delivered")
}

// TestVersionTrackingSurvivesCallbackErrors verifies a callback failure does
// not prevent lastVersion from advancing.
func TestVersionTrackingSurvivesCallbackErrors(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	sentinel := errors.New("callback failed")
	n.OnChange(func(context.Context, Change) error { return sentinel })

	err := n.Emit(t.Context(), Change{Version: 5})
	require.ErrorIs(t, err, sentinel)

	reached, err := waitForVersion(t, n, 5, time.Second)
	require.NoError(t, err)
	require.True(t, reached)
}

// TestOnChangeUnsubscribeIdempotent verifies the returned unsubscribe function
// can be called more than once and stops further delivery.
func TestOnChangeUnsubscribeIdempotent(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	var received []Change
	unsub := n.OnChange(func(_ context.Context, c Change) error {
		received = append(received, c)
		return nil
	})

	unsub()
	unsub()

	require.NoError(t, n.Emit(t.Context(), Change{Version: 1}))
	require.Empty(t, received)
}

// TestWaitForVersionBurstDropDoesNotTimeout verifies that when more than 16
// rapid changes arrive (overflowing the buffered channel) and the target version
// is dropped by the non-blocking send, WaitForVersion still returns true because
// lastVersion was updated by Emit before dispatching callbacks.
//
// This is a regression test for the burst-drop race: the channel buffer is 16,
// so if >16 changes arrive and the target is in the dropped range, the loop
// would previously block until ctx.Done(). The fix re-checks atOrAbove on each
// received change and on ctx.Done().
func TestWaitForVersionBurstDropDoesNotTimeout(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	// Start waiting for version 20 (beyond the 16-buffer channel)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	done := make(chan bool, 1)
	errCh := make(chan error, 1)
	go func() {
		reached, err := n.WaitForVersion(ctx, 20)
		done <- reached
		errCh <- err
	}()

	// Give the goroutine time to subscribe
	time.Sleep(10 * time.Millisecond)

	// Emit 25 changes rapidly — versions 0-24. The channel buffer is 16,
	// so versions 17-24 will be dropped by the non-blocking send.
	// The target (20) is in the dropped range.
	for i := int64(0); i < 25; i++ {
		require.NoError(t, n.Emit(t.Context(), Change{Version: i}))
	}

	// WaitForVersion should return true because lastVersion was updated to 24
	// by Emit, even though the specific change with version 20 was dropped.
	select {
	case reached := <-done:
		require.True(t, reached, "WaitForVersion should return true when lastVersion >= target")
		require.NoError(t, <-errCh)
	case <-time.After(3 * time.Second):
		require.Fail(t, "WaitForVersion timed out — burst-drop race not handled")
	}
}

// TestWaitForVersionChecksAtOrAboveOnEachReceivedChange verifies that after
// receiving a change below the target, WaitForVersion re-checks atOrAbove
// in case lastVersion was updated beyond what the received change indicates.
func TestWaitForVersionChecksAtOrAboveOnEachReceivedChange(t *testing.T) {
	t.Parallel()
	n := NewNotifier()

	// Emit version 10 first
	require.NoError(t, n.Emit(t.Context(), Change{Version: 10}))

	// Now wait for version 5 — should return immediately via atOrAbove check
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	reached, err := n.WaitForVersion(ctx, 5)
	require.NoError(t, err)
	require.True(t, reached)
}
