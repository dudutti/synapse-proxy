// Tests for ToolDedupHook — extracted from proxy.go inline block
// (formerly lines ~252-259) into the hook pipeline.
//
// Behaviour preserved:
//   - When the request has a single file-read tool call (or
//     multiple calls all targeting the same file path):
//       1. Hash the file path (sha256) → key = "synapse:tools:<vk>:<hash>"
//       2. GET the key. If a body is stored, this is a dedup hit:
//          increment a hits counter and return HasDup=true.
//       3. Otherwise SET a placeholder body with a 5-minute TTL.
//   - When the request has multiple file-read calls to DIFFERENT
//     paths, return early with no work done (the proxy's
//     CheckToolDedup returns the zero value in that case).
//   - When there are no file-read tool calls at all, return early.
//   - The hook is observation-only: it sets the dedup info on
//     HookContext.Features["tool_dedup_hit"] so other hooks and
//     the proxy can consume it, but it does NOT mutate the payload
//     or short-circuit the request.
//
// Why observation-only? The legacy inline code computed
// HasDup + ReuseBody but NEVER consumed ReuseBody to rewrite the
// payload — the call sites only logged. Migrating to the hook
// pipeline preserves that exact behaviour.

package optiagent

import (
	"net/http"
	"strings"
	"testing"
)

// TestToolDedupHook_NameAndPriority verifies the stable identity.
func TestToolDedupHook_NameAndPriority(t *testing.T) {
	h := &ToolDedupHook{}
	if h.Name() != "tool_dedup" {
		t.Fatalf("expected name 'tool_dedup', got %q", h.Name())
	}
	// Priority 140 = runs after ToolFilter (130) so a blocked
	// tool never reaches dedup (saves Redis round-trips), but
	// before L1/L2/L3 caching (200+).
	if h.Priority() != 140 {
		t.Fatalf("expected priority 140, got %d", h.Priority())
	}
}

// TestToolDedupHook_NoFileToolCallsNoOp verifies the hook does
// nothing on a request without file-read tool calls.
func TestToolDedupHook_NoFileToolCallsNoOp(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-td-plain",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"web_search","arguments":"{\"q\":\"weather\"}"}}
		]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolDedupHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := stub.Snapshot()
	if len(calls) != 0 {
		t.Fatalf("expected 0 backend calls on non-file tool, got %d: %+v", len(calls), calls)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit (observation only), got %d", hctx.ShortCircuitStatus)
	}
}

