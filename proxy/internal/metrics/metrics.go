// Package metrics holds lightweight in-process counters used by panic
// recovery and (in the future) Prometheus exposition. Kept separate
// from the dashboard and workers packages so any handler can import it
// without pulling in Postgres / Redis.
package metrics

import (
	"sync"
	"sync/atomic"
)

// panicCounts is a map from handler name → panic count since process
// start. Counters are 64-bit atomic so reads from the future
// /healthz / /metrics handlers are race-free with writes from the
// defer-recover blocks.
var (
	panicCountsMu sync.RWMutex
	panicCounts   = make(map[string]*uint64)
)

// RecordPanic increments the panic counter for the given handler name.
// Safe to call from any goroutine, including panic-recover defers.
func RecordPanic(handler string) {
	var c *uint64
	panicCountsMu.RLock()
	c = panicCounts[handler]
	panicCountsMu.RUnlock()
	if c == nil {
		panicCountsMu.Lock()
		c = panicCounts[handler]
		if c == nil {
			var zero uint64
			c = &zero
			panicCounts[handler] = c
		}
		panicCountsMu.Unlock()
	}
	atomic.AddUint64(c, 1)
}

// PanicCount returns the panic count for a handler (0 if no panic ever).
func PanicCount(handler string) uint64 {
	panicCountsMu.RLock()
	defer panicCountsMu.RUnlock()
	if c, ok := panicCounts[handler]; ok {
		return atomic.LoadUint64(c)
	}
	return 0
}

// PanicSnapshot returns a copy of all handler panic counts. Useful for
// /healthz and Prometheus exposition later.
func PanicSnapshot() map[string]uint64 {
	panicCountsMu.RLock()
	defer panicCountsMu.RUnlock()
	out := make(map[string]uint64, len(panicCounts))
	for k, v := range panicCounts {
		out[k] = atomic.LoadUint64(v)
	}
	return out
}
