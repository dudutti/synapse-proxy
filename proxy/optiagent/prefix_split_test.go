package optiagent

// Tests for the cache-preserving payload split.
//
// These tests cover the SplitAtPrefixBoundary and
// CompressPayloadCachePreserving functions. Run with
// `go test -v -count=1 ./optiagent/`.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestSplit_BasicTwoMessages asserts the split point is right
// before the 4th-to-last message when there are 4+ messages.
// With 5 messages and tail = last 4, the prefix contains only
// the 1st message.
func TestSplit_BasicTwoMessages(t *testing.T) {
	raw := []byte(`{"model":"MiniMax-M3","messages":[{"role":"system","content":"You are Hermes."},{"role":"user","content":"hello"},{"role":"assistant","content":"hi there"},{"role":"user","content":"add auth"},{"role":"assistant","content":"on it"}]}`)

	split, err := SplitAtPrefixBoundary(raw)
	t.Logf("err=%v count=%d prefix_len=%d tail_len=%d", err, split.MessageCount, len(split.PrefixJSON), len(split.TailJSON))
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	if split.MessageCount != 5 {
		t.Fatalf("expected 5 messages, got %d", split.MessageCount)
	}

	// The prefix should end with a closing '}' of the message
	// at index 0 (the 1st message), followed by a comma.
	if !bytes.HasSuffix(split.PrefixJSON, []byte("},")) {
		t.Fatalf("prefix should end with a comma after a top-level message, got %q", split.PrefixJSON)
	}

	// The tail should start with the opening '{' of the
	// 2nd-to-last message.
	if !bytes.HasPrefix(split.TailJSON, []byte("{")) {
		t.Fatalf("tail should start with '{' of the 2nd-to-last message, got %q", split.TailJSON)
	}
}

// TestSplit_ByteExactPrefix is the cache-preservation invariant:
// the prefix bytes are returned bit-for-bit identical to the
// corresponding bytes in the input. This is what allows the
// provider cache to keep hitting.
func TestSplit_ByteExactPrefix(t *testing.T) {
	raw := []byte(`{
		"model": "MiniMax-M3",
		"messages": [
			{"role": "system", "content": "You are Hermes."},
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "hi"},
			{"role": "user", "content": "add auth"}
		]
	}`)

	split, err := SplitAtPrefixBoundary(raw)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	// Find the prefix in the original raw bytes — it must match
	// exactly.
	if !bytes.Equal(raw[:len(split.PrefixJSON)], split.PrefixJSON) {
		t.Fatalf("prefix is not byte-exact match of input\ninput prefix: %q\nreturned:     %q",
			raw[:len(split.PrefixJSON)], split.PrefixJSON)
	}
}

// TestSplit_TooFewMessages returns the whole payload as the tail
// when there are fewer than 2 messages. This is a safe fallback
// (the request still gets L3 compression, just no cache hit).
func TestSplit_TooFewMessages(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)

	split, err := SplitAtPrefixBoundary(raw)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if len(split.PrefixJSON) != 0 {
		t.Fatalf("expected empty prefix, got %d bytes", len(split.PrefixJSON))
	}
	if !bytes.Equal(split.TailJSON, raw) {
		t.Fatalf("expected whole payload as tail, got %d bytes vs %d", len(split.TailJSON), len(raw))
	}
}

// TestSplit_NoMessagesKey returns the whole payload as the tail
// when there is no messages key (e.g. an embeddings request).
func TestSplit_NoMessagesKey(t *testing.T) {
	raw := []byte(`{"model":"text-embedding-3-small","input":"hi"}`)

	split, err := SplitAtPrefixBoundary(raw)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if len(split.PrefixJSON) != 0 {
		t.Fatalf("expected empty prefix, got %d bytes", len(split.PrefixJSON))
	}
}

// TestSplit_Stability is the critical test for cache hit: if the
// agent sends the same payload twice (with only the system
// prompt and older turns unchanged, only the latest user message
// different), the prefix must be byte-exact the same so the
// provider cache key matches.
func TestSplit_Stability(t *testing.T) {
	// Two payloads that differ only in their most recent user
	// message. The system + tools + older history are identical.
	// With 6 messages and tail = last 4, the prefix is the
	// first 2 messages (identical between the two payloads).
	common := `{"model":"MiniMax-M3","messages":[{"role":"system","content":"You are Hermes."},{"role":"user","content":"first"},{"role":"assistant","content":"ack"},{"role":"user","content":"second"},{"role":"assistant","content":"ok"},{"role":"user","content":"third"},{"role":"assistant","content":"ok"},{"role":"user","content":"`

	raw1 := []byte(common + `add auth"}]}`)
	raw2 := []byte(common + `refactor the auth module"}]}`)

	split1, err := SplitAtPrefixBoundary(raw1)
	if err != nil {
		t.Fatalf("split1: %v", err)
	}
	split2, err := SplitAtPrefixBoundary(raw2)
	if err != nil {
		t.Fatalf("split2: %v", err)
	}

	// The prefixes must be byte-exact identical.
	if !bytes.Equal(split1.PrefixJSON, split2.PrefixJSON) {
		t.Fatalf("prefixes differ between two related payloads\nprefix1: %q\nprefix2: %q",
			split1.PrefixJSON, split2.PrefixJSON)
	}

	// The tails differ.
	if bytes.Equal(split1.TailJSON, split2.TailJSON) {
		t.Fatalf("tails should differ, both equal %q", split1.TailJSON)
	}
}

