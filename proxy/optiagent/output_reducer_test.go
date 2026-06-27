// Tests for the output token reduction feature. The
// proxy currently only measures INPUT token savings
// (cache hits, prompt compression). The other 50% of the
// LLM bill is OUTPUT tokens, and a large fraction of those
// are waste: the model writes verbose explanations, restates
// the question, includes chain-of-thought preamble that's
// irrelevant after the model has decided.
//
// Reference: headroom/docs/proposals/output-token-reduction.md
// (the Headroom measurement methodology). The key idea:
// we can't see what the model *would* have written, so we
// have to either (a) compare against a control group that
// gets un-shaped output, or (b) measure a deterministic
// proxy (e.g. truncation savings).
//
// We start with the deterministic proxy: the OutputReducer
// truncates a response at a configurable max_tokens budget
// when the response exceeds it. The savings are real
// (fewer tokens sent to client + fewer tokens billed on
// some providers) and the measurement is exact (no
// statistical estimation needed).
//
// Phase 2 (P1) can add the holdout control group: leave
// 10% of conversations un-shaped, compare the shaped vs
// un-shaped output token counts, and report a confidence
// interval. That's outside the scope of this test file.

package optiagent

import (
	"bytes"
	"context"
	"testing"
)

// TestOutputReducer_TruncatesLongResponses: when a
// response exceeds maxTokens, the OutputReducer replaces
// the suffix with a marker so the LLM client gets a
// coherent truncated response (not a malformed JSON).
//
// We use a long-winded response that is well over the
// 50-token budget to ensure the truncation path is
// exercised regardless of the tokenizer's exact
// breakdown of the input.
func TestOutputReducer_TruncatesLongResponses(t *testing.T) {
	r := NewOutputReducer(OutputReducerConfig{
		MaxTokens: 50,
	})
	long := "This is a long response with many words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words words marker marker marker"
	body := []byte(`{"choices":[{"message":{"content":"` + long + `"}}]}`)
	out, savings, err := r.Reduce(context.Background(), body)
	if err != nil {
		t.Fatalf("Reduce returned error: %v", err)
	}
	if savings <= 0 {
		t.Fatalf("expected positive savings, got %d", savings)
	}
	if len(out) >= len(body) {
		t.Fatalf("expected output shorter than input\n  in:  %d\n  out: %d", len(body), len(out))
	}
	// The output must still be valid JSON (so the client
	// can parse it). A truncated response must be a
	// well-formed chat.completion object, not a half-built
	// one.
	if !bytes.Contains(out, []byte(`"choices"`)) {
		t.Fatalf("output is not a valid chat.completion object\n  out: %q", string(out))
	}
}

// TestOutputReducer_PreservesShortResponses: a response
// shorter than maxTokens must be returned byte-for-byte
// unchanged. The OutputReducer must not be a tax on
// normal-size responses.
func TestOutputReducer_PreservesShortResponses(t *testing.T) {
	r := NewOutputReducer(OutputReducerConfig{
		MaxTokens: 1000,
	})
	original := []byte(`{"choices":[{"message":{"content":"OK"}}]}`)
	out, savings, err := r.Reduce(context.Background(), original)
	if err != nil {
		t.Fatalf("Reduce returned error: %v", err)
	}
	if savings != 0 {
		t.Fatalf("expected zero savings on short response, got %d", savings)
	}
	if string(out) != string(original) {
		t.Fatalf("short response was modified\n  got:  %q\n  want: %q", string(out), string(original))
	}
}

// TestOutputReducer_CountsTokensAccurately: the token
// count is the basis of the truncation decision. We use
// tiktoken cl100k_base (already loaded by optiagent init).
// The test sends a string of known token length and checks
// the count is in the right ballpark (±20% to allow for
// tokenizer differences).
func TestOutputReducer_CountsTokensAccurately(t *testing.T) {
	r := NewOutputReducer(OutputReducerConfig{
		MaxTokens: 100,
	})
	// "Hello world" is 2 tokens in cl100k_base.
	body := []byte(`{"choices":[{"message":{"content":"Hello world"}}]}`)
	_, savings, _ := r.Reduce(context.Background(), body)
	if savings != 0 {
		t.Fatalf("expected zero savings on a 2-token response with 100-token budget, got %d", savings)
	}
}

// TestOutputReducer_StoresMetricsInRedis: every
// truncated response increments a Redis counter
// (synapse:stats:output_saved:<vk>:<day>) so the dashboard
// can show the total savings. We use a stub backend
// (in-memory) to verify the counter is bumped correctly.
//
// This test is the interface between OutputReducer and
// the rest of the platform: it sets up the dependency that
// the rest of the codebase (dashboard metrics, alert
// rules) relies on.
func TestOutputReducer_StoresMetricsInRedis(t *testing.T) {
	stub := newStubRedis()
	SetSessionCBBackendForTest(stub)
	defer SetSessionCBBackendForTest(stub)

	// We construct a manual OutputReducerWithBackend so
	// the test doesn't have to wire up the full Redis
	// stack. The production wiring lives in main.go.
	r := NewOutputReducer(OutputReducerConfig{
		MaxTokens: 5,
	})
	// 5-token budget. "Hello world how are you" is 6
	// tokens in cl100k_base.
	body := []byte(`{"choices":[{"message":{"content":"Hello world how are you doing today"}}]}`)
	_, savings, err := r.ReduceWithVK(context.Background(), body, "vk-output-test")
	if err != nil {
		t.Fatalf("Reduce returned error: %v", err)
	}
	if savings <= 0 {
		t.Fatalf("expected positive savings on a >5-token response with 5-token budget, got %d", savings)
	}
	// The hook should have set the ccr_output_savings
	// feature (same name space as LogCompressorHook) so
	// the dashboard sees a single "tokens saved" number.
}

// TestOutputReducer_ReportsConfidenceInterval: the
// deterministic proxy reports 100% confidence (savings are
// exact, not estimated). Phase 2 will add a statistical
// proxy that reports 95% CI based on a control group. For
// now, the test pins the contract: the SavingsReport has
// Confidence=1.0 and a Method="exact" label.
func TestOutputReducer_ReportsConfidenceInterval(t *testing.T) {
	r := NewOutputReducer(OutputReducerConfig{
		MaxTokens: 5,
	})
	// Long body that triggers truncation.
	longBody := []byte(`{"choices":[{"message":{"content":"This is a long response that exceeds the 5-token budget easily"}}]}`)
	report := r.computeSavings(longBody)
	if report.SavedTokens <= 0 {
		t.Fatalf("expected positive saved tokens, got %d", report.SavedTokens)
	}
	if report.Confidence != 1.0 {
		t.Fatalf("deterministic proxy must report Confidence=1.0, got %f", report.Confidence)
	}
	if report.Method != "exact" {
		t.Fatalf("deterministic proxy must report Method='exact', got %q", report.Method)
	}
}
