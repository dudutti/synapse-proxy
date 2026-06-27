// Package optiagent — ToolDedupHook.
//
// Migrated from proxy.go (formerly lines ~252-259 inline block).
//
// Observation-only: when the request has a single file-read tool
// call (or multiple calls to the same file path), the hook:
//  1. Hashes the file path (sha256) → key.
//  2. GETs the canonical key. If a body is stored, this is a
//     dedup hit: increments a hits counter and exposes the hit
//     on HookContext.Features["tool_dedup_hit"] for downstream
//     consumers (logging, telemetry, future payload rewrite).
//  3. Otherwise SETs a placeholder body with a 5-minute TTL.
//
// Behaviour preserved from the legacy inline code:
//   - The hook is observation-only: it never short-circuits or
//     mutates the payload. The proxy's existing inline code
//     only logged dedup hits; the value was never used to
//     rewrite tool-call bodies. Migrating to the hook pipeline
//     preserves that exact behaviour.
//   - Multiple tool calls to DIFFERENT paths → no work done
//     (matches the legacy CheckToolDedup zero-value return).
//   - Non-file tool calls (web_search, bash, ...) are filtered
//     out by ExtractToolCalls before the hook does anything.
//   - Fail open on backend errors.
//
// Priority 140: runs after ToolFilter (130) so a blocked tool
// never wastes a Redis round-trip, and before L1/L2/L3 caching
// (200+).

package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"
)

// toolDedupTTL is the time the placeholder body lives in Redis
// before being forgotten. 5 minutes matches the legacy inline value.
const toolDedupTTL = 5 * time.Minute

// ToolDedupHook observes file-read dedup hits and surfaces them
// to downstream consumers.
type ToolDedupHook struct{}

// Name returns the stable hook identifier.
func (h *ToolDedupHook) Name() string { return "tool_dedup" }

// Priority 140: between ToolFilter (130) and the cache mutators (200+).
func (h *ToolDedupHook) Priority() int { return 140 }

// IsEnabled gates on a non-empty VK. BeforeRequest is the real gate.
func (h *ToolDedupHook) IsEnabled(vk string) bool { return vk != "" }

// BeforeRequest observes file-read dedup hits. See file docstring.
//
// Returns (nil, nil) on the happy path and on backend failure
// (fail-open). The hook never short-circuits — it is purely
// observation.
func (h *ToolDedupHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(h.Name(), hctx.VK)

	payload := currentPayload(hctx)
	if len(payload) == 0 {
		return nil, nil
	}

	// ExtractToolCalls (note: NOT ExtractAllToolCalls) returns
	// ONLY file-read tool calls, filtered through fileReadTools.
	// This matches the legacy inline code's behaviour exactly.
	calls := ExtractToolCalls(payload)
	if len(calls) == 0 {
		return nil, nil
	}

	// Legacy semantics: the inline code returned early when calls
	// targeted multiple DIFFERENT paths. Same here.
	var path string
	for _, c := range calls {
		if c.FilePath == "" {
			return nil, nil
		}
		if path == "" {
			path = c.FilePath
		} else if path != c.FilePath {
			return nil, nil
		}
	}
	if path == "" {
		return nil, nil
	}

	backend := currentSessionCBBackend()
	if backend == nil {
		return nil, nil
	}

	key := buildToolDedupKey(hctx.VK, path)
	existing, err := backend.Get(ctx, key)
	if err != nil {
		log.Printf("[tool-dedup] Get failed for vk=%s path=%s: %v (fail-open)", hctx.VK, path, err)
		return nil, nil
	}
	if len(existing) > 0 {
		// Hot path: a previous request stored this body. Increment
		// the hits counter (best-effort) and surface the hit on
		// hctx.Features so downstream consumers can read it.
		if _, err := backend.Incr(ctx, key+":hits"); err != nil {
			log.Printf("[tool-dedup] Incr failed for vk=%s: %v (non-fatal)", hctx.VK, err)
		}
		hctx.SetFeature("tool_dedup_hit", map[string]interface{}{
			"file_path": path,
			"tool_name": calls[0].ToolName,
			"reused":    true,
		})
		log.Printf("[tool-dedup] HIT: tool=%s file=%s vk=%s (reusing stored body)",
			calls[0].ToolName, path, hctx.VK)
		return nil, nil
	}

	// Cold path: store a placeholder body so a later read can
	// detect the dedup. The placeholder references the file path
	// and tool name so an operator inspecting Redis can see what's
	// being tracked.
	placeholder := []byte(`{"path":"` + path + `","toolName":"` + calls[0].ToolName + `"}`)
	if err := backend.Set(ctx, key, placeholder, toolDedupTTL); err != nil {
		log.Printf("[tool-dedup] Set failed for vk=%s: %v (fail-open)", hctx.VK, err)
	}
	return nil, nil
}

// AfterResponse is a no-op.
func (h *ToolDedupHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// buildToolDedupKey mirrors the legacy inline construction:
//   "synapse:tools:" + vk + ":" + sha256(path)
func buildToolDedupKey(vk, filePath string) string {
	h := sha256.Sum256([]byte(filePath))
	return "synapse:tools:" + vk + ":" + hex.EncodeToString(h[:])
}

func init() {
	RegisterHook(&ToolDedupHook{})
}