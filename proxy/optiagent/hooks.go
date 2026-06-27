// Package optiagent — request/response Hook pipeline.
//
// This file defines the extension seam used by the proxy to plug in
// CCR (compress-cache-retrieve), ContentRouter (JSON/Logs/Code
// compression), OutputShaper (verbosity steering) and any future
// pipeline extension without growing proxy.go beyond its current
// size.
//
// # Strangler pattern
//
// proxy.go is 1685 lines today and currently inlines fingerprint
// detection, tool dedup, loop detection, compaction hint injection,
// tool-call caching and the recursive tool-loop short-circuit. Adding
// CCR + ContentRouter + OutputShaper inline would balloon it to 3000+
// lines.
//
// Instead, each behavioural unit becomes a Hook:
//
//	type Hook interface {
//	    Name() string
//	    Priority() int
//	    IsEnabled(vk string) bool
//	    BeforeRequest(ctx, hctx) ([]byte, error)
//	    AfterResponse(ctx, hctx) ([]byte, error)
//	}
//
// Hooks are auto-registered via init() in their respective files
// (hook_fingerprint.go, hook_ccr.go, hook_content_router.go,
// hook_output_shaper.go). proxy.go calls RunBeforeHooks and
// RunAfterHooks at two well-defined points in the request lifecycle.
//
// # Failure model: fail-open
//
// Every hook MUST treat errors as non-fatal. A Redis blip, a malformed
// payload, a downstream timeout — none of these may break the user
// request. The runner logs the error, increments a metric, and uses
// the prior payload. The proxy has its own panic recovery on top of
// this; the hook layer is defence in depth, not the only line.
//
// # Performance budget
//
// The whole pipeline must stay under 6ms p50 / 15ms p99. Each hook is
// expected to declare its budget via HookMetrics and respect it.
package optiagent

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Hook is the unit of work in the request/response pipeline.
//
// All hooks are called by ProxyHandler in Priority() order. Each hook
// receives the payload state at its point of execution and returns a
// (possibly modified) payload. Hooks MUST be safe to call
// concurrently — they receive their own HookContext and must not
// share mutable state with other hooks (use Redis or sync primitives).
type Hook interface {
	// Name returns the unique identifier for this hook. Used in
	// metrics labels and feature flags. Must be stable across
	// releases (changing it breaks dashboards).
	Name() string

	// Priority controls execution order. Lower values run first.
	// Suggested buckets:
	//   100  observation / counting (fingerprint, dedup)
	//   200  payload transformation (CCR store, ContentRouter)
	//   300  prompt shaping (OutputShaper)
	//   400  telemetry-only / post-processing
	Priority() int

	// IsEnabled returns true if this hook should run for the given
	// virtual key. Hooks consult the keyconfig / feature flags here.
	// Return true to run, false to skip (no metric emitted).
	IsEnabled(vk string) bool

	// BeforeRequest runs after auth+parse, before L1/L2/L3 cache
	// lookup. Hooks MAY mutate payload (return new []byte) or return
	// the input unchanged. Returning (nil, err) is treated as
	// fail-open by the runner: the error is logged, the prior
	// payload is used.
	BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error)

	// AfterResponse runs after the upstream response is received,
	// before it is cached/written to the client. Hooks MAY mutate
	// the response body. Same fail-open contract as BeforeRequest.
	AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error)
}

