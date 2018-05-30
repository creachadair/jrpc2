package jrpc2

import (
	"context"
	"sync"
)

// MetricsWriter returns a metrics writer associated with the given context, or
// nil if ctx doees not have a metrics writer.
func MetricsWriter(ctx context.Context) *Metrics {
	if v := ctx.Value(metricsWriterKey); v != nil {
		return v.(*Metrics)
	}
	return nil
}

const metricsWriterKey = requestContextKey("metrics-writer")

// A Metrics value collects counters and maximum value trackers.  A nil
// *Metrics is valid, and discards all metrics. A *Metrics value is safe for
// concurrent use by multiple goroutines.
type Metrics struct {
	mu      sync.Mutex
	counter map[string]int64
	maxVal  map[string]int64
}

// NewMetrics creates a new, empty metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{counter: make(map[string]int64), maxVal: make(map[string]int64)}
}

// Count adds n to the current value of the counter named, defining the counter
// if it does not already exist.
func (m *Metrics) Count(name string, n int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.counter[name] += n
	}
}

// SetMaxValue sets the maximum value metric named to the greater of n and its
// current value, defining the value if it does not already exist.
func (m *Metrics) SetMaxValue(name string, n int64) {
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
func (m *Metrics) CountAndSetMax(name string, n int64) {
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
func (m *Metrics) Snapshot(counters, maxValues map[string]int64) {
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
