// Tests for CCRCompressHook — extracts the compression logic into
// a dedicated hook so the proxy can cache at the *semantic* level
// rather than the *byte* level (L1) or the *embedding-similar* level
// (L2). See docs/ccr.md for the design.
//
// The compression is intentionally a single-pass, deterministic,
// side-effect-free transform: given the same input bytes, it always
// produces the same output bytes. That way the cache key
// (sha256(compressed)) is stable, and a CCR Retrieve that compares
// the hash of the new payload to the stored hash will only ever
// match byte-identical compressed inputs.

package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// TestCCRCompressHook_StripsTrailingWhitespace verifies that
// trailing whitespace and newlines on a user content string
// are dropped. (LLMs do not care about a single trailing space
// but the byte hash does, and most real-world LLM clients add
// a stray newline.)
func TestCCRCompressHook_StripsTrailingWhitespace(t *testing.T) {
	h := &CCRCompressHook{}
	// "hello   \n\n" → internal 3-space run collapses to 1
	// space, then trailing "\n\n" is trimmed. The result is
	// "hello" (the trailing space after collapsing is itself
	// trailing and is trimmed).
	in := []byte(`{"messages":[{"role":"user","content":"hello   \n\n"}]}`)
	out, _ := h.compress(in)
	// We don't assert on the exact bytes (Go's json.Marshal
	// sorts map keys alphabetically, so the order isn't
	// guaranteed). Instead, we parse the output and verify that
	// the content field is now "hello".
	var got struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n  out: %q", err, string(out))
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hello" {
		t.Fatalf("trailing whitespace not stripped\n  got:  %q\n  want: %q", got.Messages[0].Content, "hello")
	}
}

// TestCCRCompressHook_CollapsesInternalRunsOfSpaces: two
// semantically equivalent prompts ("hello world" vs "hello
//   world") should hash to the same cache key. This is the
// core invariant CCR depends on.
func TestCCRCompressHook_CollapsesInternalRunsOfSpaces(t *testing.T) {
	h := &CCRCompressHook{}
	a := []byte(`{"messages":[{"role":"user","content":"hello   world"}]}`)
	b := []byte(`{"messages":[{"role":"user","content":"hello world"}]}`)
	ca, _ := h.compress(a)
	cb, _ := h.compress(b)
	ha := sha256.Sum256(ca)
	hb := sha256.Sum256(cb)
	if ha != hb {
		t.Fatalf("two semantically equal inputs produced different hashes after compression\n  a hash: %s\n  b hash: %s", hex.EncodeToString(ha[:]), hex.EncodeToString(hb[:]))
	}
}

// TestCCRCompressHook_NormalizesNewlines verifies that the
// canonicalizeString helper (the actual newline-normalization
// function) replaces CRLF with LF regardless of how the bytes
// arrive. We test the helper directly because a raw 0x0D byte
// is invalid JSON, so we can't test it via compress() with a
// realistic input.
func TestCCRCompressHook_NormalizesNewlines(t *testing.T) {
	// Build CRLF with raw bytes (NOT in a JSON string, just
	// testing the helper in isolation).
	crlf := "line1\r\nline2"
	lf := "line1\nline2"
	cCRLF := canonicalizeString(crlf)
	cLF := canonicalizeString(lf)
	if cCRLF != cLF {
		t.Fatalf("newline normalization didn't converge\n  crlf: %q\n  lf:   %q", cCRLF, cLF)
	}
	// And the trailing-whitespace strip works alongside the
	// newline normalization.
	crlfTrailing := "line1\r\nline2   	\n"
	cTrailing := canonicalizeString(crlfTrailing)
	if cTrailing != "line1\nline2" {
		t.Fatalf("trailing whitespace not stripped after CRLF normalization\n  got: %q\n  want: %q", cTrailing, "line1\nline2")
	}
}