// HookContext carries the per-request state shared by hooks.
//
// Each request gets its own HookContext. Hooks MUST NOT share
// references to HookContext across goroutines. The Features map is
// the recommended way to pass data between hooks (e.g. CCR hook
// populates Features.CCRStore, a later retrieval hook reads it).
type HookContext struct {
	// Identity — populated by proxy.go before RunBeforeHooks.
	VK            string
	Provider      string
	Model         string
	AgentID       string
	AgentLabel    string
	SessionID     string
	TurnCount     int
	ConvSignature string

	// Payload chain. RawPayload is the bytes the client sent.
	// OptimizedPayload is the bytes after L3 (cache-level NONE / miss
	// path). Hooks receive the current payload as their argument and
	// return the next one. FinalOptimizedPayload is what gets sent
	// upstream.
	//
	// CCRCompressedPayload is the version of OptimizedPayload after
	// semantic-level canonicalization (whitespace collapse, newline
	// normalization, blank-system-message stripping). Set by
	// CCRCompressHook in BeforeRequest. Read by CCRRetrieveHook
	// to compute the CCR cache key (sha256(CCRCompressedPayload)).
	// The proxy's hot path uses the CCR hash as a third cache
	// lookup level alongside L1 (exact) and L2 (similar).
	RawPayload            []byte
	OptimizedPayload      []byte
	CCRCompressedPayload  []byte
	FinalOptimizedPayload []byte

	// Upstream response (populated before RunAfterHooks).
	UpstreamResponse []byte
	UpstreamStatus   int

	// Response headers the proxy has already committed. Hooks MAY
	// add X-Synapse-* observability headers via SetHeader.
	ResponseHeaders http.Header

	// Short-circuit signals. A hook MAY set ShortCircuitStatus and
	// ShortCircuitBody to terminate the request early (e.g. loop
	// kill switch). The runner detects this and returns the body
	// instead of forwarding upstream.
	ShortCircuitStatus int
	ShortCircuitBody   []byte

	// Cross-hook state. Recommended keys:
	//   "ccr_store"        map[string]CCREntry   — populated by CCR hook
	//   "ccr_retrievals"   int                    — incremented on retrieval
	//   "compression_strategy" string              — "json_array" | "logs" | ...
	//   "shaper_level"     int                    — 0..4, 0 = off
	//   "shaper_arm"       string                 — "treatment" | "holdout"
	Features map[string]interface{}

	// Provider / model routing info populated by proxy.go.
	RealKey     string
	ProviderURL string
	WantStream  bool
	IsBenchmark bool
	StartTime   time.Time
}

// SetHeader sets an X-Synapse-* observability header. Safe to call
// from any hook. Returns false if ResponseHeaders is nil (defensive
// — should never happen in practice).
func (h *HookContext) SetHeader(key, value string) bool {
	if h.ResponseHeaders == nil {
		return false
	}
	h.ResponseHeaders.Set(key, value)
	return true
}

// SetFeature stores a cross-hook value. Returns false if Features is
// nil (defensive).
func (h *HookContext) SetFeature(key string, value interface{}) bool {
	if h.Features == nil {
		return false
	}
	h.Features[key] = value
	return true
}

// Feature fetches a cross-hook value with a type assertion. Returns
// (zero, false) if missing or wrong type.
func (h *HookContext) Feature(key string) (interface{}, bool) {
	if h.Features == nil {
		return nil, false
	}
	v, ok := h.Features[key]
	return v, ok
}

// GetFeature is a convenience wrapper that returns the
// value with no ok flag. If the key is missing, the
// returned value is nil. This is the test-friendly
// version of Feature; production code should use Feature
// to handle missing keys explicitly.
func (h *HookContext) GetFeature(key string) interface{} {
	v, _ := h.Feature(key)
	return v
}

// --- Registry --------------------------------------------------------

var (
	registryMu sync.RWMutex
	hooks      = make([]Hook, 0, 8)
)

// RegisterHook adds a hook to the global registry. Typically called
// from init() in the hook's source file. Duplicate names overwrite
// (the last registration wins — useful for tests).
func RegisterHook(h Hook) {
	if h == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()

	for i, existing := range hooks {
		if existing.Name() == h.Name() {
			hooks[i] = h
			return
		}
	}
	hooks = append(hooks, h)
}

// UnregisterHook removes a hook by name. Returns true if removed.
// Mostly used in tests.
func UnregisterHook(name string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	for i, h := range hooks {
		if h.Name() == name {
			hooks = append(hooks[:i], hooks[i+1:]...)
			return true
		}
	}
	return false
}

// GetHooks returns a snapshot of registered hooks, sorted by
// Priority() then Name() (stable tie-break).
func GetHooks() []Hook {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Hook, len(hooks))
	copy(out, hooks)
	return out
}

// ResetHooks clears the registry. Test-only.
func ResetHooks() {
	registryMu.Lock()
	defer registryMu.Unlock()
	hooks = hooks[:0]
}

// --- Runner ----------------------------------------------------------

