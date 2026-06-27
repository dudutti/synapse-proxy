// Tests for SessionCircuitBreakerHook — extracted from proxy.go inline
// block (formerly lines ~299-311) into the hook pipeline.
//
// Behaviour preserved:
//   - Reads SessionTokenLimit from Features["session_token_limit"] (int)
//   - Reads SessionID from hctx.SessionID
//   - When SessionTokenLimit > 0 AND SessionID != "":
//       1. Approximate tokens of the payload via len/4.
//       2. IncrByExpire on `synapse:session_usage:<sessionID>` (TTL 24h).
//       3. If the cumulative count exceeds SessionTokenLimit,
//          set ShortCircuitStatus=400 + an OpenAI-compatible error body.
//   - Returns (nil, nil) when feature is off or no session.
//   - Fails open on backend error.

package optiagent

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestSessionCircuitBreakerHook_NameAndPriority verifies the stable
// identity used by metrics labels and the registry ordering.
func TestSessionCircuitBreakerHook_NameAndPriority(t *testing.T) {
	h := &SessionCircuitBreakerHook{}
	if h.Name() != "session_circuit_breaker" {
		t.Fatalf("expected name 'session_circuit_breaker', got %q", h.Name())
	}
	if h.Priority() != 110 {
		t.Fatalf("expected priority 110 (early counter), got %d", h.Priority())
	}
}

// TestSessionCircuitBreakerHook_IsEnabledGatedOnVK verifies the
// outermost gate: empty VK disables the hook.
func TestSessionCircuitBreakerHook_IsEnabledGatedOnVK(t *testing.T) {
	h := &SessionCircuitBreakerHook{}
	if h.IsEnabled("") {
		t.Fatal("hook should be disabled for empty VK")
	}
	if !h.IsEnabled("vk-anything") {
		t.Fatal("hook should be enabled for non-empty VK (further gates run in BeforeRequest)")
	}
}

// TestSessionCircuitBreakerHook_LimitZeroShortCircuits verifies that
// a zero (or negative) limit disables the breaker even when the
// session is set and the backend is wired.
func TestSessionCircuitBreakerHook_LimitZeroShortCircuits(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:               "vk-cb-zero",
		SessionID:        "sess-x",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{"session_token_limit": 0},
	}
	_, err := (&SessionCircuitBreakerHook{}).BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.Snapshot()) != 0 {
		t.Fatalf("backend should not have been called when limit is 0; got %d calls", len(stub.Snapshot()))
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit, got status=%d", hctx.ShortCircuitStatus)
	}
}

// TestSessionCircuitBreakerHook_NoSessionShortCircuits verifies the
// second gate: empty SessionID disables the breaker.
func TestSessionCircuitBreakerHook_NoSessionShortCircuits(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:               "vk-cb-empty",
		SessionID:        "",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{"session_token_limit": 10000},
	}
	_, err := (&SessionCircuitBreakerHook{}).BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.Snapshot()) != 0 {
		t.Fatalf("backend should not have been called when SessionID is empty; got %d calls", len(stub.Snapshot()))
	}
}

// TestSessionCircuitBreakerHook_BelowLimitAllows verifies the happy
// path: when the cumulative usage stays below the limit, the hook
// passes the payload through unchanged and does NOT short-circuit.
func TestSessionCircuitBreakerHook_BelowLimitAllows(t *testing.T) {
	stub := newStubRedis() // starts at 0, will return value of first IncrBy
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:               "vk-cb-ok",
		SessionID:        "sess-ok",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{"session_token_limit": 1000},
	}
	h := &SessionCircuitBreakerHook{}
	payload, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload (no mutation), got %q", payload)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit, got status=%d body=%q",
			hctx.ShortCircuitStatus, hctx.ShortCircuitBody)
	}
	calls := stub.Snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(calls))
	}
	if calls[0].Key != "synapse:session_usage:sess-ok" {
		t.Fatalf("unexpected key: %q", calls[0].Key)
	}
	if calls[0].Value <= 0 {
		t.Fatalf("expected positive value, got %d", calls[0].Value)
	}
	if calls[0].TTL != 24*60*60*1e9 /* 24h in ns */ {
		// 24h time.Duration is exactly 24 * time.Hour = 24 * 60 * 60 * 1e9 ns
		t.Fatalf("expected 24h TTL, got %v", calls[0].TTL)
	}
}

// TestSessionCircuitBreakerHook_OverLimitShortCircuits verifies the
// trip: when the post-increment total exceeds the limit, the hook
// sets a 400 short-circuit with an OpenAI-compatible error body so
// the runner returns it without forwarding upstream.
func TestSessionCircuitBreakerHook_OverLimitShortCircuits(t *testing.T) {
	stub := newStubBackendWithCount(5000) // simulated cumulative usage, way over limit
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:               "vk-cb-trip",
		SessionID:        "sess-trip",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{"session_token_limit": 1000},
	}
	h := &SessionCircuitBreakerHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error (fail-open), got %v", err)
	}
	if hctx.ShortCircuitStatus != http.StatusBadRequest {
		t.Fatalf("expected 400 short-circuit, got %d", hctx.ShortCircuitStatus)
	}
	body := string(hctx.ShortCircuitBody)
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("expected OpenAI-compatible error JSON, got %q", body)
	}
	if !strings.Contains(body, "Session token limit exceeded") {
		t.Fatalf("expected descriptive message, got %q", body)
	}
	if !strings.Contains(body, "1000") {
		t.Fatalf("expected limit value in message, got %q", body)
	}
}

// TestSessionCircuitBreakerHook_BackendErrorFailsOpen verifies that
// a transient backend error does NOT block the request (per the hook
// pipeline fail-open contract).
func TestSessionCircuitBreakerHook_BackendErrorFailsOpen(t *testing.T) {
	stub := newStubRedis()
	stub.returnErr = errors.New("backend down")
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:               "vk-cb-down",
		SessionID:        "sess-x",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{"session_token_limit": 1000},
	}
	h := &SessionCircuitBreakerHook{}
	payload, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error (fail-open), got %v", err)
	}
	if payload != nil {
		t.Fatalf("expected nil payload (no mutation on backend down), got %q", payload)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit on backend failure, got %d", hctx.ShortCircuitStatus)
	}
}

// TestSessionCircuitBreakerHook_NoBackendFailsOpen verifies the
// production-wiring case where the backend was never set (e.g.
// startup race). Must fail open, never crash.
func TestSessionCircuitBreakerHook_NoBackendFailsOpen(t *testing.T) {
	// Ensure both backend slots are nil.
	defer SetSessionCBBackendForTest(nil)()

	hctx := &HookContext{
		VK:               "vk-cb-noback",
		SessionID:        "sess-x",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders:  http.Header{},
		Features:         map[string]interface{}{"session_token_limit": 1000},
	}
	h := &SessionCircuitBreakerHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error when no backend, got %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit when no backend, got %d", hctx.ShortCircuitStatus)
	}
}

// TestSessionCircuitBreakerHook_AfterResponseNoOp documents that the
// hook acts only on the request side (mirrors FingerprintHook).
func TestSessionCircuitBreakerHook_AfterResponseNoOp(t *testing.T) {
	h := &SessionCircuitBreakerHook{}
	hctx := &HookContext{
		VK:               "vk-cb-after",
		OptimizedPayload: []byte(`{"messages":[]}`),
		UpstreamResponse: []byte(`{"ok":true}`),
		ResponseHeaders:  http.Header{},
	}
	body, err := h.AfterResponse(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body (no response-side mutation), got %q", body)
	}
}
