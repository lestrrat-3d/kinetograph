package memo

// Parked returns how many callers have waited on k's current entry, and
// whether k has an entry. Tests use it to know a caller is parked before they
// let the call it waits on finish.
func (m *Map[K, V]) Parked(k K) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[k]
	if !ok {
		return 0, false
	}
	return e.parked, true
}