// TestCCRCompressHook_PreservesToolCallArguments: this is the
// regression guard. We CANNOT collapse the whitespace inside a
// tool_calls.function.arguments string (it's JSON, not natural
// language). A naive compress that didn't special-case tool
// calls would corrupt the JSON and the upstream would 400.
// This test makes that an explicit failure.
func TestCCRCompressHook_PreservesToolCallArguments(t *testing.T) {
	h := &CCRCompressHook{}
	in := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/etc/hosts\"}"}}]}]}`)
	out, _ := h.compress(in)
	// The arguments string must round-trip byte-identical.
	if string(in) != string(out) {
		t.Fatalf("tool_call arguments were modified by compression\n  got:  %q\n  want: %q", string(out), string(in))
	}
}

// TestCCRCompressHook_LeavesCacheHeadersAlone: a 200-char prompt
// must NOT be transformed. The point of CCR is to be a no-op when
// the input is already canonical.
func TestCCRCompressHook_LeavesCacheHeadersAlone(t *testing.T) {
	h := &CCRCompressHook{}
	in := []byte(`{"messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"What is 2+2?"}]}`)
	out, _ := h.compress(in)
	if string(in) != string(out) {
		t.Fatalf("canonical input was rewritten\n  got:  %q\n  want: %q", string(out), string(in))
	}
}

// TestCCRCompressHook_BeforeRequestIsNoOpWhenNoMessages: the hook
// must not panic or crash on payloads that don't have a
// "messages" field (e.g. legacy /v1/completions calls, or
// embeddings requests). It should just leave the OptimizedPayload
// untouched on the hctx and set NO ccr_hash feature.
func TestCCRCompressHook_BeforeRequestIsNoOpWhenNoMessages(t *testing.T) {
	h := &CCRCompressHook{}
	cases := [][]byte{
		[]byte(`{"prompt":"Tell me a joke"}`),
		[]byte(`{"input":"embed this"}`),
		[]byte(`{}`),
		[]byte(``),
	}
	for _, in := range cases {
		hctx := &HookContext{
			VK: "vk-ccr-noop",
			OptimizedPayload: in,
			Features: map[string]interface{}{},
		}
		_, _ = h.BeforeRequest(context.Background(), hctx)
		// OptimizedPayload must remain the input (hook is no-op).
		if string(hctx.OptimizedPayload) != string(in) {
			t.Fatalf("non-messages payload was modified by BeforeRequest\n  in:  %q\n  out: %q", string(in), string(hctx.OptimizedPayload))
		}
		// CCRCompressedPayload must not be set.
		if hctx.CCRCompressedPayload != nil {
			t.Fatalf("CCRCompressedPayload was set for a non-chat payload: %q", string(hctx.CCRCompressedPayload))
		}
		// ccr_hash feature must not be set.
		if v, ok := hctx.Feature("ccr_hash"); ok {
			t.Fatalf("ccr_hash feature was set for a non-chat payload: %v", v)
		}
	}
}

// TestCCRCompressHook_ExposesCompressedHashOnContext: the hook
// must set hctx.CCRHash (a new field) to the sha256 of the
// compressed payload. The CCR Retrieve hook reads this field to
// look up a cached response.
func TestCCRCompressHook_ExposesCompressedHashOnContext(t *testing.T) {
	h := &CCRCompressHook{}
	in := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	hctx := &HookContext{
		VK: "vk-ccr-hash",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	h.BeforeRequest(context.Background(), hctx)
	got, _ := hctx.Feature("ccr_hash")
	if got == nil {
		t.Fatal("expected ccr_hash feature to be set")
	}
	want := sha256.Sum256([]byte(`{"messages":[{"role":"user","content":"hello"}]}`))
	if got.(string) != hex.EncodeToString(want[:]) {
		t.Fatalf("ccr_hash mismatch\n  got:  %s\n  want: %s", got.(string), hex.EncodeToString(want[:]))
	}
}

// TestCCRCompressHook_StripsBlankSystemMessages is the slightly
// more opinionated canonicalization. Some clients add an empty
// "You are a helpful assistant." system message at the start
// of every request, which is the most common reason L1 cache
// misses when the only thing that changed is whitespace in a
// harmless system prompt. We strip the empty-and-default
// ones so the cache key matches.
func TestCCRCompressHook_StripsBlankSystemMessages(t *testing.T) {
	h := &CCRCompressHook{}
	a := []byte(`{"messages":[{"role":"system","content":""},{"role":"user","content":"hi"}]}`)
	b := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	ca, _ := h.compress(a)
	cb, _ := h.compress(b)
	ha := sha256.Sum256(ca)
	hb := sha256.Sum256(cb)
	if ha != hb {
		t.Fatalf("blank system message was not stripped\n  a: %q\n  b: %q", string(ca), string(cb))
	}
}
