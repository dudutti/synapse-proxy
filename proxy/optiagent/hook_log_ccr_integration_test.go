// Tests for the CCR integration in LogCompressor. When
// the LogCompressor compresses a log dump, it should
// also store the original in a CompressionStore under
// a stable cache_key, so the LLM can later request the
// full version via the headroom_retrieve tool (P0.6).
//
// The CompressionStore is a thin abstraction: a key
// derived from the canonical hash of the content, and
// the original bytes. Phase 1 (this test) uses an
// in-memory implementation. Phase 2 (P0.6) will use
// Redis as the backend.

package optiagent

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestCCRStoreCompressionStoreSavesOriginal: when
// LogCompressor compresses a log dump, the original
// is saved in the CompressionStore under a cache_key
// derived from the canonical hash.
func TestCCRStoreCompressionStoreSavesOriginal(t *testing.T) {
	h := &LogCompressorHook{}
	store := newInMemoryCompressionStore()
	h.SetCompressionStore(store)
	// Reset between tests.
	store.Reset()

	original := strings.Join([]string{
		"INFO: starting up",
		"INFO: connecting to db",
		"INFO: ready",
		"DEBUG: heartbeat 1",
		"DEBUG: heartbeat 2",
		"DEBUG: heartbeat 3",
		"DEBUG: heartbeat 4",
		"DEBUG: heartbeat 5",
		"DEBUG: heartbeat 6",
		"DEBUG: heartbeat 7",
		"DEBUG: heartbeat 8",
		"DEBUG: heartbeat 9",
		"DEBUG: heartbeat 10",
		"DEBUG: heartbeat 11",
		"DEBUG: heartbeat 12",
		"DEBUG: heartbeat 13",
		"DEBUG: heartbeat 14",
		"DEBUG: heartbeat 15",
		"DEBUG: heartbeat 16",
		"DEBUG: heartbeat 17",
		"DEBUG: heartbeat 18",
		"DEBUG: heartbeat 19",
		"DEBUG: heartbeat 20",
		"INFO: done",
	}, "\n")
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.ReplaceAll(original, "\n", "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-ccr-store", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	// The output must be shorter than the input.
	if len(out) >= len(body) {
		t.Fatalf("output not compressed\n  in:  %d\n  out: %d", len(body), len(out))
	}
	// The store must have at least one entry.
	if store.Count() == 0 {
		t.Fatalf("no entry was saved in the CompressionStore")
	}
	// The saved entry must contain the original.
	// We match against the raw bytes (the payload
	// is stored as-is).
	found := false
	originalRaw := []byte(original)
	for _, entry := range store.Entries() {
		// Try matching the raw original (no escape).
		if bytes.Contains(entry.Value, originalRaw) {
			found = true
			break
		}
		// Try matching with JSON-style escape.
		originalEscaped := []byte(strings.ReplaceAll(original, "\n", `\`+`n`))
		if bytes.Contains(entry.Value, originalEscaped) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("original content was not saved verbatim in the store")
	}
	// The cache_key must be non-empty and 16+ chars
	// (hex hash).
	for _, entry := range store.Entries() {
		if len(entry.Key) < 16 {
			t.Errorf("cache_key too short: %q", entry.Key)
		}
	}
	// The save must be reported in the hook features.
	savedCount, ok := hctx.GetFeature("ccr_compression_store_saved").(int)
	if !ok {
		t.Fatalf("expected ccr_compression_store_saved feature to be set")
	}
	if savedCount == 0 {
		t.Fatalf("expected saved count > 0, got %d", savedCount)
	}
	t.Logf("compressed: in=%d out=%d saved_count=%d store_count=%d",
		len(body), len(out), savedCount, store.Count())
}

// TestCCRStoreCompressionStoreNoSaveForNoOp: when
// LogCompressor does not compress (small payload,
// plain text), no entry should be saved.
func TestCCRStoreCompressionStoreNoSaveForNoOp(t *testing.T) {
	h := &LogCompressorHook{}
	store := newInMemoryCompressionStore()
	store.Reset()
	h.SetCompressionStore(store)

	original := "Just plain prose, nothing to compress."
	body := []byte(`{"messages":[{"role":"tool","content":"` + original + `"}]}`)
	hctx := &HookContext{
		VK: "vk-ccr-noop", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	// The output must be unchanged (no compression).
	if string(out) != string(body) {
		t.Fatalf("output was modified for non-compressible payload")
	}
	// The store must be empty.
	if store.Count() != 0 {
		t.Fatalf("expected store to be empty, got %d entries", store.Count())
	}
}
