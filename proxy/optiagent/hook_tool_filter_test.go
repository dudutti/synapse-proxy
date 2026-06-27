// Tests for ToolFilterHook — extracted from proxy.go inline block
// (formerly lines ~244-278) into the hook pipeline.
//
// Behaviour preserved:
//   - Reads block_unknown_tools from Features["block_unknown_tools"]
//   - Reads allowed_tools from Features["allowed_tools"] (CSV string)
//   - When the request has tool calls:
//       1. If block_unknown_tools is OFF, consult the per-VK
//          denylist (Redis SET synapse:denied_tools:<vk>). Any
//          tool in the set triggers a 403 + a JSON error.
//       2. If block_unknown_tools is ON AND allowed_tools is set,
//          consult the allowlist. Any tool NOT in the list
//          triggers a 400 + a JSON error.
//   - Empty allowed_tools + block_unknown_tools=true ⇒ no filtering
//     (matches legacy "opt-in" semantics — strict mode is opt-in).
//   - Returns (nil, nil) when there are no tool calls (no work to do).
//   - Fails open on backend errors.

package optiagent

import (
	"net/http"
	"strings"
	"testing"
)

// TestToolFilterHook_NameAndPriority verifies the stable identity.
func TestToolFilterHook_NameAndPriority(t *testing.T) {
	h := &ToolFilterHook{}
	if h.Name() != "tool_filter" {
		t.Fatalf("expected name 'tool_filter', got %q", h.Name())
	}
	// Priority 130 = runs after fingerprint (100) + circuit breaker (110),
	// before payload-mutating hooks (200+). A blocked tool should
	// short-circuit BEFORE we spend cycles on L3 compression.
	if h.Priority() != 130 {
		t.Fatalf("expected priority 130, got %d", h.Priority())
	}
}

// TestToolFilterHook_NoToolCallsPassThrough verifies that a request
// without any tool_calls never triggers the firewall — the hook is a
// no-op for plain chat traffic.
func TestToolFilterHook_NoToolCallsPassThrough(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)() // reuse the test helper for the denylist backend (nil here)

	hctx := &HookContext{
		VK:              "vk-tf-plain",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": true,
			"allowed_tools":       "read_file",
		},
	}
	h := &ToolFilterHook{}
	payload, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload (no mutation), got %q", payload)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit on no-tool payload, got status=%d",
			hctx.ShortCircuitStatus)
	}
}

// TestToolFilterHook_AllowlistAllowsKnownTool verifies the happy path
// of the strict mode: a tool that IS in the allowlist passes through.
func TestToolFilterHook_AllowlistAllowsKnownTool(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-allow-ok",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": true,
			"allowed_tools":       "read_file, ls, web_search",
		},
	}
	h := &ToolFilterHook{}
	payload, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload, got %q", payload)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit for allowed tool, got status=%d body=%q",
			hctx.ShortCircuitStatus, hctx.ShortCircuitBody)
	}
}

// TestToolFilterHook_AllowlistBlocksUnknownTool is the SECURITY-CRITICAL
// test: an unknown tool under strict mode MUST trigger a 400 with a
// clear error. This is the firewall's primary line of defence against
// an agent that tries to call a tool the operator never authorised.
func TestToolFilterHook_AllowlistBlocksUnknownTool(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-allow-block",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"rm_rf","arguments":"{\"path\":\"/\"}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": true,
			"allowed_tools":       "read_file, ls, web_search",
		},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hctx.ShortCircuitStatus != http.StatusBadRequest {
		t.Fatalf("expected 400 short-circuit, got %d", hctx.ShortCircuitStatus)
	}
	body := string(hctx.ShortCircuitBody)
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("expected OpenAI-compatible error JSON, got %q", body)
	}
	if !strings.Contains(body, "rm_rf") {
		t.Fatalf("expected blocked tool name in message, got %q", body)
	}
	if !strings.Contains(body, "unauthorized") {
		t.Fatalf("expected 'unauthorized' in message, got %q", body)
	}
}

// TestToolFilterHook_AllowlistEmptyDoesNotBlock verifies the legacy
// opt-in semantics: strict mode with no allowlist configured does
// NOT block anything (an empty allowlist is treated as "feature
// disabled", not as "deny all").
func TestToolFilterHook_AllowlistEmptyDoesNotBlock(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-allow-empty",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"anything","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": true,
			"allowed_tools":       "",
		},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("empty allowlist must not block, got status=%d", hctx.ShortCircuitStatus)
	}
}

