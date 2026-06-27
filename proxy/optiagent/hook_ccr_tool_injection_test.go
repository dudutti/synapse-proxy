// Tests for the CCR tool injection. When the proxy
// compresses content (L3, CCR Compress, LogCompressor,
// etc.), the original is stored in the CompressionStore
// under a cache_key. P1.1 adds a `synapse_retrieve` tool
// to the request's tools[] array, so the LLM can call
// it to get the original back.
//
// This is the first half of the Headroom CCR tool
// injection pattern. The second half (P1.2) is the
// response handler that intercepts the LLM's tool call
// and returns the original.

package optiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestCCRToolInjection_AddsToolWhenCompressionOccurred:
// when a CompressionStore entry exists for this
// payload (e.g. the LogCompressor hook stored it), the
// CCRToolInjectionHook must add a "synapse_retrieve"
// tool to the payload's tools[] array.
func TestCCRToolInjection_AddsToolWhenCompressionOccurred(t *testing.T) {
	h := &CCRToolInjectionHook{}
	// Seed the CompressionStore with an entry.
	store := GetGlobalCompressionStore()
	store.Reset()
	original := []byte(`{"messages":[{"role":"user","content":"a very long log that will be compressed"}]}`)
	key := cacheKeyFor(original)
	store.Save(key, original)

	body := []byte(`{"model":"minimax","messages":[{"role":"user","content":"summarize this"}]}`)
	hctx := &HookContext{
		VK: "vk-ccr-tool", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	// Mark that compression happened by setting a feature.
	hctx.SetFeature("ccr_cache_key", key)
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)

	// Parse the output as JSON and check that tools[] contains
	// a "synapse_retrieve" tool.
	var parsed map[string]interface{}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n  out: %s", err, outStr)
	}
	tools, ok := parsed["tools"].([]interface{})
	if !ok {
		t.Fatalf("output has no tools[] array\n  out: %s", outStr)
	}
	found := false
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		if toolMap["type"] == "function" {
			fn, ok := toolMap["function"].(map[string]interface{})
			if !ok {
				continue
			}
			if fn["name"] == "synapse_retrieve" {
				found = true
				// The description should mention the cache_key.
				desc, _ := fn["description"].(string)
				if !strings.Contains(desc, "compressed content") {
					t.Errorf("tool description should mention 'compressed content', got: %q", desc)
				}
				break
			}
		}
	}
	if !found {
		t.Fatalf("synapse_retrieve tool not found in tools[]\n  out: %s", outStr)
	}
}

// TestCCRToolInjection_NoToolWhenNoCompression: when
// no compression has happened (no ccr_cache_key
// feature), the hook must NOT add the tool. Adding it
// would be a false positive: the LLM would call the
// tool and get nothing useful back.
func TestCCRToolInjection_NoToolWhenNoCompression(t *testing.T) {
	h := &CCRToolInjectionHook{}
	body := []byte(`{"model":"minimax","messages":[{"role":"user","content":"hello"}]}`)
	hctx := &HookContext{
		VK: "vk-ccr-noop", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	// No ccr_cache_key feature set.
	out, _ := h.BeforeRequest(context.Background(), hctx)
	// The output must be unchanged.
	if !bytes.Equal(out, body) {
		t.Fatalf("output was modified for non-compressed payload\n  got:  %s\n  want: %s", string(out), string(body))
	}
}

// TestCCRToolInjection_NoToolWhenToolsAlreadyPresent:
// when the user has already defined tools in the
// request, the hook must APPEND (not replace) the
// synapse_retrieve tool. This is important: the
// user's tools (code interpreter, web search, etc.)
// must not be lost.
func TestCCRToolInjection_NoToolWhenToolsAlreadyPresent(t *testing.T) {
	h := &CCRToolInjectionHook{}
	// Seed CompressionStore.
	store := GetGlobalCompressionStore()
	store.Reset()
	original := []byte(`{"messages":[{"role":"user","content":"some compressed content"}]}`)
	key := cacheKeyFor(original)
	store.Save(key, original)

	// User-defined tools include a code interpreter.
	body := []byte(`{"model":"minimax","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"code_interpreter"}}]}`)
	hctx := &HookContext{
		VK: "vk-ccr-merge", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	hctx.SetFeature("ccr_cache_key", key)
	out, _ := h.BeforeRequest(context.Background(), hctx)
	var parsed map[string]interface{}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	tools, _ := parsed["tools"].([]interface{})
	// The user tool must still be present.
	userToolFound := false
	ccrToolFound := false
	for _, t := range tools {
		fn, _ := t.(map[string]interface{})["function"].(map[string]interface{})
		switch fn["name"] {
		case "code_interpreter":
			userToolFound = true
		case "synapse_retrieve":
			ccrToolFound = true
		}
	}
	if !userToolFound {
		t.Fatalf("user tool 'code_interpreter' was lost\n  out: %s", string(out))
	}
	if !ccrToolFound {
		t.Fatalf("CCR tool 'synapse_retrieve' not added\n  out: %s", string(out))
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

// TestCCRToolInjection_HandlesMalformedJSON: when the
// payload is not valid JSON, the hook must not crash.
// It must return the payload unchanged. This is a
// defensive guarantee: the hook is at the end of the
// pipeline and must never break a request.
func TestCCRToolInjection_HandlesMalformedJSON(t *testing.T) {
	h := &CCRToolInjectionHook{}
	body := []byte(`{not valid json`)
	hctx := &HookContext{
		VK: "vk-ccr-bad", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	hctx.SetFeature("ccr_cache_key", "deadbeef")
	out, _ := h.BeforeRequest(context.Background(), hctx)
	// The output must be unchanged (no crash, no
	// modification).
	if string(out) != string(body) {
		t.Fatalf("malformed payload was modified\n  got:  %s\n  want: %s", string(out), string(body))
	}
}
