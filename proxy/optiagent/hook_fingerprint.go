// Package optiagent — Fingerprint Loop Detect hook.
//
// This hook is the first production migration of an existing
// in-proxy.go behaviour into the hook pipeline (Sprint 0, étape 2).
// It detects runaway agents that retry the same (tool_name, args)
// tuple too many times within the FingerprintWindowSecs window and
// surfaces the observation via X-Synapse-Fingerprint-* response
// headers. A subsequent soft-loop injection (also done here, to
// preserve exact behaviour) appends a system warning to nudge the
// LLM out of the loop without triggering the kill switch.
//
// # Why migrate
//
// Before this hook, the fingerprint logic lived inline in
// proxy.go (lines 282-297) along with the soft-loop injection
// (line 504). Mixing counting logic with response-header writes
// makes the proxy handler hard to test and forces every future
// feature to re-parse the same patterns. Extracting into a hook:
//
//   - Removes ~50 lines from proxy.go.
//   - Makes the behaviour unit-testable in isolation.
//   - Allows runtime enable/disable via IsEnabled() without code
//     changes.
//   - Lets the future CCR / ContentRouter / OutputShaper hooks run
//     before or after fingerprint via Priority().
//
// # Behavioural parity
//
// This hook preserves the EXACT semantics of the original inline
// code. The integration test (hook_fingerprint_test.go) asserts
// byte-equality of behaviour for the following scenarios:
//
//   - VK without FingerprintLoopDetect enabled → no Redis access,
//     no header writes.
//   - VK with feature enabled, no tool calls → headers not written.
//   - VK with feature enabled, fp.IsLoop=true → X-Synapse-Fingerprint-*
//     headers written, PushTelemetry fired with event=fingerprint_observed.
//   - VK with feature enabled, KillSwitch=true → loop behaviour is
//     handed off to the loop_detect hook (out of scope here; we
//     observe only).
//   - Soft-loop injection (after fingerprint count crosses threshold
//     and cache missed) → appends system warning to the payload.
//     Same text as the legacy injectSystemWarning() function.
//
// # Fail-open
//
// All Redis errors are swallowed (matches legacy behaviour). A nil
// rdb disables the hook entirely.

package optiagent

