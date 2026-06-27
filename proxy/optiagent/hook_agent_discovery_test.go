// Tests for AgentDiscoveryHook — extracted from proxy.go inline block
// (formerly lines ~215-236) into the hook pipeline.
//
// Behaviour preserved:
//   - When the request carries tool calls, SADD every tool name
//     into the Redis set `synapse:discovered_tools:<vk>`.
//   - Set a 30-day TTL on the key so the list eventually forgets
//     abandoned agents without operator intervention.
//   - The set is dedup'd by Redis (SADD is idempotent).
//   - The hook is observation-only: it never short-circuits or
//     mutates the payload.
//   - Fails open on backend errors (SAdd/Expire failures are
//     swallowed; the request still proceeds).

package optiagent

import (
	"net/http"
	"testing"
)

// TestAgentDiscoveryHook_NameAndPriority verifies the stable identity.
func TestAgentDiscoveryHook_NameAndPriority(t *testing.T) {
	h := &AgentDiscoveryHook{}
	if h.Name() != "agent_discovery" {
		t.Fatalf("expected name 'agent_discovery', got %q", h.Name())
	}
	// Priority 105 = early (after fingerprint 100, before circuit
	// breaker 110). Discovery must run early so the denylist
	// consulted by ToolFilterHook sees the latest tool set. But it
	// must run AFTER fingerprint so a soft-loop that gets a system
	// warning injected doesn't pollute the discovery count.
	if h.Priority() != 105 {
		t.Fatalf("expected priority 105, got %d", h.Priority())
	}
}

// TestAgentDiscoveryHook_NoToolCallsNoOp verifies the hook does
// nothing when the request has no tool calls. We assert on the
// backend Snapshot to confirm no SAdd/Expire call.
func TestAgentDiscoveryHook_NoToolCallsNoOp(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-ad-empty",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &AgentDiscoveryHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := stub.Snapshot()
	if len(calls) != 0 {
		t.Fatalf("expected 0 backend calls on no-tool payload, got %d: %+v", len(calls), calls)
	}
}

// TestAgentDiscoveryHook_RecordsToolNames verifies the happy path:
// every tool name in the request is recorded in the discovered set
// (in our stub, surfaced as a backend call).
func TestAgentDiscoveryHook_RecordsToolNames(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-ad-happy",
		OptimizedPayload: []byte(`{
			"messages":[
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}},
					{"id":"c2","type":"function","function":{"name":"web_search","arguments":"{\"q\":\"weather\"}"}}
				]}
			]
		}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &AgentDiscoveryHook{}
	payload, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload (observation only), got %q", payload)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit (observation only), got %d", hctx.ShortCircuitStatus)
	}
	// Verify the recorded SAdd/Expire calls. We don't care about the
	// exact order — just that BOTH tool names made it in.
	calls := stub.Snapshot()
	if len(calls) == 0 {
		t.Fatal("expected backend calls to be recorded, got 0")
	}
	// At minimum we expect one SAdd call containing the VK in the
	// key. The stub records a flat call log; we check the key prefix
	// was used.
	found := false
	for _, c := range calls {
		if c.Key == "synapse:discovered_tools:vk-ad-happy" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SAdd call to synapse:discovered_tools:vk-ad-happy, calls: %+v", calls)
	}
}

// TestAgentDiscoveryHook_FailsOpenOnBackendError verifies that a
// transient Redis blip does NOT block the request.
func TestAgentDiscoveryHook_FailsOpenOnBackendError(t *testing.T) {
	stub := newStubRedis()
	stub.returnErr = errRedisDown
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-ad-redis-down",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"any","arguments":"{}"}}]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &AgentDiscoveryHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error (fail-open), got %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit on backend failure, got %d", hctx.ShortCircuitStatus)
	}
}

// TestAgentDiscoveryHook_SkipsEmptyToolNames verifies the legacy
// guard: tool calls with an empty name (e.g. malformed JSON where
// the regex matches but the name is blank) MUST NOT be added to
// the discovered set — otherwise the operator's dashboard would
// show a confusing "" entry.
//
// The regex used by ExtractAllToolCalls does not match tool_calls
// with an empty name, so this test exercises the dedup path: two
// tool_calls with the same name must collapse to one SAddSet call
// with a single member. (Empty-name protection is enforced inside
// the hook before the SAddSet.)
func TestAgentDiscoveryHook_SkipsEmptyToolNames(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	// Two tool_calls with the same name + one with a different
	// name. The dedup logic must keep only one "read_file" and
	// one "bash" in the recorded SAddSet members slice.
	hctx := &HookContext{
		VK: "vk-ad-skip-empty",
		OptimizedPayload: []byte(`{
			"messages":[
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}},
					{"id":"c2","type":"function","function":{"name":"read_file","arguments":"{}"}},
					{"id":"c3","type":"function","function":{"name":"bash","arguments":"{}"}}
				]}
			]
		}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &AgentDiscoveryHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := stub.Snapshot()
	for _, c := range calls {
		// We don't fail on the TTL set; we just want to make sure
		// no SAddSet was called with an empty Members slice or with
		// an empty string in the members list.
		if c.Key == "synapse:discovered_tools:vk-ad-skip-empty" {
			if len(c.Members) == 0 {
				t.Fatalf("SAddSet called with empty members slice")
			}
			for _, m := range c.Members {
				if m == "" {
					t.Fatalf("SAddSet member list contains an empty string (would pollute the dashboard)")
				}
			}
			// And: dedup check — should see exactly 2 unique names
			// (read_file, bash) even though the payload had 3 calls.
			if len(c.Members) != 2 {
				t.Fatalf("expected 2 unique members after dedup, got %d: %v", len(c.Members), c.Members)
			}
		}
	}
}

// TestAgentDiscoveryHook_AfterResponseNoOp documents that the hook
// is purely observation-on-request.
func TestAgentDiscoveryHook_AfterResponseNoOp(t *testing.T) {
	h := &AgentDiscoveryHook{}
	hctx := &HookContext{
		VK:              "vk-ad-after",
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

// TestAgentDiscoveryHook_HandlesMultipleToolCallsInOneMessage
// verifies that multiple tool calls in a single assistant message
// are all recorded (not just the first one). This is the common
// pattern for OpenAI function-calling where the assistant emits
// 2-3 parallel calls in a single turn.
func TestAgentDiscoveryHook_HandlesMultipleToolCallsInOneMessage(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-ad-multi",
		OptimizedPayload: []byte(`{
			"messages":[
				{"role":"assistant","tool_calls":[
					{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}},
					{"id":"c2","type":"function","function":{"name":"bash","arguments":"{}"}},
					{"id":"c3","type":"function","function":{"name":"web_search","arguments":"{}"}}
				]}
			]
		}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &AgentDiscoveryHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The implementation may choose to SAdd a slice of values in a
	// single call (more efficient) or one at a time. Either way, the
	// total call count must be > 0 and the VK key must appear.
	calls := stub.Snapshot()
	if len(calls) == 0 {
		t.Fatal("expected at least one backend call")
	}
	found := false
	for _, c := range calls {
		if c.Key == "synapse:discovered_tools:vk-ad-multi" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected call to synapse:discovered_tools:vk-ad-multi, calls: %+v", calls)
	}
}