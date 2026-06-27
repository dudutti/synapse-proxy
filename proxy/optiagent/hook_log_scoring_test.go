// Tests for the LogCompressor scoring feature. The current
// LogCompressor (P0.2) uses a blind keep-first + keep-last
// strategy: it always keeps the first 3 and last 3 frames
// of a stack trace, regardless of which frames are most
// informative. This works for 70% of cases but fails on
// traces where the most important frame is in the middle
// (e.g. a tight loop where the Nth iteration is the one
// that finally fails).
//
// Headroom's approach (log_compressor.py) is line-by-line
// scoring: each line gets a score based on (level, content
// type, position), and the compressor keeps the top-K
// scored lines. This is the approach we adopt in P0.5.
//
// We add scoring WITHOUT removing the existing first/last
// strategy. The two strategies compose:
//   - The scoring filter keeps high-priority lines from the
//     middle (e.g. WARN, FAIL, ERROR, exception lines).
//   - The first/last strategy keeps the structural anchors
//     (header, first frame, last frame, error message).
//   - The output is: header + first/last + scored_middle
//     + dedup.

package optiagent

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// TestLogCompressorScoring_KeepsHighPriorityLines:
// a line with level=ERROR and a specific exception name
// must be kept even if it's not in the first/last window.
func TestLogCompressorScoring_KeepsHighPriorityLines(t *testing.T) {
	h := &LogCompressorHook{}
	// 20-line log with one ERROR in the middle.
	lines := []string{
		"INFO: starting up",
		"INFO: loading config",
		"INFO: connecting to db",
		"INFO: ready",
		"DEBUG: heartbeat",
		"DEBUG: processing req 1",
		"DEBUG: processing req 2",
		"ERROR: connection refused at db.connect() (port=5432)",
		"DEBUG: retrying",
		"DEBUG: processing req 3",
		"DEBUG: processing req 4",
		"DEBUG: processing req 5",
		"DEBUG: processing req 6",
		"DEBUG: processing req 7",
		"DEBUG: processing req 8",
		"INFO: shutting down",
		"INFO: cleanup",
		"INFO: done",
		"DEBUG: post-cleanup",
		"INFO: exit",
	}
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.Join(lines, "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-score", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)
	// The ERROR line must be kept (scoring should pull it in).
	if !strings.Contains(outStr, "ERROR: connection refused") {
		t.Fatalf("high-priority ERROR line was dropped\n  out:\n%s", outStr)
	}
	// The first line (INFO: starting up) should be kept
	// (first-N strategy).
	if !strings.Contains(outStr, "INFO: starting up") {
		t.Fatalf("first line was dropped\n  out:\n%s", outStr)
	}
	// The last line (INFO: exit) should be kept (last-N).
	if !strings.Contains(outStr, "INFO: exit") {
		t.Fatalf("last line was dropped\n  out:\n%s", outStr)
	}
}

// TestLogCompressorScoring_DropsLowPriorityLines:
// a line with level=DEBUG must be dropped if there's
// pressure on the budget. This is the savings story:
// the LLM sees fewer lines, the agent still has the
// important ones.
func TestLogCompressorScoring_DropsLowPriorityLines(t *testing.T) {
	h := &LogCompressorHook{}
	// A 200-line log of DEBUG messages, with 2 WARNs.
	var lines []string
	lines = append(lines, "INFO: starting up")
	lines = append(lines, "INFO: ready")
	for i := 0; i < 200; i++ {
		lines = append(lines, "DEBUG: heartbeat #"+itoa(i+1))
	}
	lines = append(lines, "WARN: slow query 1.2s")
	for i := 0; i < 100; i++ {
		lines = append(lines, "DEBUG: tick #"+itoa(i+1))
	}
	lines = append(lines, "WARN: slow query 2.4s")
	for i := 0; i < 50; i++ {
		lines = append(lines, "DEBUG: tick #"+itoa(i+1))
	}
	lines = append(lines, "INFO: done")
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.Join(lines, "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-low", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)
	// The WARN lines should be kept.
	if !strings.Contains(outStr, "WARN: slow query 1.2s") {
		t.Fatalf("WARN line was dropped (scoring failed)\n  out:\n%s", outStr)
	}
	if !strings.Contains(outStr, "WARN: slow query 2.4s") {
		t.Fatalf("WARN line was dropped (scoring failed)\n  out:\n%s", outStr)
	}
	// The DEBUG lines should be heavily compressed.
	debugCount := strings.Count(outStr, "DEBUG:")
	if debugCount > 20 {
		t.Fatalf("expected DEBUG lines to be heavily compressed, got %d occurrences\n  out:\n%s", debugCount, outStr)
	}
}

