package memo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph/internal/memo"
)

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
	got := make([]*int, n)
	var started, wg sync.WaitGroup
	started.Add(n)
	for i := range n {
		wg.Go(func() {
			started.Done()
			v, err := m.Get(t.Context(), "a", f)
			require.NoError(t, err)
			got[i] = v
		})
	}
	started.Wait()
	close(release)
	wg.Wait()

	require.Equal(t, int32(1), calls.Load())
	for i := range n {
		require.Same(t, got[0], got[i])
	}
	require.Equal(t, 42, *got[0])

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

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := m.Get(first, 1, func(ctx context.Context) (string, error) {
			close(entered)
			<-fail
			cancelFirst()
			return "", ctx.Err()
		})
		require.ErrorIs(t, err, context.Canceled)
	})
	<-entered

	// This caller finds the first call running and waits for it; the first
	// call fails under a cancelled context, so this caller builds itself.
	result := make(chan string, 1)
	wg.Go(func() {
		v, err := m.Get(t.Context(), 1, func(context.Context) (string, error) { return "second", nil })
		require.NoError(t, err)
		result <- v
	})
	close(fail)
	wg.Wait()
	require.Equal(t, "second", <-result)
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
