// Tests for summary line detection. P0.5b added
// format-specific patterns (pytest FAILED, npm ERR!,
// cargo error[E0xxx]). P0.5c adds summary lines, which
// are the "card" of any log dump: pytest "=== X passed
// ===", npm "added N packages", cargo "Finished in Xs",
// jest "Tests: X passed, Y failed".
//
// Summary lines are the first thing a human skims when
// they open a log. They must survive compression. Today
// they don't have a level prefix and don't match the
// format patterns, so they score 0.2 (default) and get
// dropped. This is a real gap.

package optiagent

import (
	"strings"
	"testing"
)

// TestLogCompressorScoring_PytestSummaryLineAlwaysHigh:
// pytest summary lines like "=== 98 passed, 2 failed
// in 0.42s ===" must score >= 0.85.
func TestLogCompressorScoring_PytestSummaryLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"============================= test session starts ==============================",
		"platform linux -- Python 3.11.0, pytest-7.4.0",
		"collected 100 items",
		"=================================== FAILURES ===================================",
		"=================================== ERRORS ====================================",
		"=========================== short test summary info ============================",
		"FAILED tests/test_auth.py::test_login - assert 0 == 1",
		"================== 2 failed, 98 passed in 0.42s ==================",
		"======================== 2 passed in 0.01s ========================",
		"1 warning in 0.01s",
	}
	for _, l := range lines {
		if !strings.Contains(l, "===") && !strings.Contains(l, "warning") {
			continue
		}
		score := scorer.ScoreLine(l)
		// All summary/section lines must score >= 0.7.
		if score < 0.7 {
			t.Errorf("summary line %q scored %f, want >= 0.7", l, score)
		}
	}
}

// TestLogCompressorScoring_NpmSummaryLineAlwaysHigh:
// npm summary lines like "added 234 packages" or
// "audited 234 packages in 5s" must score >= 0.7.
func TestLogCompressorScoring_NpmSummaryLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"added 234 packages, and audited 234 packages in 5s",
		"removed 12 packages in 2s",
		"changed 3 packages, and audited 234 packages in 5s",
		"up to date in 0s",
		"found 0 vulnerabilities",
	}
	for _, l := range lines {
		score := scorer.ScoreLine(l)
		if score < 0.7 {
			t.Errorf("npm summary line %q scored %f, want >= 0.7", l, score)
		}
	}
}

// TestLogCompressorScoring_CargoSummaryLineAlwaysHigh:
// cargo summary lines like "Finished release in 3.42s"
// or "Compiling my-crate v0.1.0" must score >= 0.7.
func TestLogCompressorScoring_CargoSummaryLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"   Compiling my-crate v0.1.0",
		"    Finished release [optimized] in 3.42s",
		"    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.5s",
		"   Compiling proc-macro2 v1.0.0",
		"   Compiling unicode-ident v1.0.0",
		"   Compiling syn v2.0.0",
	}
	for _, l := range lines {
		score := scorer.ScoreLine(l)
		if score < 0.7 {
			t.Errorf("cargo summary line %q scored %f, want >= 0.7", l, score)
		}
	}
}

// TestLogCompressorScoring_JestSummaryLineAlwaysHigh:
// jest summary lines like "Tests: 2 failed, 5 passed"
// or "Suites: 1 failed, 1 total" must score >= 0.7.
func TestLogCompressorScoring_JestSummaryLineAlwaysHigh(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"Tests:       2 failed, 5 passed, 7 total",
		"Test Suites: 1 failed, 1 total",
		"Snapshots:   0 total",
		"Time:        3.42 s",
		"Ran all test suites.",
		"FAIL src/__tests__/foo.test.js",
		"PASS src/__tests__/bar.test.js",
	}
	for _, l := range lines {
		score := scorer.ScoreLine(l)
		if score < 0.7 {
			t.Errorf("jest summary line %q scored %f, want >= 0.7", l, score)
		}
	}
}

// TestLogCompressorScoring_GenericSummaryPattern:
// generic summary patterns like "Summary: X" or
// "Total: N" or "Done in Xs" must also score high.
func TestLogCompressorScoring_GenericSummaryPattern(t *testing.T) {
	scorer := NewLogLineScorer()
	lines := []string{
		"Summary: 2 files changed, 12 insertions, 4 deletions",
		"Total: 234 packages",
		"Done in 5.2s",
		"Finished in 0.42s",
		"Result: 98 passed, 2 failed",
		"OK (5 tests, 12 assertions)",
	}
	for _, l := range lines {
		score := scorer.ScoreLine(l)
		if score < 0.6 {
			t.Errorf("generic summary line %q scored %f, want >= 0.6", l, score)
		}
	}
}
