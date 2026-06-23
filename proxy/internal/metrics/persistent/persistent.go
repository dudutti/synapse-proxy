// Package persistent persists in-process Prometheus counters to a Redis
// hash so the SUPERADMIN HUD shows non-zero totals across proxy
// restarts and crashes.
//
// Why: the metrics package keeps counters in process memory
// (sync/atomic uint64s). On any proxy restart those counters reset
// to zero, so the dashboard's "Upstream requests", "Cache hits",
// "$ saved (proxy)" gauges go back to 0 even though the totals in
// Postgres / Redis still reflect the full history.
//
// Design:
//
//   * Every counter increment is also submitted to a buffered channel
//     (deltas).
//   * A background goroutine drains the channel every 2s and ships
//     the coalesced deltas to Redis in a single pipeline.
//   * On boot, hydrateFromRedis reads the cumulative HASH and seeds
//     a "hydrated" map; WritePrometheus in metrics.go sums the
//     in-memory + hydrated values when rendering the text.
//
// The hot path NEVER blocks on Redis. If the channel is full, the
// delta is dropped (the in-memory counter still increments, so the
// current-process /metrics stays accurate; the next process picks up
// the missing increments via the Postgres / RequestLog aggregate
// endpoint instead).
//
// Key shape: `synapse:metrics:cumulative` (HASH)
//   cache_hits_total{L0} = 12.0
//   cache_hits_total{L1} = 4208.0
//   tokens_saved_total{L3} = 89234.0
//   cost_saved_millicents_total{L3} = 320.0
//   panics_total{ProxyHandler} = 2.0
//   upstream_requests_total = 12211.0
//   upstream_errors_total = 8.0
package persistent

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"synapse-proxy/internal/db"
)

const redisKey = "synapse:metrics:cumulative"

type pendingDelta struct {
	field string
	delta float64
}

var (
	deltaCh  chan pendingDelta
	initOnce sync.Once

	muHydrated sync.RWMutex
	hydrated   = make(map[string]float64)
)

// Init starts the background flusher and hydrates the in-process
// counters from whatever Redis already has from prior runs. Safe to
// call once at boot; subsequent calls are no-ops.
func Init() {
	initOnce.Do(func() {
		deltaCh = make(chan pendingDelta, 4096)
		go hydrateFromRedis()
		go flushLoop()
	})
}

// Submit records a counter delta. Non-blocking: returns immediately if
// the buffer is full and silently drops. The in-memory counter
// (in metrics.go) already increments atomically, so /metrics stays
// consistent within the current process; the Redis mirror is only for
// cross-restart persistence.
func Submit(field string, delta float64) {
	if deltaCh == nil {
		return
	}
	select {
	case deltaCh <- pendingDelta{field, delta}:
	default:
		// buffer full; drop
	}
}

func flushLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		drainAndFlush()
	}
}

func drainAndFlush() {
	if len(deltaCh) == 0 {
		return
	}
	rdb := db.GetRedis()
	if rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Coalesce up to 1000 deltas per flush into a single Redis pipeline.
	pending := make(map[string]float64, 64)
	for i := 0; i < 1000; i++ {
		select {
		case d := <-deltaCh:
			pending[d.field] += d.delta
		default:
			i = 1000
		}
	}
	if len(pending) == 0 {
		return
	}

	pipe := rdb.Pipeline()
	for field, v := range pending {
		pipe.HIncrByFloat(ctx, redisKey, field, v)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[metrics persistent] flush failed: %v", err)
	}
}

// hydrateFromRedis reads the cumulative HASH once at boot and seeds
// the hydrated map. WritePrometheus in metrics.go reads from this map
// to add cross-restart totals on top of the in-memory counters.
func hydrateFromRedis() {
	rdb := db.GetRedis()
	if rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	raw, err := rdb.HGetAll(ctx, redisKey).Result()
	if err != nil {
		log.Printf("[metrics persistent] hydrate failed: %v", err)
		return
	}
	if len(raw) == 0 {
		return
	}

	newHydrated := make(map[string]float64, len(raw))
	for k, v := range raw {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			newHydrated[k] = n
		}
	}
	muHydrated.Lock()
	hydrated = newHydrated
	muHydrated.Unlock()
	log.Printf("[metrics persistent] hydrated %d cumulative counters from Redis", len(newHydrated))
}

// Cumulative returns a copy of the persisted cumulative map
// (read-only). WritePrometheus uses this to add cross-restart totals
// on top of the in-memory counters.
func Cumulative() map[string]float64 {
	muHydrated.RLock()
	defer muHydrated.RUnlock()
	out := make(map[string]float64, len(hydrated))
	for k, v := range hydrated {
		out[k] = v
	}
	return out
}