import (
	"context"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// FingerprintHook runs the fingerprint loop detection + soft-loop
// injection that used to be inline in proxy.go.
//
// Singleton — RegisterHook is called from init().
type FingerprintHook struct{}

// Name returns the stable hook identifier.
func (f *FingerprintHook) Name() string { return "fingerprint" }

// Priority 100 = early observation hook (counters + headers).
func (f *FingerprintHook) Priority() int { return 100 }

// IsEnabled consults the per-VK FingerprintLoopDetect flag. The
// caller (proxy.go) passes it via Features["fingerprint_loop_detect"]
// so we don't depend on Redis directly here. Future work: read the
// flag from Redis in this hook (skip the proxy.go plumbing).
func (f *FingerprintHook) IsEnabled(vk string) bool {
	if vk == "" {
		return false
	}
	v, ok := fingerprintEnabled.Load(vk)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// fingerprintEnabled is a process-local cache of the per-VK
// FingerprintLoopDetect flag. Populated by SetFingerprintEnabled
// from proxy.go (which reads it from the VirtualKeyConfig). Refreshed
// on every request — keeps the hook testable and avoids a Redis
// round-trip on the hot path.
//
// Thread-safe via sync.Map (atomic Load/Store).
var fingerprintEnabled sync.Map // map[string]bool

// SetFingerprintEnabled is called by proxy.go after reading the
// VirtualKeyConfig. Cheap; called once per request.
func SetFingerprintEnabled(vk string, enabled bool) {
	if vk == "" {
		return
	}
	fingerprintEnabled.Store(vk, enabled)
}

// ClearFingerprintEnabledCache is test-only.
func ClearFingerprintEnabledCache() {
	fingerprintEnabled.Range(func(k, _ interface{}) bool {
		fingerprintEnabled.Delete(k)
		return true
	})
}

// BeforeRequest runs the fingerprint counter increment + sets
// X-Synapse-Fingerprint-* headers on the response. The soft-loop
// injection (mutating the payload) is also done here to preserve
// the legacy single-place behaviour; we keep it because the
// fingerprint observation AND the warning injection are tightly
// coupled (same fp.ToolName, same payload mutation).
func (f *FingerprintHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(f.Name(), hctx.VK)

	// No Redis → no fingerprint observation. The legacy inline code
	// already short-circuits when rdb is nil; we mirror that here.
	rdb := fingerprintRedis()
	if rdb == nil {
		return nil, nil
	}

	payload := currentPayload(hctx)
	if len(payload) == 0 {
		return nil, nil
	}

	fp := CheckToolFingerprint(ctx, rdb, hctx.VK, payload)
	if !fp.IsLoop {
		return nil, nil
	}

	// Surface the observation. proxy.go reads these headers later to
	// emit PushTelemetry and decide on the soft-loop injection. We
	// mirror the legacy contract so the proxy-side integration stays
	// minimal (only the telemetry block needs adjustment).
	hctx.SetHeader("X-Synapse-Fingerprint-Count", strconv.Itoa(fp.LoopCount))
	hctx.SetHeader("X-Synapse-Fingerprint-Tool", fp.ToolName)

	log.Printf("[fingerprint-hook] OBSERVED: tool=%s count=%d (vk=%s)",
		fp.ToolName, fp.LoopCount, hctx.VK)

	// Soft-loop injection: same threshold + same text as legacy.
	// Done here (not in a separate hook) because the legacy code did
	// it inline and we want byte-equivalent behaviour. The kill
	// switch is the orchestrator's call — this hook only injects
	// when the kill switch is OFF.
	killSwitch, _ := hctx.Feature("kill_switch")
	if killSwitchB, ok := killSwitch.(bool); !ok || !killSwitchB {
		count := fp.LoopCount
		if count >= FingerprintThreshold {
			toolName := fp.ToolName
			log.Printf("[fingerprint-hook] SOFT LOOP INJECTION: tool=%s count=%d (vk=%s) — injecting warning into prompt and continuing",
				toolName, count, hctx.VK)

			modified := InjectSystemWarningCompat(payload, toolName)
			if modified != nil {
				hctx.SetHeader("X-Synapse-Soft-Loop-Injected", "1")
				return modified, nil
			}
		}
	}

	return nil, nil
}

// AfterResponse is a no-op for this hook (fingerprint only acts on
// the request side).
func (f *FingerprintHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(f.Name(), hctx.VK)
	return nil, nil
}

// --- helpers ---------------------------------------------------------

// fingerprintRedis returns the process-global Redis client. nil if
// the database layer hasn't been initialised yet (cold start).
func fingerprintRedis() *redis.Client { return fingerprintRDB.Load() }

// fingerprintRDB is the Redis client used by fingerprint hooks.
// Populated by SetFingerprintRedis from main.go on startup.
var fingerprintRDB atomic.Pointer[redis.Client]

// SetFingerprintRedis is called once at startup with the global
// Redis client. Stored in an atomic.Pointer for lock-free reads on
// the hot path.
func SetFingerprintRedis(c *redis.Client) {
	fingerprintRDB.Store(c)
}

// currentPayload returns the payload the hook should inspect. The
// runner passes the optimized payload (post-L3) to BeforeRequest;
// if it's empty we fall back to the raw input.
func currentPayload(hctx *HookContext) []byte {
	if hctx.OptimizedPayload != nil {
		return hctx.OptimizedPayload
	}
	return hctx.RawPayload
}

// FingerprintObservationTTL is how long a fingerprint observation
// remains visible to the proxy (header-based). After this time, the
// soft-loop injection stops firing for the same loop. Matches the
// legacy FingerprintWindowSecs (30s).
const FingerprintObservationTTL = 30 * time.Second

func init() {
	RegisterHook(&FingerprintHook{})
}
