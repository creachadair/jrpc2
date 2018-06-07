// Package metrics defines a concurrently-accessible metrics collector.
//
// A *metrics.M value exports methods to track integer counters and maximum
// values. A metric has a caller-assigned string name that is not interpreted
// by the collector except to locate its stored value.
package metrics

import "sync"

// An M collects counters and maximum value trackers.  A nil *M is valid, and
// discards all metrics. The methods of an *M are safe for concurrent use by
// multiple goroutines.
type M struct {
	mu      sync.Mutex
	counter map[string]int64
	maxVal  map[string]int64
}

// New creates a new, empty metrics collector.
func New() *M {
	return &M{counter: make(map[string]int64), maxVal: make(map[string]int64)}
}

// Count adds n to the current value of the counter named, defining the counter
// if it does not already exist.
func (m *M) Count(name string, n int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.counter[name] += n
	}
}

// SetMaxValue sets the maximum value metric named to the greater of n and its
// current value, defining the value if it does not already exist.
func (m *M) SetMaxValue(name string, n int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		if n > m.maxVal[name] {
			m.maxVal[name] = n
		}
	}
}

// CountAndSetMax adds n to the current value of the counter named, and also
// updates a max value tracker with the same name in a single step.
func (m *M) CountAndSetMax(name string, n int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		if n > m.maxVal[name] {
			m.maxVal[name] = n
		}
		m.counter[name] += n
	}
}

// Snapshot copies an atomic snapshot of the counters and max value trackers
// into the provided non-nil maps.
func (m *M) Snapshot(counters, maxValues map[string]int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		for name, val := range m.counter {
			counters[name] = val
		}
		for name, val := range m.maxVal {
			maxValues[name] = val
		}
	}
}
