// Tests for the warning dedup by keyword. P0.5b added
// the dedup of ADJACENT identical lines (a tight loop
// emitting the same frame N times). P0.5c adds the
// dedup of NON-adjacent warnings that share the same
// keyword (e.g. "WARN: connection refused" appearing
// 5 times in a log with different ports).
//
// The Headroom equivalent is dedupe_warnings: it
// groups warnings by the "key" (a stable signature
// after normalising digits/paths), keeps the first
// one, and adds a count marker. We implement a
// simpler version: group by the first keyword
// (timeout, refused, missing, etc.), keep the first
// occurrence, replace subsequent occurrences with a
// count marker.

package optiagent

import (
	"context"
	"strings"
	"testing"
)

// TestLogCompressorScoring_DedupsNonAdjacentWarnings:
// 5 WARNINGs with the same keyword (timeout) but
// different details (port, duration, etc.) must be
// deduped to 1 + count marker.
func TestLogCompressorScoring_DedupsNonAdjacentWarnings(t *testing.T) {
	h := &LogCompressorHook{}
	lines := []string{
		"INFO: starting up",
		"WARN: timeout fetching https://api.example.com/orders (5s)",
		"INFO: processing",
		"WARN: timeout fetching https://api.example.com/users (3s)",
		"INFO: processing",
		"WARN: timeout fetching https://api.example.com/items (7s)",
		"INFO: processing",
		"WARN: timeout fetching https://api.example.com/cart (4s)",
		"INFO: processing",
		"WARN: timeout fetching https://api.example.com/checkout (6s)",
		"INFO: retrying",
		"INFO: backing off",
		"INFO: trying again",
		"INFO: still failing",
		"INFO: rate limit hit",
		"INFO: sleeping",
		"INFO: backing off more",
		"INFO: retrying harder",
		"INFO: still rate limited",
		"INFO: giving up temporarily",
		"INFO: will retry in 30s",
		"INFO: waiting",
		"INFO: still waiting",
		"INFO: done for now",
	}
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.Join(lines, "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-warn", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)
	// The first WARN: timeout must be kept.
	if !strings.Contains(outStr, "WARN: timeout") {
		t.Fatalf("first WARN was dropped\n  out:\n%s", outStr)
	}
	// A count marker must be present.
	if !strings.Contains(outStr, "identical") {
		t.Fatalf("dedup count marker missing\n  out:\n%s", outStr)
	}
	// The total number of WARN: timeout occurrences in
	// the output must be <= 2 (1 original + 1 marker).
	warnCount := strings.Count(outStr, "WARN: timeout")
	if warnCount > 2 {
		t.Fatalf("expected <= 2 WARN: timeout, got %d\n  out:\n%s", warnCount, outStr)
	}
}

// TestLogCompressorScoring_KeepsDifferentWarnings:
// 3 WARNINGs with different keywords (timeout, refused,
// missing) must each be kept — dedup is per-keyword.
func TestLogCompressorScoring_KeepsDifferentWarnings(t *testing.T) {
	h := &LogCompressorHook{}
	lines := []string{
		"INFO: starting",
		"WARN: timeout fetching orders (5s)",
		"WARN: refused connecting to db (port 5432)",
		"WARN: missing config file /etc/app/config.yaml",
		"INFO: done",
	}
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.Join(lines, "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-warn-diff", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)
	// All 3 different warnings must be kept.
	if !strings.Contains(outStr, "WARN: timeout") {
		t.Fatalf("WARN: timeout was dropped\n  out:\n%s", outStr)
	}
	if !strings.Contains(outStr, "WARN: refused") {
		t.Fatalf("WARN: refused was dropped\n  out:\n%s", outStr)
	}
	if !strings.Contains(outStr, "WARN: missing") {
		t.Fatalf("WARN: missing was dropped\n  out:\n%s", outStr)
	}
}