// TestToolDedupHook_FirstReadStoresPlaceholder verifies the cold
// path: a fresh file read stores a placeholder body in Redis with
// a 5-minute TTL. No dedup hit (HasDup is not set on Features).
func TestToolDedupHook_FirstReadStoresPlaceholder(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-td-cold",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}}
		]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolDedupHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The placeholder body must be retrievable under the canonical
	// key (vk + sha256 of the path). The stub records the call log
	// so we can assert the Set was issued with the right key and a
	// 5-minute TTL.
	calls := stub.Snapshot()
	if len(calls) == 0 {
		t.Fatal("expected at least one backend call (placeholder Set), got 0")
	}
	found := false
	for _, c := range calls {
		if strings.HasPrefix(c.Key, "synapse:tools:vk-td-cold:") {
			// The key should have a sha256 hex suffix; we don't
			// care about the exact value but the prefix must match.
			if len(c.Key) != len("synapse:tools:vk-td-cold:")+64 {
				t.Fatalf("expected sha256 hex suffix on key, got %q", c.Key)
			}
			// TTL should be ~5 minutes (300s).
			if c.TTL != 5*60*1_000_000_000 /* 5min in ns */ {
				t.Fatalf("expected 5m TTL on placeholder Set, got %v", c.TTL)
			}
			// The placeholder body should reference the file path
			// and tool name so the dashboard can render the dedup
			// hit context.
			if !strings.Contains(c.ValueStr(), "/etc/hosts") {
				t.Fatalf("expected placeholder to reference /etc/hosts, got %q", c.ValueStr())
			}
			if !strings.Contains(c.ValueStr(), "read_file") {
				t.Fatalf("expected placeholder to reference read_file, got %q", c.ValueStr())
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Set on synapse:tools:vk-td-cold:<hash>, calls: %+v", calls)
	}
	// Features["tool_dedup_hit"] must NOT be set on the cold path.
	if _, has := hctx.Feature("tool_dedup_hit"); has {
		t.Fatalf("Features[tool_dedup_hit] must not be set on cold path")
	}
}

// TestToolDedupHook_RepeatReadDetectsDup verifies the hot path:
// when the same file is read again, the hook sees the stored
// body, increments a hits counter, and exposes a dedup hit on
// hctx.Features["tool_dedup_hit"] for downstream consumers.
func TestToolDedupHook_RepeatReadDetectsDup(t *testing.T) {
	stub := newStubRedis()
	// Pre-seed the canonical key with a non-empty body so the
	// next call to CheckToolDedup sees a "stored" value.
	stub.setStoredBody("vk-td-hot", "/etc/hosts", []byte(`{"content":"127.0.0.1 localhost"}`))
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-td-hot",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}}
		]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolDedupHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The hook must have observed a hit and exposed it via
	// Features so the runner / proxy / downstream hooks can read it.
	hit, ok := hctx.Feature("tool_dedup_hit")
	if !ok {
		t.Fatal("expected Features[tool_dedup_hit] to be set on dedup hit")
	}
	hitMap, ok := hit.(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_dedup_hit to be a map, got %T", hit)
	}
	if hitMap["file_path"] != "/etc/hosts" {
		t.Fatalf("expected file_path=/etc/hosts, got %v", hitMap["file_path"])
	}
	if hitMap["tool_name"] != "read_file" {
		t.Fatalf("expected tool_name=read_file, got %v", hitMap["tool_name"])
	}
	// The hits counter (Incr) must have been called.
	calls := stub.Snapshot()
	incrFound := false
	for _, c := range calls {
		if strings.HasSuffix(c.Key, ":hits") && strings.HasPrefix(c.Key, "synapse:tools:vk-td-hot:") {
			incrFound = true
			break
		}
	}
	if !incrFound {
		t.Fatalf("expected Incr on synapse:tools:vk-td-hot:<hash>:hits, calls: %+v", calls)
	}
	// No placeholder Set on the hot path (we don't want to clobber
	// the stored body with a stub).
	for _, c := range calls {
		if c.Op == "set" && strings.HasPrefix(c.Key, "synapse:tools:vk-td-hot:") {
			t.Fatalf("unexpected Set on hot path, would clobber stored body: %+v", c)
		}
	}
}

// TestToolDedupHook_MultipleDistinctPathsNoOp verifies that a
// request with tool calls to DIFFERENT files does nothing
// (matches the legacy CheckToolDedup return-zero-value behaviour).
func TestToolDedupHook_MultipleDistinctPathsNoOp(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-td-multi",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}},
			{"id":"c2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/passwd\"}"}}
		]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolDedupHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := stub.Snapshot()
	if len(calls) != 0 {
		t.Fatalf("expected 0 backend calls on multi-path payload, got %d: %+v", len(calls), calls)
	}
	if _, has := hctx.Feature("tool_dedup_hit"); has {
		t.Fatal("Features[tool_dedup_hit] must not be set on multi-path payload")
	}
}

// TestToolDedupHook_BackendErrorFailsOpen verifies the fail-open
// contract: a Redis blip must not block the request. The hook
// would rather miss a dedup than break the agent.
func TestToolDedupHook_BackendErrorFailsOpen(t *testing.T) {
	stub := newStubRedis()
	stub.returnErr = errRedisDown
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-td-down",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}}
		]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolDedupHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error (fail-open), got %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit on backend failure, got %d", hctx.ShortCircuitStatus)
	}
}

// TestToolDedupHook_NonFileToolOnlyNoOp verifies the regex's
// filter: only tool calls in fileReadTools are considered. A
// "bash" tool (even with a path-looking argument) is NOT a file
// read and must not produce a dedup lookup.
func TestToolDedupHook_NonFileToolOnlyNoOp(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK: "vk-td-bash",
		OptimizedPayload: []byte(`{"messages":[{"role":"assistant","tool_calls":[
			{"id":"c1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"cat /etc/hosts\"}"}}
		]}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ToolDedupHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := stub.Snapshot()
	if len(calls) != 0 {
		t.Fatalf("expected 0 backend calls (bash is not a file read), got %d: %+v", len(calls), calls)
	}
}

// TestToolDedupHook_AfterResponseNoOp documents that the hook
// is purely observation-on-request.
func TestToolDedupHook_AfterResponseNoOp(t *testing.T) {
	h := &ToolDedupHook{}
	hctx := &HookContext{
		VK:              "vk-td-after",
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