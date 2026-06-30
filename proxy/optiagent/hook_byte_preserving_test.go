package optiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestBytePreservingCompressorHook_HermesMultiturn simulates a
// 6-turn Hermes agent conversation and verifies that the hook
// compresses stale <thinking> blocks and oversized tool outputs
// on turns 1-4, while leaving the last 2 turns intact.
func TestBytePreservingCompressorHook_HermesMultiturn(t *testing.T) {
	// Build a realistic Hermes-like payload with thinking blocks
	// in the older assistant turns and 600-byte tool outputs.
	var messages []map[string]interface{}
	messages = append(messages, map[string]interface{}{"role": "system", "content": "You are a coding assistant."})
	for i := 0; i < 4; i++ {
		messages = append(messages,
			map[string]interface{}{"role": "user", "content": fmt.Sprintf("turn %d request", i)},
			map[string]interface{}{"role": "assistant", "content": "<thinking>internal scratch thought " + strings.Repeat("a", 100) + "</thinking>answer for turn " + fmt.Sprint(i)},
			map[string]interface{}{"role": "tool", "tool_call_id": fmt.Sprintf("t%d", i), "name": "execute_code", "content": "a" + strings.Repeat("X", 600)},
		)
	}
	messages = append(messages,
		map[string]interface{}{"role": "assistant", "content": "<thinking>recent thought</thinking>latest answer"},
		map[string]interface{}{"role": "user", "content": "final question"},
	)
	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o-mini",
		"messages": messages,
		"system":   "",
	})
	fmt.Fprintf(os.Stderr, "Test payload: %d bytes\n", len(body))

	hctx := &HookContext{
		VK:               "test-vk",
		Provider:         "minimax",
		Model:            "gpt-4o-mini",
		OptimizedPayload: body,
		Features:         map[string]interface{}{},
	}

	hook := &BytePreservingCompressorHook{}
	out, err := hook.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if out == nil {
		t.Fatal("hook returned nil (no compression happened)")
	}
	fmt.Fprintf(os.Stderr, "Compressed: %d bytes (saved %d = %.1f%%)\n",
		len(out), len(body)-len(out),
		100.0*float64(len(body)-len(out))/float64(len(body)))

	if len(out) >= len(body) {
		t.Errorf("expected output shorter than input; got %d >= %d", len(out), len(body))
	}

	// Verify the output is still valid JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nbody: %s", err, out)
	}

	// Verify the latest assistant <thinking> block is preserved
	// (it was on a recent turn and should NOT have been stripped).
	if !bytes.Contains(out, []byte("recent thought")) {
		t.Errorf("expected recent <thinking> content preserved, got: %.500s", out)
	}
	if !bytes.Contains(out, []byte("[Pruned Thought Process]")) {
		t.Errorf("expected older <thinking> blocks stripped to [Pruned Thought Process], got: %.500s", out)
	}

	// Verify the tool output truncation marker is present (it
	// was on a non-recent tool message and should have been
	// truncated).
	if !bytes.Contains(out, []byte("…truncated by Synapse L3…")) {
		t.Errorf("expected tool output truncation marker, got: %.500s", out)
	}

	// Verify the per-hook savings feature is recorded.
	if saved, ok := hctx.Feature("byte_preserving_compressor_bytes_saved"); !ok || saved.(int) <= 0 {
		t.Errorf("expected bytes_saved feature > 0, got: %v", saved)
	}
}