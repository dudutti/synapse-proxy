// Package optiagent — hook pipeline metrics.
//
// Lightweight in-process counters for the hook pipeline. Mirrors the
// style of internal/metrics/metrics.go (no Prometheus client_golang
// dependency, hand-written text format). The /metrics endpoint
// already iterates over these via the existing Prometheus exposition
// path in internal/metrics/metrics.go, so we only expose
// hook-relevant counters here.
//
// All counters are best-effort. Failures to record (Redis down,
// mutex contention) MUST NOT propagate to the caller — the hook
// pipeline is on the hot path.
//
// Metric labels:
//
//	synapse_hook_invocations_total{hook,vk,phase}
//	    Counter. phase is "before" or "after".
//
//	synapse_hook_errors_total{hook,vk,kind}
//	    Counter. kind is "panic", "before_error", "after_error",
//	    "disabled_redis", "timeout".
//
//	synapse_hook_latency_ms{hook,vk}
//	    Counter of microseconds (we accumulate a sum and count and
//	    expose the average via Prometheus exposition). Histograms
//	    would be nicer but require an external library.

package optiagent

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"synapse-proxy/internal/metrics/persistent"
)

// --- Per-hook, per-vk counters ----------------------------------------

type hookCounters struct {
	invocations  uint64 // total calls (before+after)
	beforeCalls  uint64
	afterCalls   uint64
	errors       uint64
	latencySumUs uint64
	latencyCount uint64
}

var (
	hookMu          sync.RWMutex
	hookCountersMap = make(map[string]*hookCounters) // key = "hook|vk"
)

// recordOnce records a single hook invocation metric. Safe to call
// from any goroutine.
func recordOnce(key string, fn func(c *hookCounters)) {
	hookMu.RLock()
	c := hookCountersMap[key]
	hookMu.RUnlock()
	if c != nil {
		fn(c)
		return
	}
	hookMu.Lock()
	c = hookCountersMap[key]
	if c == nil {
		c = &hookCounters{}
		hookCountersMap[key] = c
	}
	hookMu.Unlock()
	fn(c)
}

// RecordHookLatency records one BeforeRequest or AfterResponse call
// latency. Cheap — two atomic adds.
func RecordHookLatency(hook, vk string, d time.Duration) {
	key := hook + "|" + vk
	us := uint64(d.Microseconds())
	recordOnce(key, func(c *hookCounters) {
		atomic.AddUint64(&c.invocations, 1)
		atomic.AddUint64(&c.latencySumUs, us)
		atomic.AddUint64(&c.latencyCount, 1)
	})
	persistent.Submit(fmt.Sprintf("hooks_latency_us_total{hook=%q,vk=%q}", hook, vk), float64(us))
}

// RecordHookError increments the error counter for a hook/VK with a
// free-form kind label.
func RecordHookError(hook, vk, kind string) {
	key := hook + "|" + vk
	recordOnce(key, func(c *hookCounters) {
		atomic.AddUint64(&c.errors, 1)
	})
	persistent.Submit(fmt.Sprintf("hooks_errors_total{hook=%q,vk=%q,kind=%q}", hook, vk, kind), 1)
}

// IncrementBefore / IncrementAfter are convenience helpers used by
// the runner (instead of mixing counter increments into the latency
// path). Kept separate so future hooks can register custom counters.
func IncrementBefore(hook, vk string) {
	key := hook + "|" + vk
	recordOnce(key, func(c *hookCounters) {
		atomic.AddUint64(&c.beforeCalls, 1)
		atomic.AddUint64(&c.invocations, 1)
	})
	persistent.Submit(fmt.Sprintf("hooks_invocations_total{hook=%q,vk=%q,phase=\"before\"}", hook, vk), 1)
}

