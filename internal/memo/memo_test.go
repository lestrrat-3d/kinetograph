package memo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph/internal/memo"
)

// waitParked blocks until n callers have parked on k's entry.
func waitParked[K comparable, V any](t *testing.T, m *memo.Map[K, V], k K, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, ok := m.Parked(k)
		return ok && got == n
	}, 10*time.Second, time.Millisecond, "waiting for %d parked callers", n)
}

type result[V any] struct {
	value V
	err   error
}

func TestGetCallsOncePerKey(t *testing.T) {
	var m memo.Map[string, *int]
	var calls atomic.Int32
	release := make(chan struct{})
	//nolint:unparam // Get takes a function of this shape; this one never fails
	f := func(context.Context) (*int, error) {
		calls.Add(1)
		<-release
		v := 42
		return &v, nil
	}

	const n = 8
	results := make([]result[*int], n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			v, err := m.Get(t.Context(), "a", f)
			results[i] = result[*int]{v, err}
		})
	}
	// One caller is inside f and the other n-1 wait on its entry before f
	// may return, so every caller below took the shared result.
	waitParked(t, &m, "a", n-1)
	require.Equal(t, int32(1), calls.Load())
	close(release)
	wg.Wait()

	require.Equal(t, int32(1), calls.Load())
	for i := range n {
		require.NoError(t, results[i].err)
		require.Same(t, results[0].value, results[i].value)
	}
	require.Equal(t, 42, *results[0].value)

	// Another key is another call.
	_, err := m.Get(t.Context(), "b", f)
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
}

func TestGetKeepsAFailure(t *testing.T) {
	var m memo.Map[int, string]
	errFail := errors.New("fail")
	calls := 0
	f := func(context.Context) (string, error) {
		calls++
		return "", errFail
	}
	for range 3 {
		_, err := m.Get(t.Context(), 1, f)
		require.ErrorIs(t, err, errFail)
	}
	require.Equal(t, 1, calls)
}

func TestGetDropsACancelledFailure(t *testing.T) {
	var m memo.Map[int, string]
	ctx, cancel := context.WithCancel(t.Context())
	_, err := m.Get(ctx, 1, func(ctx context.Context) (string, error) {
		cancel()
		return "", ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)

	v, err := m.Get(t.Context(), 1, func(context.Context) (string, error) { return "built", nil })
	require.NoError(t, err)
	require.Equal(t, "built", v)
}

func TestGetWaiterRetriesAfterCancelledCall(t *testing.T) {
	var m memo.Map[int, string]
	first, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	entered := make(chan struct{})
	fail := make(chan struct{})

	var firstResult, secondResult result[string]
	var secondCalls atomic.Int32
	var wg sync.WaitGroup
	wg.Go(func() {
		v, err := m.Get(first, 1, func(ctx context.Context) (string, error) {
			close(entered)
			<-fail
			cancelFirst()
			return "", ctx.Err()
		})
		firstResult = result[string]{v, err}
	})
	<-entered

	wg.Go(func() {
		v, err := m.Get(t.Context(), 1, func(context.Context) (string, error) {
			secondCalls.Add(1)
			return "second", nil
		})
		secondResult = result[string]{v, err}
	})
	// The second caller waits on the first call's entry; only then does the
	// first call fail under its cancelled context.
	waitParked(t, &m, 1, 1)
	close(fail)
	wg.Wait()

	require.ErrorIs(t, firstResult.err, context.Canceled)
	require.NoError(t, secondResult.err)
	require.Equal(t, "second", secondResult.value)
	require.Equal(t, int32(1), secondCalls.Load(), "the waiter called its own function after the drop")
}

func TestGetWaiterCancelled(t *testing.T) {
	var m memo.Map[int, string]
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = m.Get(t.Context(), 1, func(context.Context) (string, error) {
			close(entered)
			<-release
			return "late", nil
		})
	})
	<-entered

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := m.Get(ctx, 1, func(context.Context) (string, error) { return "never", nil })
	require.Equal(t, context.Canceled, err)
	close(release)
	wg.Wait()
}

func TestGetDropsAPanic(t *testing.T) {
	var m memo.Map[int, string]
	require.Panics(t, func() {
		_, _ = m.Get(t.Context(), 1, func(context.Context) (string, error) { panic("boom") })
	})
	v, err := m.Get(t.Context(), 1, func(context.Context) (string, error) { return "after", nil })
	require.NoError(t, err)
	require.Equal(t, "after", v)
}
