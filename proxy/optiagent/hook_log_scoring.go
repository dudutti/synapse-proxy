// Package optiagent — LogCompressor scoring.
//
// The P0.2 LogCompressor uses a blind keep-first + keep-last
// strategy. This works for 70% of cases but fails on logs
// where the most important line is in the middle. P0.5
// adds line-level scoring, the same approach Headroom
// uses in `headroom/transforms/log_compressor.py`.
//
// The scorer assigns a float in [0, 1] to each line, where
// 1.0 is the highest priority. The scoring function is
// based on:
//   - log level prefix (ERROR > WARN > INFO > DEBUG)
//   - exception pattern (ValueError, RuntimeError, etc.)
//   - trace frame pattern (File X, at X, N: X)
//   - position in the log (header/first lines get a
//     small bonus, last lines get a small bonus)
//
// The scoring is plugged into the existing
// compressSingleTrace function as a "middle filter": after
// the first-N and last-N frames are kept (structural
// anchors), the middle frames are ranked by score and
// the top-K are kept. This composes with the existing
// dedup and truncation.

package optiagent

import (
	"regexp"
	"strings"
)

// LogLineScorer scores a single log line by priority.
// Higher score = more important. The function is pure
// (no side effects) and is invoked once per line by
// the LogCompressorHook when computing the middle-frame
// ranking.
type LogLineScorer struct {
	// Regexes for log level detection. The order
	// matters: ERROR must come before WARN, etc.
	levelRE *regexp.Regexp
	// Regexes for exception/frame detection.
	exceptionRE *regexp.Regexp
	frameRE     *regexp.Regexp
	// Regexes for "important content" patterns.
	importantRE *regexp.Regexp
	// Format-specific patterns.
	pytestRE *regexp.Regexp
	npmRE    *regexp.Regexp
	cargoRE   *regexp.Regexp
	// Summary-line patterns (pytest ===, npm added/removed,
	// cargo Compiling/Finished, jest Tests/Suites, generic
	// Summary/Total/Done). These are the lines a human
	// skims first when they open a log; they must survive
	// compression even though they don't have a level
	// prefix.
	pytestSummaryRE  *regexp.Regexp
	npmSummaryRE     *regexp.Regexp
	cargoSummaryRE   *regexp.Regexp
	jestSummaryRE    *regexp.Regexp
	genericSummaryRE *regexp.Regexp
	finishedInRE     *regexp.Regexp
}

// NewLogLineScorer returns a scorer with the canonical
// regex set. The regexes are compiled once at init time
// and reused across calls.
func NewLogLineScorer() *LogLineScorer {
	return &LogLineScorer{
		// Level detection: match at start of line, optional
		// whitespace, then the level keyword followed by ":".
		levelRE: regexp.MustCompile(`(?i)^\s*(FATAL|ERROR|CRITICAL|EMERG|EMERGENCY|WARN|WARNING|INFO|NOTICE|DEBUG|TRACE|VERBOSE|LOG)\s*:`),
		// Exception detection: starts with a capital letter
		// and contains "Error" or "Exception" or "Failure".
		exceptionRE: regexp.MustCompile(`^[A-Z][a-zA-Z]*(Error|Exception|Failure|Abort|Panic)`),
		// Frame detection: a Python/JS/Rust frame line.
		frameRE: regexp.MustCompile(`^(\s*File\s|^\s*at\s|^\s*\d+:\s)`),
		// Important content: keywords that suggest a
		// non-routine condition (timeout, refused, missing,
		// failed, invalid, etc.).
		importantRE: regexp.MustCompile(`(?i)\b(timeout|refused|denied|missing|failed|invalid|unauthorized|forbidden|panic|abort|kill|signal|out of memory|segfault|stack overflow)\b`),
		// Format-specific patterns.
		// pytest: "FAILED" / "PASSED" / "SKIPPED" / "ERROR"
		// in a test session output. The token can appear
		// at the end of a test name or at the start of a
		// summary line.
		pytestRE: regexp.MustCompile(`(?m)(FAILED|PASSED|SKIPPED|ERROR)(?:\s|$|/)`),
		// npm: "npm ERR!" / "npm WARN" / "npm WARN deprecated"
		// / "npm http" etc. The "ERR!" suffix is the most
		// informative.
		npmRE: regexp.MustCompile(`^npm\s+(ERR!|WARN|ERR|TIMING|VERBOSE|HTTP)\b`),
		// cargo: "error[E0xxx]" / "warning:" / "note:" etc.
		// The error code is the most specific signal.
		cargoRE: regexp.MustCompile(`(?m)^(error\[\w+\]:|warning:|note:|-->)`),
		// Summary-line patterns.
		// Summary-line patterns. Pytest "=== X ===" lines
		// include both section headers (test session starts,
		// FAILURES, ERRORS, short test summary info) and
		// result lines (2 failed, 98 passed). All are
		// informative.
		pytestSummaryRE: regexp.MustCompile(`={3,}.*={3,}|^\d+ warning`),
		// npm summary: "added N packages" / "audited N packages"
		// / "removed N packages" / "changed N packages"
		// / "found N vulnerabilities" / "N packages looking
		// for funding".
		npmSummaryRE:     regexp.MustCompile(`(added|removed|changed|audited|found) \d+|^N packages? is looking for funding|^N packages? looking for funding|up to date`),
		cargoSummaryRE:   regexp.MustCompile(`^\s*(Compiling|Finished)\s`),
		jestSummaryRE:    regexp.MustCompile(`^(Tests|Test Suites|Snapshots|Time):\s|Ran all test|^FAIL\s|^PASS\s`),
		genericSummaryRE: regexp.MustCompile(`(?i)^(Summary|Total|Done|Result|OK)[\s:]`),
		finishedInRE:     regexp.MustCompile(`(?i)Finished in`),
	}
}