// TestToolFilterHook_PermissiveModeDoesNotCheckAllowlist verifies that
// when block_unknown_tools is false, the allowlist is ignored (legacy
// behaviour — the strict mode is opt-in via the feature flag).
func TestToolFilterHook_PermissiveModeDoesNotCheckAllowlist(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-permissive",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"unauthorised_tool","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": false,
			"allowed_tools":       "read_file",
		},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("permissive mode must not block, got status=%d", hctx.ShortCircuitStatus)
	}
}

// TestToolFilterHook_DenylistBlocksConfiguredTool is the SECOND
// SECURITY-CRITICAL test: under permissive mode (no strict
// allowlist), the operator-curated denylist MUST still trigger
// a 403. This is the case where a tool was discovered and the
// operator unchecked it in the dashboard.
func TestToolFilterHook_DenylistBlocksConfiguredTool(t *testing.T) {
	stub := newStubRedis()
	stub.setDenyList("vk-tf-deny-hit", []string{"dangerous_tool"})
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-deny-hit",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"dangerous_tool","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": false,
			"allowed_tools":       "",
		},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hctx.ShortCircuitStatus != http.StatusForbidden {
		t.Fatalf("expected 403 short-circuit, got %d", hctx.ShortCircuitStatus)
	}
	body := string(hctx.ShortCircuitBody)
	if !strings.Contains(body, "dangerous_tool") {
		t.Fatalf("expected blocked tool name in message, got %q", body)
	}
	if !strings.Contains(body, "denied") {
		t.Fatalf("expected 'denied' in message, got %q", body)
	}
}

// TestToolFilterHook_DenylistAllowsNonListedTool verifies the
// negative case: a tool that is NOT in the denylist is not blocked.
func TestToolFilterHook_DenylistAllowsNonListedTool(t *testing.T) {
	stub := newStubRedis()
	stub.setDenyList("vk-tf-deny-miss", []string{"dangerous_tool"})
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-deny-miss",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"safe_tool","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("non-listed tool must not be blocked, got status=%d", hctx.ShortCircuitStatus)
	}
}

// TestToolFilterHook_DenylistFailsOpenOnBackendError verifies the
// fail-open contract: if the Redis denylist lookup errors out, the
// request still proceeds (we'd rather let a tool through than break
// the agent on a transient Redis blip).
func TestToolFilterHook_DenylistFailsOpenOnBackendError(t *testing.T) {
	stub := newStubRedis()
	stub.denyError = errRedisDown
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-deny-down",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"any_tool","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error (fail-open), got %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit on backend failure, got %d", hctx.ShortCircuitStatus)
	}
}

// TestToolFilterHook_AllowlistShadowesDenylistInStrictMode documents
// the legacy precedence rule: in strict mode (block_unknown_tools=true),
// the denylist is SKIPPED and the allowlist is the sole gate. This is
// the inverse of "denylist wins" — the legacy inline code consulted
// the denylist only when BlockUnknownTools was false.
//
// A test that wants "denylist wins" semantics would need a code
// change upstream: swap the order in proxy.go and update the
// comment that explains the legacy behaviour. We do not change the
// behaviour here because that would be a semantic break for any
// existing operator who relies on "strict mode = allowlist only".
func TestToolFilterHook_AllowlistShadowesDenylistInStrictMode(t *testing.T) {
	stub := newStubRedis()
	stub.setDenyList("vk-tf-deny-over-allow", []string{"compromised"})
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-deny-over-allow",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"compromised","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": true,
			"allowed_tools":       "compromised, read_file", // compromised is in allowlist
		},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// In strict mode the denylist is skipped, the allowlist is the
	// sole gate, "compromised" IS in the allowlist → no block.
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit (allowlist covers the tool), got status=%d body=%q",
			hctx.ShortCircuitStatus, hctx.ShortCircuitBody)
	}
}

// TestToolFilterHook_AllowlistWithSpacesAndTrimmedNames verifies the
// allowlist parses a CSV with surrounding whitespace correctly.
func TestToolFilterHook_AllowlistWithSpacesAndTrimmedNames(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-tf-allow-spaces",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features: map[string]interface{}{
			"block_unknown_tools": true,
			"allowed_tools":       " read_file , ls , web_search ",
		},
	}
	h := &ToolFilterHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("whitespace-padded allowlist must still allow, got status=%d",
			hctx.ShortCircuitStatus)
	}
}

// TestToolFilterHook_AfterResponseNoOp documents that the hook acts
// only on the request side.
func TestToolFilterHook_AfterResponseNoOp(t *testing.T) {
	h := &ToolFilterHook{}
	hctx := &HookContext{
		VK:              "vk-tf-after",
		OptimizedPayload: []byte(`{"messages":[]}`),
		UpstreamResponse: []byte(`{"ok":true}`),
		ResponseHeaders: http.Header{},
	}
	body, err := h.AfterResponse(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body, got %q", body)
	}
}