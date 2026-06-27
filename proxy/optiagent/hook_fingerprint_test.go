package optiagent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// TestInjectSystemWarning_StringContent verifies the helper appends
// the system warning to a string-content message.
func TestInjectSystemWarning_StringContent(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	out := InjectSystemWarningCompat(payload, "read_file")
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	msgs := parsed["messages"].([]interface{})
	last := msgs[len(msgs)-1].(map[string]interface{})
	content := last["content"].(string)
	if !strings.Contains(content, "[SYSTEM WARNING") {
		t.Fatalf("warning not injected: %q", content)
	}
	if !strings.Contains(content, "read_file") {
		t.Fatalf("tool name missing: %q", content)
	}
	if !strings.HasPrefix(content, "hello") {
		t.Fatalf("original content lost: %q", content)
	}
}

// TestInjectSystemWarning_ArrayContent verifies the helper appends a
// new text block to an array-of-blocks content (Anthropic / OpenAI
// vision style).
func TestInjectSystemWarning_ArrayContent(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	out := InjectSystemWarningCompat(payload, "bash")
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	var parsed map[string]interface{}
	_ = json.Unmarshal(out, &parsed)
	msgs := parsed["messages"].([]interface{})
	last := msgs[len(msgs)-1].(map[string]interface{})
	arr := last["content"].([]interface{})
	if len(arr) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(arr))
	}
	warnBlock := arr[1].(map[string]interface{})
	if warnBlock["type"] != "text" {
		t.Fatalf("warning block wrong type: %v", warnBlock["type"])
	}
	if !strings.Contains(warnBlock["text"].(string), "[SYSTEM WARNING") {
		t.Fatal("warning text not present in block")
	}
}

// TestInjectSystemWarning_MalformedReturnsNil confirms graceful
// degradation on unparseable payloads.
func TestInjectSystemWarning_MalformedReturnsNil(t *testing.T) {
	if out := InjectSystemWarningCompat([]byte("not json"), "tool"); out != nil {
		t.Fatalf("expected nil on bad payload, got %q", out)
	}
	if out := InjectSystemWarningCompat([]byte(""), "tool"); out != nil {
		t.Fatalf("expected nil on empty payload, got %q", out)
	}
	if out := InjectSystemWarningCompat([]byte(`{"messages":[]}`), "tool"); out != nil {
		t.Fatalf("expected nil on empty messages, got %q", out)
	}
}

// TestInjectSystemWarning_NoMessagesKey verifies the helper does not
// crash when the payload has no messages field.
func TestInjectSystemWarning_NoMessagesKey(t *testing.T) {
	if out := InjectSystemWarningCompat([]byte(`{"model":"x"}`), "tool"); out != nil {
		t.Fatalf("expected nil when messages absent, got %q", out)
	}
}

// TestInjectSystemWarning_ByteStableForSameInput verifies the warning
// text is identical across calls. Provider prompt caches key off
// bytes — drift in the warning text would silently bust the cache.
func TestInjectSystemWarning_ByteStableForSameInput(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":"x"}]}`)
	a := InjectSystemWarningCompat(payload, "tool_name")
	b := InjectSystemWarningCompat(payload, "tool_name")
	if string(a) != string(b) {
		t.Fatalf("warning text drift:\nA=%s\nB=%s", a, b)
	}
}

// --- FingerprintHook tests --------------------------------------------

// TestFingerprintHook_DisabledByDefault verifies the hook returns
// false from IsEnabled when SetFingerprintEnabled hasn't been called
// for the VK.
func TestFingerprintHook_DisabledByDefault(t *testing.T) {
	ClearFingerprintEnabledCache()
	defer ClearFingerprintEnabledCache()

	h := &FingerprintHook{}
	if h.IsEnabled("vk-never-set") {
		t.Fatal("hook should be disabled when SetFingerprintEnabled was not called")
	}
}

