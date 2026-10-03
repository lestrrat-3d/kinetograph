// Package memo calls a function once per key and hands every caller for that
// key the same result. kinetograph's BuildCache and render's mesh cache use it
// so that several workers never build or tessellate one thing twice.
package memo

import (
	"context"
	"sync"
)

// entry is one key's result. done is closed once value and err are set;
// dropped is set before done closes when the call failed under a done ctx or
// panicked, and tells a waiter to call f itself. parked counts the callers
// that found the entry and waited on done; it is guarded by Map.mu.
type entry[V any] struct {
	done    chan struct{}
	value   V
	err     error
	dropped bool
	parked  int
}

// Map holds one result per key. The zero Map is empty and ready to use. It is
// safe for concurrent use, and it keeps every result until the Map is dropped.
type Map[K comparable, V any] struct {
	mu      sync.Mutex
	entries map[K]*entry[V] // lookup only; never iterated
}

// Get returns the result of f for k. The first caller for k calls f(ctx); a
// caller that asks for k while that call runs waits for it and receives the
// same value and error. A failed result is kept like a successful one, so f is
// not called again for k.
//
// The exception is a call that fails while its caller's ctx is done: that
// result is dropped, and a waiting caller whose ctx is still live calls f
// itself. A caller whose ctx is done while it waits returns ctx.Err().
func (m *Map[K, V]) Get(ctx context.Context, k K, f func(context.Context) (V, error)) (V, error) {
	for {
		m.mu.Lock()
		if m.entries == nil {
			m.entries = map[K]*entry[V]{}
		}
		e, ok := m.entries[k]
		if !ok {
			e = &entry[V]{done: make(chan struct{})}
			m.entries[k] = e
			m.mu.Unlock()
			return m.fill(ctx, k, e, f)
		}
		e.parked++
		m.mu.Unlock()

		select {
		case <-e.done:
		case <-ctx.Done():
			var zero V
			return zero, ctx.Err()
		}
		if e.dropped {
			continue
		}
		return e.value, e.err
	}
}

// fill calls f for the entry e it just created under k, publishes the result
// and closes e.done. When f panics, the entry is dropped before the panic
// continues, so no waiter blocks on it forever.
func (m *Map[K, V]) fill(ctx context.Context, k K, e *entry[V], f func(context.Context) (V, error)) (V, error) {
	published := false
	defer func() {
		if published {
			return
		}
		m.drop(k, e)
		close(e.done)
	}()

	e.value, e.err = f(ctx)
	if e.err != nil && ctx.Err() != nil {
		m.drop(k, e)
	}
	published = true
	close(e.done)
	return e.value, e.err
}

// drop marks e dropped and removes it from the map.
func (m *Map[K, V]) drop(k K, e *entry[V]) {
	e.dropped = true
	m.mu.Lock()
	delete(m.entries, k)
	m.mu.Unlock()
}
