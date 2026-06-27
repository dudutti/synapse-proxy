// Tests for ModelRadarHook — extracted from proxy.go inline call
// to optiagent.CheckAndFlagNewModel (formerly line ~263) into
// the hook pipeline.
//
// Behaviour preserved:
//   - When the request model is NOT in the Redis "known models"
//     set, the hook creates a learning entry under
//     `synapse:radar:models:<modelID>` and exposes a
//     `model_radar_new` feature on hctx for downstream consumers.
//   - When the model IS already known, the hook updates its
//     last-seen timestamp.
//   - Fail open on backend errors.
//   - Empty model / no backend / no provider => no-op.

package optiagent

import (
	"net/http"
	"strings"
	"testing"
)

// TestModelRadarHook_NameAndPriority verifies the stable identity.
func TestModelRadarHook_NameAndPriority(t *testing.T) {
	h := &ModelRadarHook{}
	if h.Name() != "model_radar" {
		t.Fatalf("expected name 'model_radar', got %q", h.Name())
	}
	// Priority 160: runs after LoopDetection (150) and before
	// cache mutators. A flagged new model is metadata, not a
	// short-circuit, so it can run late in the observation phase.
	if h.Priority() != 160 {
		t.Fatalf("expected priority 160, got %d", h.Priority())
	}
}

// TestModelRadarHook_NewModelDetected verifies the cold path:
// a model not in the known set triggers a learning entry and
// surfaces `model_radar_new` on the hook context.
func TestModelRadarHook_NewModelDetected(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-mr-new",
		Provider:        "openai",
		Model:           "gpt-99-unknown",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ModelRadarHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Feature must be set so the proxy can include "new model" in
	// its telemetry row.
	val, ok := hctx.Feature("model_radar_new")
	if !ok {
		t.Fatal("expected Features[model_radar_new] to be set on a new model")
	}
	if v, _ := val.(bool); !v {
		t.Fatalf("expected model_radar_new=true, got %v", val)
	}
	// The stub should have recorded at least an SIsMember (to check
	// known models) and a Set (to write the learning entry).
	calls := stub.Snapshot()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 backend calls, got %d: %+v", len(calls), calls)
	}
	foundSet := false
	for _, c := range calls {
		if c.Op == "set" && strings.HasPrefix(c.Key, "synapse:radar:models:") {
			foundSet = true
		}
	}
	if !foundSet {
		t.Fatalf("expected Set on synapse:radar:models:<id>, calls: %+v", calls)
	}
}

// TestModelRadarHook_KnownModelNoOp verifies the warm path: a
// model already in the known set does NOT trigger `model_radar_new`
// and does NOT create a learning entry.
func TestModelRadarHook_KnownModelNoOp(t *testing.T) {
	stub := newStubRedis()
	stub.addKnownModel("openai", "gpt-4o")
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-mr-known",
		Provider:        "openai",
		Model:           "gpt-4o",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ModelRadarHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := hctx.Feature("model_radar_new"); has {
		t.Fatal("Features[model_radar_new] must not be set for a known model")
	}
	// We still expect the last-seen update (Get + Set on the
	// entry key), but the SISMEMBER result was true so the
	// "create new entry" branch must NOT fire.
	calls := stub.Snapshot()
	// Count Set calls on the learning entry: at most 1 (the
	// last-seen update), never "create a new entry" which would
	// include a distinctive Notes="auto-flagged by proxy" body.
	for _, c := range calls {
		if c.Op == "set" && strings.Contains(c.ValueStr(), "auto-flagged by proxy") {
			t.Fatalf("known model triggered a 'new entry' Set, calls: %+v", calls)
		}
	}
}

// TestModelRadarHook_AlreadyFlaggedModelSkipsRecreate verifies
// that a model that was flagged in a previous request (entry
// exists in Redis) gets a last-seen update but no new entry
// is created. This is the legacy "Update last-seen" branch.
func TestModelRadarHook_AlreadyFlaggedModelSkipsRecreate(t *testing.T) {
	stub := newStubRedis()
	// SISMEMBER returns false (not in known set), Exists returns
	// true (entry already created on a previous request), and the
	// stored body is the existing JSON entry the hook should
	// re-stamp with last-seen.
	existingJSON := []byte(`{"model_id":"gpt-99-unknown","provider":"openai","status":"learning","sample_count":3,"notes":"pre-existing"}`)
	stub.seedRadarEntry("synapse:radar:models:gpt-99-unknown", existingJSON)
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-mr-flagged",
		Provider:        "openai",
		Model:           "gpt-99-unknown",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ModelRadarHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The hook should NOT have created a new entry (the
	// "auto-flagged" Set must not fire — the existing entry's
	// notes are preserved).
	calls := stub.Snapshot()
	for _, c := range calls {
		if c.Op == "set" && strings.Contains(c.ValueStr(), "auto-flagged by proxy") {
			t.Fatalf("already-flagged model triggered a new entry, calls: %+v", calls)
		}
	}
	// And the new model flag must NOT be set.
	if _, has := hctx.Feature("model_radar_new"); has {
		t.Fatal("Features[model_radar_new] must not be set on already-flagged model")
	}
}

// TestModelRadarHook_EmptyModelNoOp verifies the empty-model
// guard. The legacy code returned false on rdb==nil OR modelID=="".
func TestModelRadarHook_EmptyModelNoOp(t *testing.T) {
	stub := newStubRedis()
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-mr-empty",
		Provider:        "openai",
		Model:           "", // empty
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ModelRadarHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := hctx.Feature("model_radar_new"); has {
		t.Fatal("Features[model_radar_new] must not be set on empty model")
	}
	if len(stub.Snapshot()) != 0 {
		t.Fatalf("expected 0 backend calls on empty model, got %d: %+v",
			len(stub.Snapshot()), stub.Snapshot())
	}
}

// TestModelRadarHook_RedisErrorFailsOpen verifies the fail-open
// contract: a Redis blip must NOT block the request. The hook
// would rather miss a new-model flag than break the agent.
func TestModelRadarHook_RedisErrorFailsOpen(t *testing.T) {
	stub := newStubRedis()
	stub.returnErr = errRedisDown
	defer SetSessionCBBackendForTest(stub)()

	hctx := &HookContext{
		VK:              "vk-mr-down",
		Provider:        "openai",
		Model:           "gpt-99-unknown",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ModelRadarHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error (fail-open), got %v", err)
	}
	if _, has := hctx.Feature("model_radar_new"); has {
		t.Fatal("Features[model_radar_new] must not be set on Redis failure")
	}
}

// TestModelRadarHook_NoBackendFailsOpen verifies the no-backend
// case: same fail-open contract as the other hooks.
func TestModelRadarHook_NoBackendFailsOpen(t *testing.T) {
	defer SetSessionCBBackendForTest(nil)()

	hctx := &HookContext{
		VK:              "vk-mr-noback",
		Provider:        "openai",
		Model:           "gpt-99-unknown",
		OptimizedPayload: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders: http.Header{},
		Features:        map[string]interface{}{},
	}
	h := &ModelRadarHook{}
	_, err := h.BeforeRequest(testCtx(), hctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit, got %d", hctx.ShortCircuitStatus)
	}
}

// TestModelRadarHook_AfterResponseNoOp documents that the hook
// is purely observation-on-request. The legacy inline code had
// no AfterResponse counterpart either.
func TestModelRadarHook_AfterResponseNoOp(t *testing.T) {
	h := &ModelRadarHook{}
	hctx := &HookContext{
		VK:              "vk-mr-after",
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