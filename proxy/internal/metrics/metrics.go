// Package metrics holds lightweight in-process counters used by panic
// recovery, health checks, and Prometheus exposition. Kept separate
// from the dashboard and workers packages so any handler can import it
// without pulling in Postgres / Redis.
//
// Design choices:
//   - Counters are 64-bit atomic so reads from the /healthz and
//     /metrics handlers are race-free with writes from any goroutine.
//   - We hand-write the Prometheus text format instead of depending on
//     github.com/prometheus/client_golang. The format is simple, our
//     metric set is small (no histograms, no quantiles), and avoiding
//     the dependency keeps `go build` instant.
//   - Per-cache-level counters live here, incremented by engine.go at
//     hit/miss time.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"synapse-proxy/internal/metrics/persistent"
)

// --- Panic counters ---------------------------------------------------

var (
	panicCountsMu sync.RWMutex
	panicCounts   = make(map[string]*uint64)
)

// RecordPanic increments the panic counter for the given handler name.
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
	persistent.Submit("panics_total{"+handler+"}", 1)
}

// PanicCount returns the panic count for a handler.
func PanicCount(handler string) uint64 {
	panicCountsMu.RLock()
	defer panicCountsMu.RUnlock()
	if c, ok := panicCounts[handler]; ok {
		return atomic.LoadUint64(c)
	}
	return 0
}

func panicSnapshot() map[string]uint64 {
	panicCountsMu.RLock()
	defer panicCountsMu.RUnlock()
	out := make(map[string]uint64, len(panicCounts))
	for k, v := range panicCounts {
		out[k] = atomic.LoadUint64(v)
	}
	return out
}

// --- Cache hit/miss counters ------------------------------------------
//
// One counter per cache level (L0 in-flight, L1 exact, L2 semantic,
// L3 compression, LOOP loop-detect, NONE upstream-miss). Each call to
// RecordCacheHit(N, level) adds N to the counter for that level.
// Tokens saved are tracked separately so we can show $/saved metrics.

var (
	cacheHitCounters   = make(map[string]*uint64)
	cacheSavedTokens   = make(map[string]*uint64)
	cacheSavedCostCents = make(map[string]*uint64) // stored as cents (Ã—1000) for atomicity
	cacheHitsMu        sync.RWMutex
)

// RecordCacheHit records one (or more) cache hits. The cost parameter
// is the dollar amount of savings (we multiply by 1000 and store as
// an integer millicents so we can use atomic ops without floats).
func RecordCacheHit(level string, tokensSaved uint64, costSavedDollars float64) {
	cacheHitsMu.Lock()
	defer cacheHitsMu.Unlock()

	if c := cacheHitCounters[level]; c != nil {
		atomic.AddUint64(c, 1)
	} else {
		var one uint64 = 1
		cacheHitCounters[level] = &one
	}

	if tokensSaved > 0 {
		if c := cacheSavedTokens[level]; c != nil {
			atomic.AddUint64(c, tokensSaved)
		} else {
			cacheSavedTokens[level] = &tokensSaved
		}
	}

	if costSavedDollars > 0 {
		mc := uint64(costSavedDollars * 1000)
		if c := cacheSavedCostCents[level]; c != nil {
			atomic.AddUint64(c, mc)
		} else {
			cacheSavedCostCents[level] = &mc
		}
	}

	// Mirror to Redis (fire-and-forget). Submit is non-blocking and
	// channel-buffered, so holding the mutex is fine.
	persistent.Submit("cache_hits_total{"+level+"}", 1)
	if tokensSaved > 0 {
		persistent.Submit("tokens_saved_total{"+level+"}", float64(tokensSaved))
	}
	if costSavedDollars > 0 {
		persistent.Submit("cost_saved_millicents_total{"+level+"}", costSavedDollars*1000)
	}
}

// CacheHits returns a copy of the per-level hit counts.
func CacheHits() map[string]uint64 {
	cacheHitsMu.RLock()
	defer cacheHitsMu.RUnlock()
	out := make(map[string]uint64, len(cacheHitCounters))
	for k, v := range cacheHitCounters {
		out[k] = atomic.LoadUint64(v)
	}
	return out
}

func cacheSavedTokensSnapshot() map[string]uint64 {
	cacheHitsMu.RLock()
	defer cacheHitsMu.RUnlock()
	out := make(map[string]uint64, len(cacheSavedTokens))
	for k, v := range cacheSavedTokens {
		out[k] = atomic.LoadUint64(v)
	}
	return out
}

func cacheSavedCostCentsSnapshot() map[string]uint64 {
	cacheHitsMu.RLock()
	defer cacheHitsMu.RUnlock()
	out := make(map[string]uint64, len(cacheSavedCostCents))
	for k, v := range cacheSavedCostCents {
		out[k] = atomic.LoadUint64(v)
	}
	return out
}

// --- Upstream latency histogram (very coarse) -------------------------
//
// Histograms would be ideal here but require a Go library. For now we
// keep five simple buckets (counts per latency range) so Prometheus
// can compute a rough p50/p95.
//
//   <10ms, 10-100ms, 100-500ms, 500-2000ms, >=2000ms
//   (plus a separate count for upstream errors, status >= 400)

var (
	upstreamLatencyBuckets = []uint64{0, 0, 0, 0, 0} // 5 buckets
	upstreamErrors         uint64
	upstreamReqs            uint64
	upstreamMu              sync.Mutex
)