// RunBeforeHooks invokes BeforeRequest on every enabled hook in
// Priority() order. Returns the final payload (after all mutations)
// and a flag indicating whether any hook set ShortCircuitStatus.
//
// Fail-open contract: a hook that returns an error or panics has its
// error logged and counted, but does not abort the request. The
// previous payload is used.
func RunBeforeHooks(ctx context.Context, hctx *HookContext) ([]byte, bool) {
	if hctx == nil {
		return nil, false
	}

	all := GetHooks()
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Priority() != all[j].Priority() {
			return all[i].Priority() < all[j].Priority()
		}
		return all[i].Name() < all[j].Name()
	})

	payload := hctx.OptimizedPayload
	if payload == nil {
		payload = hctx.RawPayload
	}

	for _, hk := range all {
		if !hk.IsEnabled(hctx.VK) {
			continue
		}
		start := time.Now()
		newPayload, err := safeBeforeRequest(ctx, hk, hctx)
		latency := time.Since(start)

		if err != nil {
			log.Printf("[hooks] %s BeforeRequest error on vk=%s: %v (continuing with prior payload)",
				hk.Name(), hctx.VK, err)
		}
		if newPayload != nil {
			payload = newPayload
			// Update HookContext so subsequent hooks in the chain
			// see the mutated payload via hctx.OptimizedPayload.
			hctx.OptimizedPayload = newPayload
		}

		RecordHookLatency(hk.Name(), hctx.VK, latency)
		if err != nil {
			RecordHookError(hk.Name(), hctx.VK, "before_error")
		}
		if latency > 100*time.Millisecond {
			log.Printf("[hooks] %s slow BeforeRequest on vk=%s: %v", hk.Name(), hctx.VK, latency)
		}

		if hctx.ShortCircuitStatus != 0 && hctx.ShortCircuitBody != nil {
			return payload, true
		}
	}

	hctx.FinalOptimizedPayload = payload
	return payload, false
}

// RunAfterHooks invokes AfterResponse on every enabled hook in
// Priority() order. Returns the final response body.
//
// Same fail-open contract as RunBeforeHooks.
func RunAfterHooks(ctx context.Context, hctx *HookContext) []byte {
	if hctx == nil {
		return hctx.UpstreamResponse
	}

	all := GetHooks()
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Priority() != all[j].Priority() {
			return all[i].Priority() < all[j].Priority()
		}
		return all[i].Name() < all[j].Name()
	})

	body := hctx.UpstreamResponse
	for _, hk := range all {
		if !hk.IsEnabled(hctx.VK) {
			continue
		}
		start := time.Now()
		newBody, err := safeAfterResponse(ctx, hk, hctx)
		latency := time.Since(start)

		if err != nil {
			log.Printf("[hooks] %s AfterResponse error on vk=%s: %v (continuing with prior body)",
				hk.Name(), hctx.VK, err)
		}
		if newBody != nil {
			body = newBody
			hctx.UpstreamResponse = newBody
		}

		RecordHookLatency(hk.Name(), hctx.VK, latency)
		if err != nil {
			RecordHookError(hk.Name(), hctx.VK, "after_error")
		}
	}
	return body
}

// safeBeforeRequest wraps BeforeRequest with panic recovery.
// Returns the previous hctx.OptimizedPayload on error (fail-open).
func safeBeforeRequest(ctx context.Context, hk Hook, hctx *HookContext) (payload []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hooks] %s BeforeRequest PANIC: %v", hk.Name(), r)
			err = errPanic{val: r}
			payload = nil
			RecordHookError(hk.Name(), hctx.VK, "panic")
		}
	}()
	return hk.BeforeRequest(ctx, hctx)
}

// safeAfterResponse mirrors safeBeforeRequest for the response phase.
func safeAfterResponse(ctx context.Context, hk Hook, hctx *HookContext) (body []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hooks] %s AfterResponse PANIC: %v", hk.Name(), r)
			err = errPanic{val: r}
			body = nil
			RecordHookError(hk.Name(), hctx.VK, "panic")
		}
	}()
	return hk.AfterResponse(ctx, hctx)
}

type errPanic struct{ val interface{} }

func (e errPanic) Error() string {
	return "hook panic recovered"
}