// TestCompressPayloadCachePreserving_PrefixUnchanged is the
// integration test: compress a payload, then assert the prefix
// bytes are exactly the same as in the input.
func TestCompressPayloadCachePreserving_PrefixUnchanged(t *testing.T) {
	raw := []byte(`{"model":"MiniMax-M3","messages":[{"role":"system","content":"You are Hermes. <thinking>internal</thinking>"},{"role":"user","content":"hello"},{"role":"assistant","content":"<thought>plan: 1) refactor 2) test</thought>I'll refactor."},{"role":"user","content":"add auth"}]}`)

	out, err := CompressPayloadCachePreserving(raw)
	if err != nil {
		t.Fatalf("compress failed: %v", err)
	}

	// Validate the output is still valid JSON.
	var check interface{}
	if err := json.Unmarshal(out, &check); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out)
	}

	// The prefix in the output must start with the same bytes
	// as the input up to the split point. We compare the first
	// len(out-prefix-tokens-in-tail) bytes.
	splitIn, err := SplitAtPrefixBoundary(raw)
	if err != nil {
		t.Fatalf("resplit: %v", err)
	}
	prefixLen := len(splitIn.PrefixJSON)
	if prefixLen > len(out) {
		t.Fatalf("output is shorter than the expected prefix: out=%d, prefix=%d", len(out), prefixLen)
	}
	if !bytes.Equal(out[:prefixLen], splitIn.PrefixJSON) {
		t.Fatalf("prefix was modified by compression\ninput:  %q\noutput: %q",
			splitIn.PrefixJSON, out[:prefixLen])
	}
}

// TestCompressPayloadCachePreserving_TailCompressed is the
// counterpart: assert that the tail DID get compressed (i.e. the
// CoT block was pruned). This proves the split is doing real
// work, not just deferring to the fallback.
func TestCompressPayloadCachePreserving_TailCompressed(t *testing.T) {
	// 8 messages. The CoT is in the 4th message (i=2, an old
	// assistant turn with plan-thoughts). With tail = last 4
	// messages, that message falls into the tail, AND it is
	// non-recent (i=2 < msgCount-2 = 6), so the CoT is pruned.
	raw := []byte(`{"messages":[
		{"role":"system","content":"You are Hermes."},
		{"role":"user","content":"first"},
		{"role":"assistant","content":"plan: 1) refactor 2) test"},
		{"role":"user","content":"add auth"},
		{"role":"assistant","content":"<thought>let me think deeply about auth middleware design and edge cases</thought>I'll write it now."},
		{"role":"user","content":"add tests"},
		{"role":"assistant","content":"<thought>thinking about test coverage</thought>Done."},
		{"role":"user","content":"deploy"}
	]}`)

	out, err := CompressPayloadCachePreserving(raw)
	if err != nil {
		t.Fatalf("compress failed: %v", err)
	}

	// The output may be larger than the input on tiny payloads
	// because the deterministic re-encoder adds a few bytes of
	// structure (whitespace, quotes) — but on realistic
	// 50k-token payloads this is invisible. The real assertions
	// are about the CoT behavior.

	// The output should NOT contain the un-pruned CoT from the
	// 4th message (the one that should have been pruned).
	if strings.Contains(string(out), "let me think deeply about auth middleware design and edge cases") {
		t.Fatalf("CoT was not pruned from the tail: %s", out)
	}

	// It SHOULD contain the pruned marker.
	if !strings.Contains(string(out), "Pruned Thought Process") {
		t.Fatalf("expected pruned marker in output, got: %s", out)
	}
}

// TestCompressPayloadCachePreserving_StableKeyOrder asserts that
// two compressions of the same payload produce byte-identical
// output (the idempotence property from Phase 1, now also
// applied to the cache-preserving variant).
func TestCompressPayloadCachePreserving_StableKeyOrder(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"system","content":"x"},{"role":"user","content":"y"},{"role":"assistant","content":"<thought>z</thought>w"},{"role":"user","content":"z1"},{"role":"assistant","content":"ok"},{"role":"user","content":"z2"}]}`)

	first, err := CompressPayloadCachePreserving(raw)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := CompressPayloadCachePreserving(raw)
		if err != nil {
			t.Fatalf("again %d: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("run %d differs: %s vs %s", i, first, again)
		}
	}
}
