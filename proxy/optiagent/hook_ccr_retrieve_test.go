// Tests for CCR Retrieve + Store hooks. These close the
// CCR loop: CCR CompressHook computes a canonical hash
// (see hook_ccr_compress_test.go) but without a Retrieve
// step the hash is never used as a cache key, and without
// a Store step the response is never written under that
// key. Together the two hooks make CCR a real cache level,
// not just a hash computation.
//
// Reference: headroom/docs/ccr.md (the Headroom equivalent
// is described but not implemented; we implement both hooks
// here because we need the cache level for our internal
// L0-L1-L2-L3 + CCR pipeline).

package optiagent

import (
	"context"
	"testing"
	"time"
)

// TestCCRRetrieveHook_HitShortCircuits: when the CCR hash
// already has a stored response in Redis, the hook must
// short-circuit the request with HTTP 200 and the stored
// body. This is the whole point of CCR — an identical
// canonical prompt never has to hit the upstream.
func TestCCRRetrieveHook_HitShortCircuits(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub) // restore

	body := []byte(`{"id":"cached","choices":[{"message":{"content":"cached answer"}}]}`)
	if err := stub.Set(context.Background(), "ccr:aabbccdd", body, time.Hour); err != nil {
		t.Fatalf("seed Set failed: %v", err)
	}

	h := &CCRRetrieveHook{}
	hctx := &HookContext{
		VK: "vk-ccr-hit",
		Features: map[string]interface{}{
			"ccr_hash": "aabbccdd",
		},
	}
	out, err := h.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}
	if hctx.ShortCircuitStatus != 200 {
		t.Fatalf("expected short-circuit status 200, got %d", hctx.ShortCircuitStatus)
	}
	if string(hctx.ShortCircuitBody) != string(body) {
		t.Fatalf("expected short-circuit body %q, got %q", string(body), string(hctx.ShortCircuitBody))
	}
	// We don't assert on `out` here: the hook contract is
	// "short-circuit when hit, return whatever upstream was
	// going to send otherwise". The short-circuit signals
	// the proxy to use ShortCircuitBody+Status, not out.
	_ = out
}

// TestCCRRetrieveHook_MissIsNoOp: when the hash is not in
// Redis, the hook must NOT short-circuit. The request
// must proceed to upstream normally.
func TestCCRRetrieveHook_MissIsNoOp(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	h := &CCRRetrieveHook{}
	hctx := &HookContext{
		VK: "vk-ccr-miss",
		Features: map[string]interface{}{
			"ccr_hash": "deadbeef",
		},
	}
	_, err := h.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit on miss, got status %d", hctx.ShortCircuitStatus)
	}
	if hctx.ShortCircuitBody != nil {
		t.Fatalf("expected no short-circuit body on miss, got %q", string(hctx.ShortCircuitBody))
	}
}

// TestCCRRetrieveHook_NoHashIsNoOp: if the ccr_hash feature
// wasn't set (e.g. non-chat payload, or CCR CompressHook
// wasn't run), the hook must not crash or short-circuit.
// It's a no-op.
func TestCCRRetrieveHook_NoHashIsNoOp(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	h := &CCRRetrieveHook{}
	hctx := &HookContext{
		VK: "vk-ccr-nohash",
		Features: map[string]interface{}{},
	}
	_, err := h.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("expected no short-circuit when ccr_hash absent")
	}
}

// TestCCRRetrieveHook_RedisErrorFailsOpen: if Redis is
// down or returns an error, the hook must NOT short-circuit
// (we'd rather hit upstream than 5xx the client). Fail-open
// is the right default here because cache is a perf
// optimization, not a correctness primitive.
func TestCCRRetrieveHook_RedisErrorFailsOpen(t *testing.T) {
	// The simplest way to force the hook to encounter a
	// Redis error is to set the backend to nil. The hook
	// must guard against nil and treat it as a no-op
	// (fail-open).
	SetSessionCBBackendForTest(nil)
	defer SetSessionCBBackendForTest(nil)

	h := &CCRRetrieveHook{}
	hctx := &HookContext{
		VK: "vk-ccr-redis-down",
		Features: map[string]interface{}{
			"ccr_hash": "anything",
		},
	}
	_, err := h.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest should not propagate Redis errors (fail-open), got: %v", err)
	}
	if hctx.ShortCircuitStatus != 0 {
		t.Fatalf("Redis errors must not trigger short-circuit")
	}
}

