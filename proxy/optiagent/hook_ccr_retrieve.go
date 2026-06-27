// Package optiagent — CCRRetrieveHook.
//
// The CCR (Compress-Cache-Retrieve) pipeline is split across
// three hooks:
//
//   1. CCRCompressHook (BeforeRequest, priority 800):
//      canonicalizes the payload and computes a stable
//      sha256(CCRCompressedPayload). The hash is exposed
//      via hctx.SetFeature("ccr_hash", ...).
//   2. CCRRetrieveHook (this file, BeforeRequest, priority 750):
//      looks up `ccr:<hash>` in Redis. If a response was
//      previously stored under that hash, short-circuit
//      the request with HTTP 200 + the cached body. This
//      is the *read* side of the CCR cache.
//   3. CCRStoreHook (AfterResponse, priority 850): writes
//      the upstream response under `ccr:<hash>` if the
//      status is 200. This is the *write* side.
//
// Priority ordering is deliberate: Retrieve (750) must run
// BEFORE Compress (800) so that the hit can short-circuit
// before we do any work. Store (850) runs in AfterResponse
// after the upstream has returned; it doesn't matter where
// in the AfterResponse chain it sits because every other
// AfterResponse hook is a no-op for our purposes (the
// available ones in this codebase: Fingerprint, LoopDetection
// which already runs in BeforeRequest, etc.).
//
// Storage layout:
//
//	ccr:<sha256-hex>   STRING, the JSON response bytes, TTL
//	                   CCRRetrieveTTL (default 1h). The hash
//	                   key is the same one CompressHook set
//	                   on the hctx feature map.
//
// Why a separate TTL from the L1/L2 caches? CCR is a
// *semantic* cache. The hash is stable across byte-different
// inputs that are semantically equivalent. A user who edits
// their request from "translate to French" to "traduis en
// français" gets the same response from CCR. The TTL on
// CCR is therefore the *semantic* staleness budget: how
// long do we trust that two semantically equivalent prompts
// should produce the same response? In practice 1h is
// generous; most agent prompts don't change their semantic
// meaning within an hour.
package optiagent

import (
	"context"
	"log"
	"time"

	"synapse-proxy/internal/metrics"
)

// CCRRetrieveTTL is how long a CCR response stays in Redis.
// Configurable per-deployment via SetCCRRetrieveTTL at
// boot, default 1h.
var CCRRetrieveTTL = time.Hour

// CCRRetrieveHook is the read side of the CCR cache. See
// the package comment for the full pipeline and storage
// layout.
type CCRRetrieveHook struct{}

// Name returns the hook name used in metrics and log lines.
func (h *CCRRetrieveHook) Name() string { return "ccr_retrieve" }

// Priority places CCRRetrieve BEFORE CCRCompress (800) so
// a hit short-circuits the rest of the BeforeRequest chain.
// It also runs AFTER the auth/L0/L1/L2 short-circuit checks
// (which sit at the proxy layer, not in the hook pipeline).
func (h *CCRRetrieveHook) Priority() int { return 750 }

// BeforeRequest reads `ccr:<hash>` from Redis. If the key
// exists, it short-circuits the request with HTTP 200 and
// the cached body. The contract is:
//
//   - hctx.ShortCircuitStatus = 200   (the cache hit status)
//   - hctx.ShortCircuitBody   = bytes (the cached response)
//
// The proxy's RunBeforeHooks reads these fields and skips
// upstream entirely if ShortCircuitStatus is set.
//
// Failure mode: any error (Redis down, key missing, network
// timeout) results in a no-op. Cache is a perf optimization;
// we'd rather hit the upstream than 5xx the client.
func (h *CCRRetrieveHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx == nil {
		return nil, nil
	}

	hashVal, ok := hctx.Feature("ccr_hash")
	if !ok || hashVal == nil {
		// CCR CompressHook didn't run (or the payload wasn't
		// a chat-completion). Nothing to retrieve.
		return nil, nil
	}
	hash, ok := hashVal.(string)
	if !ok || hash == "" {
		return nil, nil
	}

	// Guard against nil backend (Redis not initialized, or
	// init failed). Fail open.
	backend := getCCRBackend()
	if backend == nil {
		return nil, nil
	}

	key := "ccr:" + hash
	body, err := backend.Get(ctx, key)
	if err != nil {
		// Cache miss is not an error: it's the common path
		// for the first request in a window. We log at debug
		// level to avoid log spam, and any *unexpected* error
		// (timeout, conn refused) is captured here and the
		// hook fails open.
		if err.Error() != "redis: nil" {
			log.Printf("[%s] miss/err for key=%s vk=%s: %v", h.Name(), key, hctx.VK, err)
		}
		return nil, nil
	}
	if len(body) == 0 {
		return nil, nil
	}

	// Cache hit: short-circuit the request. The proxy will
	// see ShortCircuitStatus != 0 and skip the upstream call.
	hctx.ShortCircuitStatus = 200
	hctx.ShortCircuitBody = body
	hctx.SetFeature("ccr_cache_hit", hash)
	// P1.5 DASHBOARD FIRST: bump the per-hook metrics.
	// We track the hit count (token count and cost are
	// 0 because the hook doesn't parse the response) and
	// the bytes saved (the size of the cached response
	// that was served from cache instead of upstream).
	metrics.RecordCCRCacheHit("retrieve", 0, 0)
	metrics.RecordCCRCacheHitBytes("retrieve", uint64(len(body)))
	log.Printf("[%s] HIT key=%s vk=%s bytes=%d", h.Name(), key, hctx.VK, len(body))
	return nil, nil
}

// AfterResponse is a no-op for the retrieve side. The
// complementary write side is CCRStoreHook.
func (h *CCRRetrieveHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// IsEnabled returns true. CCR is the second-most-impactful
// cache level (after L0 dedup) for a typical agent
// workload, and the hook is a single Redis GET — essentially
// free. We don't gate it behind a feature flag.
func (h *CCRRetrieveHook) IsEnabled(vk string) bool { return true }

// init registers the hook with the global hook registry.
func init() {
	RegisterHook(&CCRRetrieveHook{})
	log.Printf("[hooks] registered CCRRetrieveHook at priority 750")
}
