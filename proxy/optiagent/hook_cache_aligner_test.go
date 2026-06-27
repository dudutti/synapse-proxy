// Tests for CacheAlignerHook. Inspired by Headroom's
// headroom/transforms/cache_aligner.py (PR-A2 / P2-23):
// the detector never mutates the prompt, it only logs warnings
// when the system prompt contains volatile / dynamic content
// (UUIDs, ISO 8601 timestamps, JWTs, hex hashes). The reason
// is that the system prompt is the provider's KV-cache hot
// zone — any mutation invalidates the cache for every
// subsequent request. We just observe and warn.
//
// References:
//   - headroom/transforms/cache_aligner.py (the detector logic)
//   - docs/HEADROOM_INTEGRATION_ROADMAP.md (Quick Win #1)

package optiagent

import (
	"context"
	"strings"
	"testing"
)

// TestCacheAlignerHook_DetectsUUIDInSystemPrompt: a system
// message that embeds a UUID (e.g. a session id or request id)
// is volatile. The hook must flag it via the ccr_cache_aligner_warning
// feature but MUST NOT modify the prompt — mutating the system
// message would invalidate the provider's prompt cache.
func TestCacheAlignerHook_DetectsUUIDInSystemPrompt(t *testing.T) {
	h := &CacheAlignerHook{}
	in := []byte(`{"messages":[{"role":"system","content":"You are an agent. session=550e8400-e29b-41d4-a716-446655440000"},{"role":"user","content":"hi"}]}`)
	hctx := &HookContext{
		VK: "vk-uuid",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)

	// The payload must be returned unchanged.
	if string(out) != string(in) {
		t.Fatalf("CacheAligner must not mutate the payload\n  got:  %q\n  want: %q", string(out), string(in))
	}
	// The ccr_cache_aligner_warning feature must be set, with
	// the matched kind ("uuid") in the value.
	v, ok := hctx.Feature("ccr_cache_aligner_warning")
	if !ok {
		t.Fatal("expected ccr_cache_aligner_warning to be set when a UUID is detected")
	}
	got := v.(string)
	if !strings.Contains(got, "uuid") {
		t.Fatalf("expected warning to mention uuid, got %q", got)
	}
}

// TestCacheAlignerHook_DetectsISO8601Timestamp: timestamps
// in the system prompt are also volatile. Each request would
// have a different one and bust the cache.
func TestCacheAlignerHook_DetectsISO8601Timestamp(t *testing.T) {
	h := &CacheAlignerHook{}
	in := []byte(`{"messages":[{"role":"system","content":"Today is 2026-06-24T15:30:00Z. Help the user."},{"role":"user","content":"hi"}]}`)
	hctx := &HookContext{
		VK: "vk-iso",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	if string(out) != string(in) {
		t.Fatalf("payload was mutated (must not be)\n  got:  %q\n  want: %q", string(out), string(in))
	}
	v, ok := hctx.Feature("ccr_cache_aligner_warning")
	if !ok {
		t.Fatal("expected ccr_cache_aligner_warning to be set for an ISO 8601 timestamp")
	}
	if !strings.Contains(v.(string), "iso8601") {
		t.Fatalf("expected warning to mention iso8601, got %q", v.(string))
	}
}

// TestCacheAlignerHook_DetectsJWT: a JWT in the system prompt
// is a strong sign that the caller is including the user's
// auth token in the cache hot zone. We flag it as volatile.
func TestCacheAlignerHook_DetectsJWT(t *testing.T) {
	h := &CacheAlignerHook{}
	in := []byte(`{"messages":[{"role":"system","content":"token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"}]}`)
	hctx := &HookContext{
		VK: "vk-jwt",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	_, _ = h.BeforeRequest(context.Background(), hctx)
	v, ok := hctx.Feature("ccr_cache_aligner_warning")
	if !ok {
		t.Fatal("expected ccr_cache_aligner_warning to be set for a JWT")
	}
	if !strings.Contains(v.(string), "jwt") {
		t.Fatalf("expected warning to mention jwt, got %q", v.(string))
	}
}

// TestCacheAlignerHook_NoFalsePositiveOnStaticPrompt: a
// well-behaved system prompt that contains none of the volatile
// patterns must NOT trigger the warning. Otherwise we'd be
// crying wolf on every request and operators would start
// ignoring the warnings.
func TestCacheAlignerHook_NoFalsePositiveOnStaticPrompt(t *testing.T) {
	h := &CacheAlignerHook{}
	in := []byte(`{"messages":[{"role":"system","content":"You are a helpful assistant. Answer concisely."},{"role":"user","content":"hi"}]}`)
	hctx := &HookContext{
		VK: "vk-clean",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	_, _ = h.BeforeRequest(context.Background(), hctx)
	if v, ok := hctx.Feature("ccr_cache_aligner_warning"); ok {
		t.Fatalf("did not expect a warning on a clean prompt, got: %v", v)
	}
}

// TestCacheAlignerHook_DoesNotMutateSystemPrompt: a regression
// guard. The whole point of the CacheAligner is that it's a
// detector, not a rewriter. If a future change mutates the
// payload, this test fails loud. Headroom removed their
// rewrite path for exactly this reason (PR-A2).
func TestCacheAlignerHook_DoesNotMutateSystemPrompt(t *testing.T) {
	h := &CacheAlignerHook{}
	original := `{"messages":[{"role":"system","content":"session=550e8400-e29b-41d4-a716-446655440000, ts=2026-06-24T15:30:00Z, jwt=eyJhbGciOi.eyJzdWIi.SflKxw"},{"role":"user","content":"hi"}]}`
	in := []byte(original)
	hctx := &HookContext{
		VK: "vk-no-mutate",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	if string(out) != original {
		t.Fatalf("CacheAligner mutated the payload — this is a regression\n  got:  %q\n  want: %q", string(out), original)
	}
	// All three volatile patterns should be detected.
	v, _ := hctx.Feature("ccr_cache_aligner_warning")
	got := v.(string)
	for _, kind := range []string{"uuid", "iso8601", "jwt"} {
		if !strings.Contains(got, kind) {
			t.Fatalf("expected warning to mention %s, got: %s", kind, got)
		}
	}
}
