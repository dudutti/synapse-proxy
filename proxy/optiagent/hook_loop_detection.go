// Package optiagent — LoopDetectionHook.
//
// Migrated from proxy.go (formerly lines ~441-505 inline block).
//
// Behaviour preserved from the legacy inline code:
//   - On the FIRST call in a window: no short-circuit, return early.
//   - On the 2nd call: IsLoop=true but ShouldReuse=false → no
//     short-circuit, the request still goes upstream so the
//     response can be cached for the 3rd+ call.
//   - On the 3rd+ call with kill switch OFF: set
//     ShortCircuitStatus=200 with the cached first response in
//     ShortCircuitBody. The proxy serves it without hitting upstream.
//   - On the 3rd+ call with kill switch ON: set
//     ShortCircuitStatus=400 with a self-correction hint body.
//   - Fail open on Redis errors.
//
// State machine (per VK + payload hash):
//   counter (ZSET  synapse:loops:<vk>:<hash>)       — rolling 60s window
//   cache  (STRING synapse:loops:<vk>:<hash>:first) — first-call response
//
// Priority 150: runs after ToolDedup (140) and before cache
// mutators (200+). A detected loop should short-circuit BEFORE
// L1/L2/L3 cache lookups — serving the cached first response
// without re-checking the cache saves a Redis round-trip.

package optiagent

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// loopWindowSecs is the rolling time window for the loop counter.
// Matches the legacy LOOP_WINDOW_SECS constant.
const loopWindowSecs = 60

// loopThreshold is the count at which a loop is considered
// "detected" and we start short-circuiting. Matches the legacy
// LOOP_THRESHOLD (=3).
const loopThreshold = 3

// loopKeyTTL is how long the loop counter ZSET lives in Redis
// without new entries before being forgotten. 10s of slack past
// the window so a slow request doesn't drop its own entry.
const loopKeyTTL = 70 * time.Second

// LoopDetectionHook detects runaway agents that retry the same
// tool call in a tight loop.
type LoopDetectionHook struct{}

// Name returns the stable hook identifier.
func (h *LoopDetectionHook) Name() string { return "loop_detection" }

// Priority 150: between ToolDedup (140) and the cache mutators (200+).
func (h *LoopDetectionHook) Priority() int { return 150 }

// IsEnabled gates on a non-empty VK. BeforeRequest is the real gate.
func (h *LoopDetectionHook) IsEnabled(vk string) bool { return vk != "" }

