// Package optiagent — CCRStoreHook.
//
// This is the *write* side of the CCR cache. After the
// upstream has responded, this hook writes the response
// body to Redis under `ccr:<hash>` (the same hash
// CCRCompressHook set on the hctx feature map and
// CCRRetrieveHook looks up).
//
// We use the "set if not exists" pattern (SET ... NX) so
// that two concurrent requests for the same canonical
// payload do not overwrite each other. The first writer
// wins. This is the right semantic for a cache: the first
// answer we got is good enough; the second is redundant.
//
// We also gate on UpstreamStatus == 200. 4xx and 5xx must
// not be cached: a transient upstream bug cached for the
// full TTL would mean the next N requests get the same
// bad response. The retry-after / rate-limit semantics
// are upstream's responsibility, not ours.
//
// Reference: headroom/docs/ccr.md (Headroom's CCR has the
// same gating but stores the response in a separate
// CompressionStore; we reuse Redis for simplicity).
package optiagent

import (
	"context"
	"log"

	"synapse-proxy/internal/metrics"
	"synapse-proxy/internal/utils"
)

// CCRStoreHook writes the upstream response to Redis under
// the CCR cache key, after a successful upstream call.
type CCRStoreHook struct{}

// Name returns the hook name used in metrics and log lines.
func (h *CCRStoreHook) Name() string { return "ccr_store" }

// Priority places CCRStore in the AfterResponse chain. The
// exact priority doesn't matter much (every other
// AfterResponse hook in this codebase is a no-op for the
// store path), but we put it last so any hook that wants
// to inspect the response (e.g. a future metrics hook)
// runs first.
func (h *CCRStoreHook) Priority() int { return 850 }

// BeforeRequest is a no-op. The store side only writes in
// AfterResponse.
func (h *CCRStoreHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	return nil, nil
}

// AfterResponse persists the upstream response to Redis
// under `ccr:<hash>` if:
//   - the upstream status is 200 (only successes are cached)
//   - the ccr_hash feature is set (CompressHook ran)
//   - the response body is non-empty
//
// The write uses SET NX so a concurrent write from a
// race-loser request does not overwrite the race-winner's
// response. This is the canonical pattern for distributed
// cache writes: first writer wins.
func (h *CCRStoreHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	if hctx == nil {
		return nil, nil
	}

	// Only cache successful responses. 4xx and 5xx are
	// transient by definition; caching them would propagate
	// the failure for the full TTL.
	if hctx.UpstreamStatus != 200 {
		return nil, nil
	}
	if len(hctx.UpstreamResponse) == 0 || utils.IsCachedResponseAnError(hctx.UpstreamResponse) {
		return nil, nil
	}

	hashVal, ok := hctx.Feature("ccr_hash")
	if !ok || hashVal == nil {
		// CCR CompressHook didn't run for this request.
		// Nothing to store.
		return nil, nil
	}
	hash, ok := hashVal.(string)
	if !ok || hash == "" {
		return nil, nil
	}

	backend := getCCRBackend()
	if backend == nil {
		return nil, nil
	}

	key := "ccr:" + hash
	// SET NX: only set if the key does not exist. The first
	// writer wins. We use the same TTL as the retrieve
	// side (CCRRetrieveTTL) so the two sides of the cache
	// agree on lifetime.
	stored, err := backend.SetNX(ctx, key, hctx.UpstreamResponse, CCRRetrieveTTL)
	if err != nil {
		// Log at warn level. A failed store is not a fatal
		// error — the next request will just hit the
		// upstream and try again.
		log.Printf("[%s] store failed key=%s vk=%s: %v", h.Name(), key, hctx.VK, err)
		return nil, nil
	}
	if !stored {
		// A concurrent request already wrote the response.
		// First writer wins. We log at debug level because
		// this is the expected path under contention.
		log.Printf("[%s] race-lost (key already exists) key=%s vk=%s", h.Name(), key, hctx.VK)
		return nil, nil
	}
	log.Printf("[%s] STORED key=%s vk=%s bytes=%d", h.Name(), key, hctx.VK, len(hctx.UpstreamResponse))
	// P1.5 DASHBOARD FIRST: bump the per-hook metrics
	// for the CCR lookup (write side). The "lookup"
	// kind is distinct from the "retrieve" (read/hit)
	// kind. We also record the bytes written to the
	// cache (this is not a "saving" per se but it's the
	// cache size growth, useful for the dashboard).
	metrics.RecordCCRCacheHit("lookup", 0, 0)
	metrics.RecordCCRCacheHitBytes("lookup", uint64(len(hctx.UpstreamResponse)))
	return nil, nil
}

// IsEnabled returns true. The store side is essentially
// free (a single Redis SET per request) and the data it
// writes is exactly the data the next request will look
// up via CCRRetrieveHook. Gating it would defeat the
// purpose of having a CCR cache.
func (h *CCRStoreHook) IsEnabled(vk string) bool { return true }

// init registers the hook with the global hook registry.
func init() {
	RegisterHook(&CCRStoreHook{})
	log.Printf("[hooks] registered CCRStoreHook at priority 850")
}