func RecordUpstream(latencyMs int, isError bool) {
	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	atomic.AddUint64(&upstreamReqs, 1)
	persistent.Submit("upstream_requests_total", 1)
	if isError {
		atomic.AddUint64(&upstreamErrors, 1)
		persistent.Submit("upstream_errors_total", 1)
		return
	}
	var idx int
	switch {
	case latencyMs < 10:
		idx = 0
	case latencyMs < 100:
		idx = 1
	case latencyMs < 500:
		idx = 2
	case latencyMs < 2000:
		idx = 3
	default:
		idx = 4
	}
	atomic.AddUint64(&upstreamLatencyBuckets[idx], 1)
}

func upstreamSnapshot() (buckets []uint64, errors uint64, total uint64) {
	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	buckets = make([]uint64, len(upstreamLatencyBuckets))
	for i, v := range upstreamLatencyBuckets {
		buckets[i] = atomic.LoadUint64(&v)
	}
	errors = atomic.LoadUint64(&upstreamErrors)
	total = atomic.LoadUint64(&upstreamReqs)
	return
}

// --- Prometheus exposition --------------------------------------------

// WritePrometheus writes the metrics in Prometheus text exposition
// format (https://prometheus.io/docs/instrumenting/exposition_formats/).
// Always returns a 200 with Content-Type "text/plain; version=0.0.4".
//
// Each counter is reported as (in-memory counter for the current
// process) + (persisted cumulative counter from Redis) so the
// SUPERADMIN HUD sees non-zero totals across proxy restarts. The two
// are summed at scrape time, with the cumulative value read from
// persistent.Cumulative() (a hydrated copy of the Redis HASH).
func WritePrometheus(w io.Writer) {
	cum := persistent.Cumulative()

	// Each block: HELP, TYPE, then samples = in-memory + cum[label-key].

	// cache_hits_total (prometheus convention: samples after TYPE).
	hits := CacheHits()
	fmt.Fprintln(w, "# HELP synapse_proxy_cache_hits_total Cache hits since process start, by level")
	fmt.Fprintln(w, "# TYPE synapse_proxy_cache_hits_total counter")
	for _, level := range sortedKeys(hits) {
		cumKey := "cache_hits_total{" + level + "}"
		total := uint64(hits[level]) + uint64(cum[cumKey])
		fmt.Fprintf(w, "synapse_proxy_cache_hits_total{cache_level=%q} %d\n", level, total)
	}

	tokens := cacheSavedTokensSnapshot()
	fmt.Fprintln(w, "# HELP synapse_proxy_tokens_saved_total Tokens saved from cache, by level")
	fmt.Fprintln(w, "# TYPE synapse_proxy_tokens_saved_total counter")
	for _, level := range sortedKeys(tokens) {
		cumKey := "tokens_saved_total{" + level + "}"
		total := tokens[level] + uint64(cum[cumKey])
		fmt.Fprintf(w, "synapse_proxy_tokens_saved_total{cache_level=%q} %d\n", level, total)
	}

	costs := cacheSavedCostCentsSnapshot()
	fmt.Fprintln(w, "# HELP synapse_proxy_cost_saved_total Cost saved in millicents (1/1000 USD), by level")
	fmt.Fprintln(w, "# TYPE synapse_proxy_cost_saved_total counter")
	for _, level := range sortedKeys(costs) {
		cumKey := "cost_saved_millicents_total{" + level + "}"
		total := costs[level] + uint64(cum[cumKey])
		fmt.Fprintf(w, "synapse_proxy_cost_saved_total{cache_level=%q} %d\n", level, total)
	}

	panics := panicSnapshot()
	fmt.Fprintln(w, "# HELP synapse_proxy_panics_total Panics recovered, by handler")
	fmt.Fprintln(w, "# TYPE synapse_proxy_panics_total counter")
	for _, handler := range sortedKeys(panics) {
		cumKey := "panics_total{" + handler + "}"
		total := panics[handler] + uint64(cum[cumKey])
		fmt.Fprintf(w, "synapse_proxy_panics_total{handler=%q} %d\n", handler, total)
	}

	buckets, errors, total := upstreamSnapshot()
	upReqsTotal := total + uint64(cum["upstream_requests_total"])
	upErrsTotal := errors + uint64(cum["upstream_errors_total"])

	labels := []string{"le_10ms", "le_100ms", "le_500ms", "le_2s", "ge_2s"}
	fmt.Fprintln(w, "# HELP synapse_proxy_upstream_latency_seconds Upstream latency in coarse buckets")
	fmt.Fprintln(w, "# TYPE synapse_proxy_upstream_latency_seconds counter")
	for i, label := range labels {
		if i < len(buckets) {
			fmt.Fprintf(w, "synapse_proxy_upstream_latency_seconds_bucket{le=%q} %d\n", label, buckets[i])
		}
	}

	fmt.Fprintln(w, "# HELP synapse_proxy_upstream_requests_total Total upstream requests")
	fmt.Fprintln(w, "# TYPE synapse_proxy_upstream_requests_total counter")
	fmt.Fprintf(w, "synapse_proxy_upstream_requests_total %d\n", upReqsTotal)

	fmt.Fprintln(w, "# HELP synapse_proxy_upstream_errors_total Upstream requests with status >= 400")
	fmt.Fprintln(w, "# TYPE synapse_proxy_upstream_errors_total counter")
	fmt.Fprintf(w, "synapse_proxy_upstream_errors_total %d\n", upErrsTotal)
}

// Handler returns an http.HandlerFunc that writes the Prometheus text
// format to the response. Convenient for `http.Handle("/metrics", ...)`.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		WritePrometheus(w)
	})
}

func sortedKeys(m map[string]uint64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