// BeforeRequest applies the loop detector. See file docstring.
//
// Returns (nil, nil) on the happy path and on backend failure
// (fail-open). On short-circuit (kill switch or cached reuse):
// ShortCircuitStatus and Body are set; the runner handles the
// response write.
func (h *LoopDetectionHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(h.Name(), hctx.VK)

	// Bail out if there's no payload to fingerprint. Without a
	// payload hash, we have no way to group identical calls.
	payload := currentPayload(hctx)
	if len(payload) == 0 {
		return nil, nil
	}
	payloadHash := sha256Hex(payload)

	backend := currentSessionCBBackend()
	if backend == nil {
		return nil, nil
	}

	zKey := "synapse:loops:" + hctx.VK + ":" + payloadHash
	firstKey := zKey + ":first"

	// 1. Evict entries outside the rolling window.
	cutoff := strconv.FormatInt(time.Now().Add(-loopWindowSecs*time.Second).UnixNano(), 10)
	if err := backend.ZRemRangeByScore(ctx, zKey, "-inf", cutoff); err != nil {
		log.Printf("[loop-detect] ZRemRangeByScore failed for vk=%s: %v (fail-open)", hctx.VK, err)
		return nil, nil
	}

	// 2. Count how many calls in the window.
	count, err := backend.ZCard(ctx, zKey)
	if err != nil {
		log.Printf("[loop-detect] ZCard failed for vk=%s: %v (fail-open)", hctx.VK, err)
		return nil, nil
	}

	// 3. Record this call.
	member := fmt.Sprintf("%d", time.Now().UnixNano())
	if err := backend.ZAdd(ctx, zKey, float64(time.Now().UnixNano()), member); err != nil {
		log.Printf("[loop-detect] ZAdd failed for vk=%s: %v (fail-open)", hctx.VK, err)
		// Fall through: the rest of the detection can still run.
	}
	_ = backend.Expire(ctx, zKey, loopKeyTTL)

	loopCount := int(count) + 1

	if loopCount < loopThreshold {
		// 1st or 2nd call in the window — no short-circuit, the
		// proxy still hits upstream so we can cache the first
		// response for the 3rd+ call.
		return nil, nil
	}

	// 3rd+ call: kill switch wins if enabled.
	killSwitch, _ := hctx.Feature("kill_switch")
	if killB, ok := killSwitch.(bool); ok && killB {
		body := makeSelfCorrectionHint(hctx.Model)
		hctx.ShortCircuitStatus = http.StatusBadRequest
		hctx.ShortCircuitBody = body
		// Surface the kind of short-circuit so the proxy can emit
		// the right telemetry row (loop_kill_switch).
		hctx.SetFeature("loop_short_circuit_kind", "kill_switch")
		log.Printf("[loop-detect] KILL SWITCH FIRED for vk=%s (count=%d)", hctx.VK, loopCount)
		return nil, nil
	}

	// 3rd+ call without kill switch: serve cached first response.
	cached, err := backend.Get(ctx, firstKey)
	if err != nil {
		log.Printf("[loop-detect] Get failed for vk=%s: %v (fail-open)", hctx.VK, err)
		return nil, nil
	}
	if len(cached) > 0 {
		_ = backend.Expire(ctx, firstKey, loopKeyTTL)
		hctx.ShortCircuitStatus = http.StatusOK
		hctx.ShortCircuitBody = cached
		hctx.SetFeature("loop_short_circuit_kind", "loop_reuse")
		log.Printf("[loop-detect] LOOP HIT (count=%d) vk=%s — serving cached first response", loopCount, hctx.VK)
		return nil, nil
	}

	// Cached response missing (expired, evicted, or first call's
	// upstream errored). The legacy code fell through to a normal
	// upstream call; we preserve that exact behaviour so the proxy
	// can refresh the cache via StoreLoopFirstResponse on the
	// response path.
	return nil, nil
}

// AfterResponse stores the upstream response as the "first" for
// future loop-detection hits. Mirrors the legacy
// StoreLoopFirstResponse call that proxy.go made from
// streamResponse.
//
// Behaviour: only stores the response if the request is the FIRST
// call in a loop window (loopCount==1). Subsequent calls
// (2nd/3rd/etc) are skipped because the first response has
// already been cached and we don't want to clobber it with a
// later (potentially looped) response.
func (h *LoopDetectionHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	if hctx == nil || len(hctx.UpstreamResponse) == 0 {
		return nil, nil
	}
	payload := currentPayload(hctx)
	if len(payload) == 0 {
		return nil, nil
	}
	payloadHash := sha256Hex(payload)

	backend := currentSessionCBBackend()
	if backend == nil {
		return nil, nil
	}
	zKey := "synapse:loops:" + hctx.VK + ":" + payloadHash
	firstKey := zKey + ":first"

	// Check if this was the first call in the window. The legacy
	// inline code only stored the first response; subsequent
	// calls do not clobber it.
	count, err := backend.ZCard(ctx, zKey)
	if err != nil {
		log.Printf("[loop-detect] AfterResponse ZCard failed: %v (skipping store)", err)
		return nil, nil
	}
	if count > 1 {
		// Not the first call — the cache is already populated or
		// will be populated by the actual first call.
		return nil, nil
	}
	if err := backend.Set(ctx, firstKey, hctx.UpstreamResponse, loopKeyTTL); err != nil {
		log.Printf("[loop-detect] AfterResponse Set failed: %v", err)
	}
	return nil, nil
}

// makeSelfCorrectionHint builds a self-correction hint body. The
// legacy proxy.go used makeSelfCorrectionResponse(""); we mirror
// its shape so the kill-switch response stays byte-equivalent to
// the original.
func makeSelfCorrectionHint(model string) []byte {
	// OpenAI-compatible error shape so OpenAI SDK clients parse
	// it cleanly.
	return []byte(`{"error":{"message":"Agent Firewall: loop detected, please change strategy","type":"loop_kill_switch","code":400}}`)
}

func init() {
	RegisterHook(&LoopDetectionHook{})
}