// TestCCRStoreHook_PersistsResponse: when the upstream
// returns 200 and the CCR hash is set, the hook must write
// the response body to Redis under the CCR key, with a
// TTL. This is the write side of the CCR cache.
func TestCCRStoreHook_PersistsResponse(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	body := []byte(`{"id":"new","choices":[{"message":{"content":"new answer"}}]}`)
	h := &CCRStoreHook{}
	hctx := &HookContext{
		VK: "vk-ccr-store",
		UpstreamResponse: body,
		UpstreamStatus: 200,
		Features: map[string]interface{}{
			"ccr_hash": "feedface",
		},
	}
	if _, err := h.AfterResponse(context.Background(), hctx); err != nil {
		t.Fatalf("AfterResponse returned error: %v", err)
	}
	got, err := stub.Get(context.Background(), "ccr:feedface")
	if err != nil {
		t.Fatalf("expected the response to be stored under ccr:feedface, but Get returned: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("stored body mismatch\n  got:  %q\n  want: %q", string(got), string(body))
	}
}

// TestCCRStoreHook_SkipsNon200Responses: cache only the
// successful responses. 4xx and 5xx upstream errors must
// not be cached (otherwise a transient upstream bug would
// be cached for the full TTL and the next request would
// also get the bad response).
func TestCCRStoreHook_SkipsNon200Responses(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	h := &CCRStoreHook{}
	for _, status := range []int{400, 401, 403, 404, 429, 500, 502, 503} {
		hctx := &HookContext{
			VK: "vk-ccr-" + string(rune(status)),
			UpstreamResponse: []byte(`{"error":"oops"}`),
			UpstreamStatus: status,
			Features: map[string]interface{}{
				"ccr_hash": "skipped",
			},
		}
		_, _ = h.AfterResponse(context.Background(), hctx)
	}
	// The key must not be set (or set to a non-error body).
	if got, _ := stub.Get(context.Background(), "ccr:skipped"); len(got) > 0 {
		t.Fatalf("expected no cache write for non-200, got: %q", string(got))
	}
}

// TestCCRStoreHook_DoesNotOverwriteExisting: if another
// concurrent request already wrote the response (or we
// wrote it on a previous request), we must NOT overwrite
// it. The first writer wins. This is the canonical "set if
// not exists" pattern for distributed cache writes.
func TestCCRStoreHook_DoesNotOverwriteExisting(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	// Seed: an existing cached response.
	existing := []byte(`{"cached":"first"}`)
	if err := stub.Set(context.Background(), "ccr:race", existing, time.Hour); err != nil {
		t.Fatalf("seed Set failed: %v", err)
	}

	// Try to store a different response. The hook must
	// detect the existing key and bail.
	h := &CCRStoreHook{}
	hctx := &HookContext{
		VK: "vk-ccr-race",
		UpstreamResponse: []byte(`{"cached":"second"}`),
		UpstreamStatus: 200,
		Features: map[string]interface{}{
			"ccr_hash": "race",
		},
	}
	_, _ = h.AfterResponse(context.Background(), hctx)
	got, _ := stub.Get(context.Background(), "ccr:race")
	if string(got) != string(existing) {
		t.Fatalf("CCRStore overwrote an existing entry\n  got:  %q\n  want: %q", string(got), string(existing))
	}
}

// TestCCRPipeline_EndToEnd: the full CCR loop. Compress
// computes a hash. Retrieve misses (first time). Upstream
// serves. Store writes. Second request with the same
// canonical payload: Retrieve hits, short-circuits with
// the cached body. This is the user-visible win: a
// byte-different but semantically-equal request gets a
// cached response on the second call.
func TestCCRPipeline_EndToEnd(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	// The two payloads are byte-different (different
	// whitespace) but semantically equal. CCR Compress
	// canonicalizes them to the same hash.
	canonical := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	variant := []byte(`{"messages":[{"role":"user","content":"hello   "}]}`)

	// 1. First request: Compress sets ccr_hash. Retrieve
	// misses. Upstream responds. Store writes.
	compress := &CCRCompressHook{}
	retrieve := &CCRRetrieveHook{}
	store := &CCRStoreHook{}

	hctx1 := &HookContext{
		VK: "vk-e2e",
		OptimizedPayload: canonical,
		Features: map[string]interface{}{},
	}
	compress.BeforeRequest(context.Background(), hctx1)
	hash1, _ := hctx1.Feature("ccr_hash")
	if hash1 == nil {
		t.Fatal("first request: ccr_hash not set by Compress")
	}
	retrieve.BeforeRequest(context.Background(), hctx1)
	if hctx1.ShortCircuitStatus != 0 {
		t.Fatal("first request: should NOT short-circuit on miss")
	}
	// Simulate upstream response.
	hctx1.UpstreamStatus = 200
	hctx1.UpstreamResponse = []byte(`{"answer":"hi from upstream"}`)
	store.AfterResponse(context.Background(), hctx1)

	// 2. Second request: same canonical, but with a
	// different payload (the variant). Compress must
	// produce the same hash. Retrieve must hit.
	hctx2 := &HookContext{
		VK: "vk-e2e",
		OptimizedPayload: variant,
		Features: map[string]interface{}{},
	}
	compress.BeforeRequest(context.Background(), hctx2)
	hash2, _ := hctx2.Feature("ccr_hash")
	if hash2 != hash1 {
		t.Fatalf("CCR Compress produced different hashes for semantically-equal payloads\n  hash1: %v\n  hash2: %v", hash1, hash2)
	}
	retrieve.BeforeRequest(context.Background(), hctx2)
	if hctx2.ShortCircuitStatus != 200 {
		t.Fatalf("second request: expected short-circuit 200, got %d", hctx2.ShortCircuitStatus)
	}
	if string(hctx2.ShortCircuitBody) != `{"answer":"hi from upstream"}` {
		t.Fatalf("second request: short-circuit body mismatch\n  got:  %q", string(hctx2.ShortCircuitBody))
	}
}
