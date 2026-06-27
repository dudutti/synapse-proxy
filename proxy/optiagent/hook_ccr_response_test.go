// Tests for CCR Response Handler.
//
// P1.2 closes the CCR loop. The P1.1 step injected the
// `synapse_retrieve` tool into the payload. When the
// LLM calls it, we intercept the response, look up
// the original in the CompressionStore, and return
// it. This is what makes the tool actually useful.
//
// The handler does NOT make a new upstream call. It
// short-circuits the response with the original.

package optiagent

import (
	"context"
	"encoding/json"
	"testing"
)

// buildToolCallResponse simulates what the LLM sends
// back when it wants to invoke synapse_retrieve.
func buildToolCallResponse(toolName, cacheKey string) []byte {
	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"finish_reason": "tool_calls",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]interface{}{
						{
							"id":   "call_1",
							"type": "function",
							"function": map[string]interface{}{
								"name":      toolName,
								"arguments": `{"cache_key":"` + cacheKey + `"}`,
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestResponseHandler_RetrievesOriginal(t *testing.T) {
	store := newInMemoryCompressionStore()
	store.Reset()
	original := []byte(`{"messages":[{"role":"tool","content":"the original log dump"}]}`)
	key := cacheKeyFor(original)
	store.Save(key, original)

	handler := &CCRResponseHandler{store: store}
	hctx := &HookContext{
		VK:         "vk-test",
		RawPayload: []byte(`{"messages":[{"role":"user","content":"what about X"}]}`),
	}
	upstreamResp := buildToolCallResponse(SynapseRetrieveToolName, key)

	out, err := handler.AfterResponse(context.Background(), hctx, upstreamResp)
	if err != nil {
		t.Fatalf("AfterResponse error: %v", err)
	}
	// The output should NOT contain the tool_call anymore.
	// It should contain the original content as the
	// tool response.
	if !json.Valid(out) {
		t.Fatalf("invalid JSON output: %s", out)
	}
	// Parse and verify the original content is reachable.
	var parsed map[string]interface{}
	json.Unmarshal(out, &parsed)
	choices := parsed["choices"].([]interface{})
	first := choices[0].(map[string]interface{})
	msg := first["message"].(map[string]interface{})
	if msg["role"] != "tool" {
		t.Errorf("expected role=tool, got %v", msg["role"])
	}
	contentMap, ok := msg["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected content as map, got %T: %v", msg["content"], msg["content"])
	}
	originalField, ok := contentMap["original"].(string)
	if !ok || !contains(originalField, "the original log dump") {
		t.Errorf("expected original content, got %q", originalField)
	}
}

func TestResponseHandler_NotOurToolPassesThrough(t *testing.T) {
	store := newInMemoryCompressionStore()
	handler := &CCRResponseHandler{store: store}
	hctx := &HookContext{VK: "vk"}
	upstreamResp := buildToolCallResponse("some_other_tool", "abc123")
	out, err := handler.AfterResponse(context.Background(), hctx, upstreamResp)
	if err != nil {
		t.Fatalf("AfterResponse error: %v", err)
	}
	var a, b map[string]interface{}
	json.Unmarshal(upstreamResp, &a)
	json.Unmarshal(out, &b)
	if len(a) != len(b) {
		t.Errorf("expected pass-through, len mismatch: %d vs %d", len(a), len(b))
	}
}

func TestResponseHandler_MissingCacheKey(t *testing.T) {
	store := newInMemoryCompressionStore()
	handler := &CCRResponseHandler{store: store}
	hctx := &HookContext{VK: "vk"}
	upstreamResp := buildToolCallResponse(SynapseRetrieveToolName, "missing_key")
	out, err := handler.AfterResponse(context.Background(), hctx, upstreamResp)
	if err != nil {
		t.Fatalf("AfterResponse error: %v", err)
	}
	// When the cache_key is missing, return an error
	// message as the tool response (so the LLM knows
	// the original is gone).
	if !json.Valid(out) {
		t.Fatalf("invalid JSON: %s", out)
	}
}

func TestResponseHandler_EmptyUpstreamResponse(t *testing.T) {
	store := newInMemoryCompressionStore()
	handler := &CCRResponseHandler{store: store}
	hctx := &HookContext{VK: "vk"}
	out, err := handler.AfterResponse(context.Background(), hctx, []byte{})
	if err != nil {
		t.Fatalf("AfterResponse error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output for empty input, got %s", out)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}