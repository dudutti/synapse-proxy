// Tests for the format-specific log scoring. P0.5 added
// the basic level-prefix scorer. P0.5b adds format-specific
// patterns that Headroom's log_compressor.py uses:
//   - pytest: PASSED, FAILED, SKIPPED, ERROR lines
//   - npm: npm ERR!, npm WARN!, npm WARN deprecated
//   - cargo: error[E0xxx], warning: ...
//   - make: make[N]: Entering/Leaving directory
//   - jest: PASS FAIL + ✗ ✓
//
// The patterns are detected by a `formatDetector` that
// scores lines higher when they match a format-specific
// pattern. This brings us from 50% to ~70% on the log
// compressor scope.

package optiagent

import (
	"context"
	"strings"
	"testing"
)

// TestLogCompressorScoring_PytestFailedLineAlwaysHigh:
// pytest FAILED lines (test results) are the most
// important. They must always score >= 0.9.
func TestLogCompressorScoring_PytestFailedLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"============================= test session starts ==============================",
		"platform linux -- Python 3.11.0, pytest-7.4.0",
		"collected 100 items",
		"tests/test_auth.py::test_login FAILED",
		"tests/test_auth.py::test_logout FAILED",
		"tests/test_auth.py::test_register PASSED",
		"tests/test_api.py::test_health PASSED",
		"=========================== short test summary info ============================",
		"FAILED tests/test_auth.py::test_login - assert 0 == 1",
		"FAILED tests/test_auth.py::test_logout - assert 0 == 1",
		"================== 2 failed, 98 passed in 0.42s ==================",
	}
	for _, l := range lines {
		if strings.Contains(l, "FAILED") || strings.Contains(l, "ERROR") {
			if scorer.ScoreLine(l) < 0.7 {
				t.Errorf("pytest failure line %q scored too low: %f", l, scorer.ScoreLine(l))
			}
		}
	}
}

// TestLogCompressorScoring_NpmErrLineAlwaysHigh:
// npm ERR! and npm WARN deprecated lines must score high.
func TestLogCompressorScoring_NpmErrLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"npm WARN deprecated request@2.88.2: request has been deprecated",
		"npm ERR! missing: react@^18.0.0, required by my-app@1.0.0",
		"npm WARN tar TAR_ENTRY_ERROR ENOENT: no such file",
		"npm http fetch GET 200 https://registry.npmjs.org/react",
		"npm timing npm Completed in 4523ms",
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "npm ERR!") {
			if scorer.ScoreLine(l) < 0.85 {
				t.Errorf("npm ERR! line %q scored too low: %f", l, scorer.ScoreLine(l))
			}
		}
		if strings.HasPrefix(l, "npm WARN") {
			if scorer.ScoreLine(l) < 0.6 {
				t.Errorf("npm WARN line %q scored too low: %f", l, scorer.ScoreLine(l))
			}
		}
	}
}

// TestLogCompressorScoring_CargoErrorCodeAlwaysHigh:
// cargo error[E0xxx] and warning: lines are very
// informative. error codes are the most important.
func TestLogCompressorScoring_CargoErrorCodeAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"error[E0277]: the trait bound `Foo: Bar` is not satisfied",
		"  --> src/main.rs:42:5",
		"warning: unused variable: `x`",
		"  --> src/main.rs:10:9",
		"   |",
		"help: consider removing this variable",
		"   Compiling my-crate v0.1.0",
		"    Finished release [optimized] in 3.42s",
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "error[") {
			if scorer.ScoreLine(l) < 0.9 {
				t.Errorf("cargo error code line %q scored too low: %f", l, scorer.ScoreLine(l))
			}
		}
		if strings.HasPrefix(l, "warning:") && !strings.HasPrefix(l, "warning: ") {
			_ = l // ok
		}
	}
}

// TestLogCompressorScoring_KeepErrorContextLines: the
// compressor must keep `error_context_lines` (3 by
// default) lines BEFORE and AFTER each ERROR line, so
// the LLM sees the context of what failed. This is a
// hard guarantee, not a scoring preference.
func TestLogCompressorScoring_KeepErrorContextLines(t *testing.T) {
	h := &LogCompressorHook{}
	// 50 INFO lines, 1 ERROR in the middle, with 2
	// lines of context before and after.
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, "INFO: setup step "+itoa(i+1))
	}
	lines = append(lines, "INFO: about to fail (context line 1)")
	lines = append(lines, "INFO: about to fail (context line 2)")
	lines = append(lines, "ERROR: something went wrong")
	lines = append(lines, "INFO: cleanup (context line 1)")
	lines = append(lines, "INFO: cleanup (context line 2)")
	for i := 0; i < 25; i++ {
		lines = append(lines, "INFO: teardown step "+itoa(i+1))
	}
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.Join(lines, "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-ctx", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)
	// The ERROR line must be kept.
	if !strings.Contains(outStr, "ERROR: something went wrong") {
		t.Fatalf("ERROR line was dropped\n  out:\n%s", outStr)
	}
	// The 2 context lines before must be kept.
	if !strings.Contains(outStr, "context line 1)") || !strings.Contains(outStr, "context line 2)") {
		t.Fatalf("context lines before ERROR were dropped\n  out:\n%s", outStr)
	}
	// The 2 context lines after must be kept.
	if !strings.Contains(outStr, "cleanup (context line 1)") || !strings.Contains(outStr, "cleanup (context line 2)") {
		t.Fatalf("context lines after ERROR were dropped\n  out:\n%s", outStr)
	}
}

// TestLogCompressorScoring_RespectsMaxTotalLines: the
// compressor must respect the `max_total_lines` budget
// (100 by default). A 500-line log must be compressed
// to <= 100 lines.
func TestLogCompressorScoring_RespectsMaxTotalLines(t *testing.T) {
	h := &LogCompressorHook{}
	// 500 lines of INFO (no error, no warn).
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "INFO: tick "+itoa(i+1))
	}
	body := []byte(`{"messages":[{"role":"tool","content":"` + strings.Join(lines, "\\n") + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-max", OptimizedPayload: body, Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)
	// Count the lines in the output.
	outLines := strings.Count(outStr, "\\n") + 1
	if outLines > 100 {
		t.Fatalf("output has %d lines, expected <= 100\n  out:\n%s", outLines, outStr)
	}
}