func IncrementAfter(hook, vk string) {
	key := hook + "|" + vk
	recordOnce(key, func(c *hookCounters) {
		atomic.AddUint64(&c.afterCalls, 1)
		atomic.AddUint64(&c.invocations, 1)
	})
	persistent.Submit(fmt.Sprintf("hooks_invocations_total{hook=%q,vk=%q,phase=\"after\"}", hook, vk), 1)
}

// --- Snapshot / exposition -------------------------------------------

type hookSnapshotRow struct {
	Hook         string
	VK           string
	Invocations  uint64
	BeforeCalls  uint64
	AfterCalls   uint64
	Errors       uint64
	LatencyAvgUs float64
}

func hookSnapshot() []hookSnapshotRow {
	hookMu.RLock()
	defer hookMu.RUnlock()
	out := make([]hookSnapshotRow, 0, len(hookCountersMap))
	for k, c := range hookCountersMap {
		var inv, bef, aft, errs, sum, cnt uint64
		inv = atomic.LoadUint64(&c.invocations)
		bef = atomic.LoadUint64(&c.beforeCalls)
		aft = atomic.LoadUint64(&c.afterCalls)
		errs = atomic.LoadUint64(&c.errors)
		sum = atomic.LoadUint64(&c.latencySumUs)
		cnt = atomic.LoadUint64(&c.latencyCount)
		var avg float64
		if cnt > 0 {
			avg = float64(sum) / float64(cnt)
		}
		// split key back into hook|vk
		var hookName, vk string
		for i := 0; i < len(k); i++ {
			if k[i] == '|' {
				hookName = k[:i]
				vk = k[i+1:]
				break
			}
		}
		out = append(out, hookSnapshotRow{
			Hook:         hookName,
			VK:           vk,
			Invocations:  inv,
			BeforeCalls:  bef,
			AfterCalls:   aft,
			Errors:       errs,
			LatencyAvgUs: avg,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hook != out[j].Hook {
			return out[i].Hook < out[j].Hook
		}
		return out[i].VK < out[j].VK
	})
	return out
}

// WritePrometheusHookMetrics writes the hook pipeline metrics in
// Prometheus text exposition format. Called from the existing
// /metrics handler — append, don't replace.
//
// Format:
//
//	# HELP synapse_hook_invocations_total ...
//	# TYPE synapse_hook_invocations_total counter
//	synapse_hook_invocations_total{hook="fingerprint",vk="sk-opti-x",phase="before"} 12
//	...
func WritePrometheusHookMetrics(w io.Writer) {
	rows := hookSnapshot()
	if len(rows) == 0 {
		return
	}

	fmt.Fprintf(w, "# HELP synapse_hook_invocations_total Hook pipeline invocations\n")
	fmt.Fprintf(w, "# TYPE synapse_hook_invocations_total counter\n")
	for _, r := range rows {
		fmt.Fprintf(w, "synapse_hook_invocations_total{hook=%q,vk=%q,phase=\"before\"} %d\n",
			r.Hook, r.VK, r.BeforeCalls)
		fmt.Fprintf(w, "synapse_hook_invocations_total{hook=%q,vk=%q,phase=\"after\"} %d\n",
			r.Hook, r.VK, r.AfterCalls)
	}

	fmt.Fprintf(w, "# HELP synapse_hook_errors_total Hook errors by kind\n")
	fmt.Fprintf(w, "# TYPE synapse_hook_errors_total counter\n")
	for _, r := range rows {
		fmt.Fprintf(w, "synapse_hook_errors_total{hook=%q,vk=%q} %d\n",
			r.Hook, r.VK, r.Errors)
	}

	fmt.Fprintf(w, "# HELP synapse_hook_latency_avg_us Average hook latency in microseconds\n")
	fmt.Fprintf(w, "# TYPE synapse_hook_latency_avg_us gauge\n")
	for _, r := range rows {
		fmt.Fprintf(w, "synapse_hook_latency_avg_us{hook=%q,vk=%q} %.2f\n",
			r.Hook, r.VK, r.LatencyAvgUs)
	}
}