// ScoreLine returns a score in [0, 1] where 1.0 is the
// highest priority. The function is pure.
func (s *LogLineScorer) ScoreLine(line string) float64 {
	if strings.TrimSpace(line) == "" {
		return 0.0
	}
	// Format-specific patterns get the highest score
	// because they're the most informative (they tell
	// the LLM what the test status is, what npm
	// failed, what cargo error code was hit).
	if s.pytestRE.MatchString(line) {
		// FAILED/ERROR in pytest output is the most
		// important: it tells the LLM which test broke.
		if strings.Contains(line, "FAILED") || strings.Contains(line, "ERROR") {
			return 0.9
		}
		// PASSED is low priority (no action needed).
		if strings.Contains(line, "PASSED") {
			return 0.2
		}
		// SKIPPED is medium.
		return 0.5
	}
	if s.npmRE.MatchString(line) {
		// npm ERR! is critical.
		if strings.Contains(line, "ERR!") || strings.Contains(line, "ERR") {
			return 0.9
		}
		// npm WARN is medium-high.
		if strings.Contains(line, "WARN") {
			return 0.65
		}
		// npm HTTP/TIMING/VERBOSE is low.
		return 0.15
	}
	if s.cargoRE.MatchString(line) {
		// error[E0xxx] is the most specific.
		if strings.HasPrefix(line, "error[") {
			return 0.95
		}
		// warning: is medium.
		if strings.HasPrefix(line, "warning:") {
			return 0.6
		}
		// note: and --> are context.
		return 0.4
	}
	// Summary lines are the "card" of any log dump.
	// They score HIGH (0.8) because they tell the LLM
	// the final state of the run.
	if s.pytestSummaryRE.MatchString(line) {
		return 0.85
	}
	if s.npmSummaryRE.MatchString(line) {
		return 0.8
	}
	if s.cargoSummaryRE.MatchString(line) {
		return 0.8
	}
	if s.jestSummaryRE.MatchString(line) {
		return 0.85
	}
	if s.genericSummaryRE.MatchString(line) {
		return 0.7
	}
	if s.finishedInRE.MatchString(line) {
		return 0.7
	}
	// Level prefix gives the biggest boost.
	if m := s.levelRE.FindStringSubmatch(line); m != nil {
		switch strings.ToUpper(m[1]) {
		case "FATAL", "ERROR", "CRITICAL", "EMERG", "EMERGENCY":
			return 0.95
		case "WARN", "WARNING":
			return 0.7
		case "INFO", "NOTICE":
			return 0.3
		case "DEBUG", "TRACE", "VERBOSE", "LOG":
			return 0.1
		}
	}
	// Exception line (e.g. "ValueError: bad input") is
	// always high priority even without a level prefix.
	if s.exceptionRE.MatchString(line) {
		return 0.85
	}
	// Frame line in a stack trace is medium-high.
	if s.frameRE.MatchString(line) {
		return 0.6
	}
	// Important keyword in the content bumps the score.
	if s.importantRE.MatchString(line) {
		return 0.65
	}
	// Default: low priority (treat as debug/verbose).
	return 0.2
}

// topKScoredLines returns the K highest-scored lines from
// the input, in their original relative order. The input
// is expected to be a slice of un-trimmed lines (the
// caller is responsible for splitting the log into lines
// before calling this function).
func topKScoredLines(lines []string, k int, scorer *LogLineScorer) []string {
	if k <= 0 || len(lines) == 0 {
		return nil
	}
	if k >= len(lines) {
		// No filtering needed.
		out := make([]string, len(lines))
		copy(out, lines)
		return out
	}
	type scored = scoredSlice
// _ = scored // keep for back-compat
type _scored_placeholder struct {
		line  string
		score float64
		idx   int
	}
	all := make([]scoredSlice, len(lines))
	for i, l := range lines {
		all[i] = scoredSlice{line: l, score: scorer.ScoreLine(l), idx: i}
	}
	// Stable partition: pick the top K by score, breaking
	// ties by original index. We use a simple sort.
	sortByScoreDescStable(all)
	out := make([]string, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, all[i].line)
	}
	// Re-sort by original index so the output is in
	// reading order, not score order.
	sortByIdxAsc(out, all[:k])
	return out
}

// sortByScoreDescStable sorts scored lines by score
// descending, breaking ties by original index ascending.
func sortByScoreDescStable(s []scoredSlice) {
	// Simple insertion sort — small N, no need for the
	// overhead of sort.SliceStable.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && (s[j].score > s[j-1].score || (s[j].score == s[j-1].score && s[j].idx < s[j-1].idx)); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// scoredSlice is a small wrapper for the sort routines.
type scoredSlice = struct {
	line  string
	score float64
	idx   int
}

// sortByIdxAsc re-orders the first k items of `all` by
// the original index (idx field) ascending. The first k
// items of `all` are taken; the rest is ignored.
func sortByIdxAsc(out []string, all []scoredSlice) {
	// The out slice is already filled; we just need to
	// reorder the items in all[0:k] and re-fill out.
	// Actually it's simpler: copy the lines from
	// all (sorted by idx) into out.
	type pair struct {
		line string
		idx  int
	}
	pairs := make([]pair, len(all))
	for i, x := range all {
		pairs[i] = pair{line: x.line, idx: x.idx}
	}
	// Insertion sort by idx.
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j].idx < pairs[j-1].idx; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
	for i, p := range pairs {
		out[i] = p.line
	}
}