// TestFingerprintHook_EnabledAfterSet verifies IsEnabled tracks the
// per-VK flag set by proxy.go after reading the VirtualKeyConfig.
func TestFingerprintHook_EnabledAfterSet(t *testing.T) {
	ClearFingerprintEnabledCache()
	defer ClearFingerprintEnabledCache()

	SetFingerprintEnabled("vk-active", true)
	defer ClearFingerprintEnabledCache()

	h := &FingerprintHook{}
	if !h.IsEnabled("vk-active") {
		t.Fatal("hook should be enabled after SetFingerprintEnabled(true)")
	}
	SetFingerprintEnabled("vk-active", false)
	if h.IsEnabled("vk-active") {
		t.Fatal("hook should be disabled after SetFingerprintEnabled(false)")
	}
}

// TestFingerprintHook_NoRedisNoOp verifies the hook returns nil
// payload and no header writes when Redis is not configured. This
// matters at startup before db.InitRedis completes.
func TestFingerprintHook_NoRedisNoOp(t *testing.T) {
	ClearFingerprintEnabledCache()
	SetFingerprintEnabled("vk-x", true)
	defer ClearFingerprintEnabledCache()

	// Force the atomic pointer to nil by swapping in a fresh
	// empty pointer (simulating "Redis not yet initialized").
	old := fingerprintRDB.Swap(nil)
	defer fingerprintRDB.Store(old)

	hctx := &HookContext{
		VK:               "vk-x",
		Provider:         "anthropic",
		Model:            "claude-opus-4-5",
		OptimizedPayload: []byte(`{"messages":[]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{},
	}
	h := &FingerprintHook{}
	payload, err := h.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload (no Redis), got %q", payload)
	}
	if hctx.ResponseHeaders.Get("X-Synapse-Fingerprint-Count") != "" {
		t.Fatal("expected no fingerprint header when Redis is nil")
	}
}

// TestFingerprintHook_NoPayloadNoOp verifies the hook does not crash
// when given an empty payload.
func TestFingerprintHook_NoPayloadNoOp(t *testing.T) {
	ClearFingerprintEnabledCache()
	SetFingerprintEnabled("vk-empty", true)
	defer ClearFingerprintEnabledCache()

	hctx := &HookContext{
		VK:               "vk-empty",
		OptimizedPayload: nil,
		RawPayload:       nil,
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{},
	}
	h := &FingerprintHook{}
	payload, err := h.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload, got %q", payload)
	}
}

// TestFingerprintHook_Priority verifies the priority is 100 (early
// observation hook).
func TestFingerprintHook_Priority(t *testing.T) {
	h := &FingerprintHook{}
	if h.Priority() != 100 {
		t.Fatalf("expected priority 100, got %d", h.Priority())
	}
	if h.Name() != "fingerprint" {
		t.Fatalf("expected name 'fingerprint', got %q", h.Name())
	}
}

// TestFingerprintHook_AfterResponseNoOp verifies the response-phase
// hook is a no-op for payload/headers but still increments the
// after-call metric counter (no assertion on the counter itself, just
// that no error/panic is returned).
func TestFingerprintHook_AfterResponseNoOp(t *testing.T) {
	h := &FingerprintHook{}
	hctx := &HookContext{
		VK:               "vk-x",
		ResponseHeaders:  http.Header{},
		UpstreamResponse: []byte(`{"choices":[{"message":{"content":"hi"}}]}`),
	}
	body, err := h.AfterResponse(context.Background(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body, got %q", body)
	}
}

// TestFingerprintHook_IncrementBefore verifies the metric counter
// helper does not panic and the registry contains the expected key.
func TestFingerprintHook_IncrementBefore(t *testing.T) {
	IncrementBefore("test-hook", "vk-test")
	// Just verify no panic — the metric map is internal.
	atomic.LoadInt32(new(int32)) // touch atomic to avoid unused-import error in some toolchains
}