// TestLogCompressorScoring_ScoreLineFunction: the scoring
// function is pure and can be unit-tested in isolation.
// It returns a float in [0, 1] where 1.0 is the highest
// priority.
func TestLogCompressorScoring_ScoreLineFunction(t *testing.T) {
	scorer := NewLogLineScorer()
	cases := []struct {
		line     string
		minScore float64
		maxScore float64
	}{
		{"ERROR: connection refused", 0.9, 1.0},
		{"FATAL: out of memory", 0.9, 1.0},
		{"WARN: slow query 1.2s", 0.5, 0.8},
		{"INFO: starting up", 0.2, 0.5},
		{"DEBUG: heartbeat", 0.0, 0.2},
		{"Traceback (most recent call last):", 0.0, 0.3},
		{"  File /app/x.py, line 42, in handle", 0.5, 0.8},
		{"ValueError: bad input", 0.7, 0.9},
		{"random prose line", 0.0, 0.3},
	}
	for _, c := range cases {
		score := scorer.ScoreLine(c.line)
		if score < c.minScore || score > c.maxScore {
			t.Errorf("ScoreLine(%q) = %f, want in [%f, %f]", c.line, score, c.minScore, c.maxScore)
		}
	}
}

// TestLogCompressorScoring_TopKKeepsHighestScored: the
// topK function should keep the K highest-scored lines
// from the input. We verify by ranking 10 lines by
// expected score and checking the output order.
func TestLogCompressorScoring_TopKKeepsHighestScored(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"DEBUG: tick 1", // 0.1
		"INFO: starting", // 0.3
		"ERROR: bad",     // 0.95
		"DEBUG: tick 2",  // 0.1
		"WARN: slow",     // 0.7
		"DEBUG: tick 3",  // 0.1
		"ERROR: oom",     // 0.95
		"DEBUG: tick 4",  // 0.1
		"INFO: ready",    // 0.3
		"FATAL: crash",   // 0.98
	}
	keep := topKScoredLines(lines, 3, scorer)
	if len(keep) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(keep), keep)
	}
	// FATAL and 2x ERROR should be the top 3.
	if !strings.Contains(strings.Join(keep, "\n"), "FATAL: crash") {
		t.Fatalf("FATAL line should be in top 3, got: %v", keep)
	}
	if !strings.Contains(strings.Join(keep, "\n"), "ERROR: bad") {
		t.Fatalf("ERROR line should be in top 3, got: %v", keep)
	}
	if !strings.Contains(strings.Join(keep, "\n"), "ERROR: oom") {
		t.Fatalf("ERROR line should be in top 3, got: %v", keep)
	}
	// And no DEBUG lines.
	for _, l := range keep {
		if strings.HasPrefix(l, "DEBUG:") {
			t.Errorf("DEBUG line %q should not be in top 3", l)
		}
	}
}

// TestLogCompressorScoring_ExceptionLineAlwaysHigh:
// the actual exception line (e.g. "ValueError: bad
// input", "RuntimeError: agent failed") must always get
// a high score, even if the line doesn't have a level
// prefix. These lines are the most important in any
// stack trace.
func TestLogCompressorScoring_ExceptionLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	exceptionLines := []string{
		"ValueError: bad input",
		"RuntimeError: agent failed",
		"KeyError: 'missing_key'",
		"TypeError: cannot read property 'x' of undefined",
		"ConnectionError: ECONNREFUSED",
		"AssertionError: expected True, got False",
		"  File /app/x.py, line 42, in handle", // frame line, also important
	}
	excRE := regexp.MustCompile(`^(\s*)([A-Z][a-zA-Z]*(Error|Exception|Failure)|File\s|at\s|\d+:\s.*<unknown>|Traceback)`)
	_ = excRE // keep import alive; used by the scorer implementation
	for _, l := range exceptionLines {
		score := scorer.ScoreLine(l)
		// Frame lines (start with "  File " or "    at "
		// or "   N:") score medium-high (>= 0.5).
		// Exception/Error/Failure lines (start with a
		// capital letter + Error/Exception/Failure)
		// score critical (>= 0.7).
		isFrame := strings.HasPrefix(l, "  File") || strings.HasPrefix(l, "    at") || strings.HasPrefix(l, "   ")
		if isFrame {
			if score < 0.5 {
				t.Errorf("frame line %q scored %f, want >= 0.5", l, score)
			}
		} else {
			if score < 0.7 {
				t.Errorf("exception line %q scored %f, want >= 0.7", l, score)
			}
		}
	}
}
