// Package optiagent — BytePreservingCompressorHook.
//
// Wires the existing CompressPayload() (Unmarshal + agent-aware
// rules + re-Marshal) into the BeforeRequest hook chain so that
// long multi-turn agent trajectories actually see their stale
// <thinking> blocks, oversized tool outputs, and repeated
// same-name tool results compressed.
//
// CRITICAL: priority 100 — BEFORE CCR Compress (740) and CCR
// Retrieve (750). This is the key fix: the byte-preserving hook
// must run first so the compressed payload is the one we hash
// for L1/L2 cache lookups. If it runs AFTER CCR Retrieve, the
// retrieve hook short-circuits with the un-compressed hash, and
// the dashboard's L3 row shows Orig==Opt with $0 saved.
package optiagent

import (
	"context"
	"log"
)

// BytePreservingCompressorHook is the integration point that
// actually wires the L3 rules (thinking-block stripping, tool-
// output truncation, repeated tool result blanking, todo-list
// carve-out) into the proxy hot path.
type BytePreservingCompressorHook struct{}

// Name returns the hook name used in metrics and log lines.
func (h *BytePreservingCompressorHook) Name() string { return "byte_preserving_compressor" }

// Priority is 100 — before CCR Compress (740) and CCR Retrieve
// (750). The byte-preserving compressor must run first so the
// L1 cache key is computed on the compressed payload.
func (h *BytePreservingCompressorHook) Priority() int { return 100 }

// IsEnabled returns true. The hook is always-on; it falls
// through to the original payload on parse error.
func (h *BytePreservingCompressorHook) IsEnabled(vk string) bool { return true }

// BeforeRequest calls CompressPayload on the current
// optimized payload and records the savings. On any parse
// error we return (nil, nil) so the next hook in the chain
// sees the unchanged bytes.
func (h *BytePreservingCompressorHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx == nil || len(hctx.OptimizedPayload) == 0 {
		return nil, nil
	}

	original := hctx.OptimizedPayload

	compressed, err := CompressPayload(original)
	if err != nil {
		log.Printf("[hooks] %s: CompressPayload error on vk=%s: %v (continuing with original payload)",
			h.Name(), hctx.VK, err)
		return nil, nil
	}
	if compressed == nil || len(compressed) == 0 || len(compressed) >= len(original) {
		// No compression possible (already canonical, no stale
		// tool outputs, no repeated tool results to drop, etc.).
		return nil, nil
	}

	saved := len(original) - len(compressed)
	hctx.SetFeature("byte_preserving_compressor_bytes_saved", saved)
	hctx.SetFeature("byte_preserving_compressor_orig_bytes", len(original))
	hctx.SetFeature("byte_preserving_compressor_opt_bytes", len(compressed))
	log.Printf("[hooks] %s: vk=%s compressed %d -> %d bytes (saved %d = %.1f%%)",
		h.Name(), hctx.VK, len(original), len(compressed),
		saved, 100.0*float64(saved)/float64(len(original)))
	return compressed, nil
}

// AfterResponse is a no-op. Compression is request-side only;
// the response shape is converted by AnthropicToOpenAI inside
// stream.go, not here.
func (h *BytePreservingCompressorHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

func init() {
	RegisterHook(&BytePreservingCompressorHook{})
	log.Printf("[hooks] registered BytePreservingCompressorHook at priority 100 (before CCR Compress and Retrieve)")
}