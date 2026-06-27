// Package optiagent — LogCompressorHook.
//
// Tool outputs frequently contain stack traces (Python's
// "Traceback (most recent call last):", JavaScript's "Error:
// ...\n    at <func> (<file>:<line>:<col>)", Rust's "thread
// 'main' panicked at ..."). Sending the full trace to the
// LLM is wasteful: the middle frames (the framework's
// own code: middleware, router, stdlib internals) are
// almost always noise, and the agent only needs the
// first few frames (where the request entered the
// application) and the last few frames (where the error
// propagated to the top of the stack).
//
// This hook truncates stack traces by:
//   1. Keep the first 3 frames so the LLM sees the origin
//   2. Keep the last 3 frames so the LLM sees where the
//      error surfaced
//   3. Drop the middle frames, replacing them with a
//      "... N middle frames dropped ..." marker
//   4. Dedupe adjacent identical lines (a tight loop
//      emitting the same frame N times is common)
//   5. Preserve chained exceptions ("raise X from Y")
//   6. Detect per-language trace headers so we don't
//      compress non-trace content (a JSON blob in a tool
//      output looks nothing like a Python traceback)
//
// Reference: headroom/crates/headroom-core/src/transforms/
// log_compressor.rs (the Rust port).
package optiagent

import (
	"context"
	"log"
	"regexp"
	"strings"

	"synapse-proxy/internal/metrics"
	"synapse-proxy/internal/utils"
)
// debugMin returns the smaller of two ints. Local
// helper to avoid pulling in the min builtin.
func debugMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LogCompressorHook is a BeforeRequest hook that truncates
// stack traces in tool outputs.
type LogCompressorHook struct {
	// compressionStore is the store for original
	// content. If nil, the global store is used.
	compressionStore CompressionStore
}

// SetCompressionStore sets the compression store for
// this hook. Used in tests to inject an in-memory store.
func (h *LogCompressorHook) SetCompressionStore(s CompressionStore) {
	h.compressionStore = s
}

// getCompressionStore returns the hook's store, or the
// global store if none is configured.
func (h *LogCompressorHook) getCompressionStore() CompressionStore {
	if h.compressionStore != nil {
		return h.compressionStore
	}
	return GetGlobalCompressionStore()
}

// Name returns the hook name used in metrics and log lines.
func (h *LogCompressorHook) Name() string { return "log_compressor" }

// Priority places LogCompressor between CacheAligner (700)
// and CCR Retrieve (750). LogCompressor runs AFTER
// CacheAligner (read-only) and BEFORE CCR Retrieve (uses
// post-compression payload as cache key).
func (h *LogCompressorHook) Priority() int { return 770 }

// keepFirstFrames and keepLastFrames control the
// truncation strategy. 3 + 3 = 6 frames kept out of
// potentially 50+ in a deep stack trace.
const (
	keepFirstFrames = 3
	keepLastFrames  = 3
	dedupRunMin     = 3
)

// Regexes for trace detection.
var (
	// pythonTraceStart matches "Traceback (most recent
	// call last):" — the opening of every Python
	// traceback.
	pythonTraceStart = regexp.MustCompile(`(?m)^Traceback \(most recent call last\):`)

	// jsTraceStart matches "Error: " — the opening of
	// every Node.js / V8 error.
	jsTraceStart = regexp.MustCompile(`(?m)^Error: `)

	// rustPanicStart matches "thread '...' panicked at
	// ..." — the opening of every Rust panic.
	rustPanicStart = regexp.MustCompile(`(?m)^thread '.*' panicked at `)

	// traceFrameLine matches a single frame line in any
	// of the three languages, plus the Rust "<unknown>"
	// placeholder. The pattern is intentionally broad:
	// any line that looks like a stack frame, including
	// indented continuation lines (e.g. Python's
	// "    return process(req)" body).
	traceFrameLine = regexp.MustCompile(`(?m)^[ \t]*(?:File|at|\d+:|\S+\s*\(|.*\)\s*$|\S+\s*=|<unknown>)`)

	// nonFrameContextLine matches lines that are not
	// frames but belong to the trace structure (not to a
	// different trace's chaining marker). Currently just
	// Rust's "stack backtrace:" header.
	nonFrameContextLine = regexp.MustCompile(`(?m)^[ \t]*stack backtrace:`)

	// chainedExceptionMarker matches Python's
	// "During handling of the above exception, another
	// exception occurred:" line, used to split chained
	// exceptions into separate traces.
	chainedExceptionMarker = regexp.MustCompile(`(?m)^During handling of the above exception,?`)
)

// BeforeRequest runs the trace compressor. It scans the
// payload for stack traces (per-language detection) and
// truncates each one to keepFirst+keepLast frames with a
// drop marker in the middle. Sets the
// log_compressor_savings feature to the byte delta.
func (h *LogCompressorHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx == nil || len(hctx.OptimizedPayload) == 0 {
		return hctx.OptimizedPayload, nil
	}
	payload := hctx.OptimizedPayload
	// Ensure ccr_compression_store_saved starts at 0.
	if hctx.GetFeature("ccr_compression_store_saved") == nil {
		hctx.SetFeature("ccr_compression_store_saved", 0)
	}
	out := h.compressAllTraces(payload)
	if len(out) >= len(payload) {
		// Try structured log compression (different path
		// from stack-trace compression).
		out = h.compressStructuredLogs(payload)
		if len(out) >= len(payload) {
			return hctx.OptimizedPayload, nil
		}
	}
	saved := len(payload) - len(out)
	hctx.SetFeature("log_compressor_savings", saved)
	log.Printf("[%s] saved %d bytes vk=%s", h.Name(), saved, hctx.VK)
	// P1.5 DASHBOARD FIRST: bump the per-hook metrics so
	// the dashboard shows the savings. We track bytes
	// (network bandwidth) and tokens (real cost unit)
	// separately. The metrics are also persisted to
	// Redis so they survive proxy restarts.
	metrics.RecordLogCompressor(saved)
	metrics.RecordLogCompressorTokens(utils.CountTokens(string(out)))
	// CCR integration: store the ORIGINAL (not the
	// compressed) in the CompressionStore under a
	// stable cache_key. The LLM can later request the
	// original via the headroom_retrieve tool (P0.6).
	// We use a SHA-256-derived key so identical
	// originals collide.
	if saved > 0 {
		store := h.getCompressionStore()
		if store != nil {
			key := cacheKeyFor(payload)
			if _, err := store.Save(key, payload); err == nil {
				cur := hctx.GetFeature("ccr_compression_store_saved")
				var n int
				if v, ok := cur.(int); ok {
					n = v
				}
				hctx.SetFeature("ccr_compression_store_saved", n+1)
				// P1.5 DASHBOARD FIRST: bump the per-hook metric.
				metrics.RecordCCRCompressionStore()
				// Record the cache_key so the downstream
				// response handler (Phase 2) can reference
				// the same original when the LLM asks for it.
				hctx.SetFeature("ccr_cache_key", key)
			}
		}
	}
	return out, nil
}

// AfterResponse is a no-op.
func (h *LogCompressorHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// IsEnabled returns true. LogCompressor is essentially
// free on non-trace payloads and a substantial saver on
// trace payloads.
func (h *LogCompressorHook) IsEnabled(vk string) bool { return true }

// compressStructuredLogs applies the scoring + first/last
// strategy to structured logs (lines with level prefix
// like INFO:, DEBUG:, WARN:, ERROR:). This catches the
// 70% of tool outputs that are NOT stack traces but are
// large log dumps (pytest, npm, cargo, jest, etc.).
//
// The strategy:
//   1. Keep the first N lines (start of log context)
//   2. Score every middle line
//   3. Keep the top K highest-scored middle lines
//   4. Keep the last M lines (end of log context)
//   5. Drop the rest with a "... (N lines dropped) ..."
//      marker
//
// Returns the input unchanged if no compression opportunity
// is found (e.g. payload is too small, or no recognizable
// level prefixes).
func (h *LogCompressorHook) compressStructuredLogs(payload []byte) []byte {
	scorer := NewLogLineScorer()
	out := make([]byte, 0, len(payload))
	i := 0
	for i < len(payload) {
		// Find the next tool message content.
		cs, ce, ok := findStringField(payload[i:], "content")
		if !ok {
			// No more content fields; copy the rest and stop.
			out = append(out, payload[i:]...)
			break
		}
		cs += i
		ce += i
		// Copy everything up to the opening quote of content.
		out = append(out, payload[i:cs]...) // copy up to opening quote (NOT including it)
		// Get the raw content, decode, compress, re-encode.
		raw := payload[cs+1 : ce]
		decoded := unescapeJSONString(raw)
		compressed := h.compressStructuredString(decoded, scorer)
		if compressed == decoded {
			// No savings: copy original raw wrapped
			// in the two quotes (we excluded the
			// opening quote from payload[i:cs] above).
			out = append(out, payload[cs:cs+1]...)
			out = append(out, raw...)
			out = append(out, payload[ce:ce+1]...)
		} else {
			// jsonEncodeString already includes both
			// surrounding quotes, so we skip the
			// original closing quote.
			reencoded := jsonEncodeString(compressed)
			out = append(out, reencoded...)
		}
		i = ce + 1
	}
	if len(out) >= len(payload) {
		return payload
	}
	return out
}

// keep sorter import alive for future use
var _ = itoa

// indexedLine is a scored line with its original index
// in the source. Used by compressStructuredString to
// sort by score while preserving the original order for
// ties.
type indexedLine struct {
	text  string
	score float64
	idx   int
}

// sortIndexedByScoreDescStable sorts indexedLine by score
// descending, breaking ties by original index ascending.
// Insertion sort, O(n^2) but n is small (< 200).
func sortIndexedByScoreDescStable(s []indexedLine) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && (s[j].score > s[j-1].score || (s[j].score == s[j-1].score && s[j].idx < s[j-1].idx)); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// compressStructuredString applies the scoring strategy
// to a single string. Returns the input unchanged if no
// compression opportunity is found.
func (h *LogCompressorHook) compressStructuredString(s string, scorer *LogLineScorer) string {
	lines := strings.Split(s, "\n")
	if len(lines) < 20 {
		// Too small to bother (would save < 5 lines).
		return s
	}
	// Count level-prefix lines; bail if too few (the
	// payload is probably not a structured log).
	levelCount := 0
	for _, l := range lines {
		if scorer.levelRE.MatchString(l) {
			levelCount++
		}
	}
	if levelCount < 5 {
		return s
	}
	// Strategy: keep first 5 + last 5 + top 10 scored middle
	// + error context lines (3 before and after each ERROR).
	const keepFirst = 5
	const keepLast = 5
	const keepTopK = 10
	const errorContextLines = 3
	firstLines := lines[:keepFirst]
	lastLines := lines[len(lines)-keepLast:]
	middleLines := lines[keepFirst : len(lines)-keepLast]
	// Dedupe adjacent identical lines in the middle.
	middleLines = dedupeAdjacent(middleLines, 5)
	// Dedupe non-adjacent warnings that share the same
	// keyword. We group by the first keyword found in
	// the line (timeout, refused, missing, etc.) and
	// keep the first occurrence of each group, replacing
	// subsequent ones with a count marker. This is the
	// Headroom dedupe_warnings behavior, simplified.
	// We apply it on the whole log (first + middle + last)
	// so the dedup is consistent across sections.
	allLines := append([]string{}, firstLines...)
	allLines = append(allLines, middleLines...)
	allLines = append(allLines, lastLines...)
	allLines = dedupeByKeyword(allLines, scorer)
	// Re-split into first/middle/last after dedup.
	if len(allLines) >= keepFirst {
		firstLines = allLines[:keepFirst]
		if len(allLines) >= keepFirst+keepLast {
			middleLines = allLines[keepFirst : len(allLines)-keepLast]
			lastLines = allLines[len(allLines)-keepLast:]
		} else {
			middleLines = allLines[keepFirst:]
			lastLines = nil
		}
	} else {
		firstLines = allLines
		middleLines = nil
		lastLines = nil
	}
	// Find all ERROR-level lines in the middle and
	// promote their context (N lines before and after)
	// to the must-keep set. This is a hard guarantee:
	// even if the context lines score low (INFO), they
	// are kept because they show what happened around
	// the failure.
	indexed := make([]indexedLine, len(middleLines))
	for i, l := range middleLines {
		indexed[i] = indexedLine{text: l, score: scorer.ScoreLine(l), idx: i}
	}
	// Find lines that are ERROR or higher.
	errorIdxs := make(map[int]bool)
	for i, il := range indexed {
		if il.score >= 0.85 {
			errorIdxs[i] = true
		}
	}
	// Promote the N lines before and after each error
	// to the must-keep set.
	mustKeep := make(map[int]bool)
	for i := range errorIdxs {
		for j := i - errorContextLines; j <= i+errorContextLines; j++ {
			if j >= 0 && j < len(indexed) {
				mustKeep[j] = true
			}
		}
	}
	// Score the rest and keep the top K, plus all
	// must-keep lines.
	rest := make([]indexedLine, 0, len(indexed))
	for _, il := range indexed {
		if !mustKeep[il.idx] {
			rest = append(rest, il)
		}
	}
	// Sort rest by score desc, then by idx asc.
	sortIndexedByScoreDescStable(rest)
	// Build the kept list: must-keep lines (in idx order)
	// + top K of rest (in idx order).
	keptIdx := make(map[int]bool)
	for idx := range mustKeep {
		keptIdx[idx] = true
	}
	for i := 0; i < keepTopK && i < len(rest); i++ {
		keptIdx[rest[i].idx] = true
	}
	// Build the output in original order.
	kept := make([]string, 0, len(middleLines))
	for i, il := range indexed {
		if keptIdx[i] {
			kept = append(kept, il.text)
		}
	}
	// Build the output.
	dropped := len(middleLines) - len(kept)
	out := make([]string, 0, keepFirst+len(kept)+keepLast+1)
	out = append(out, firstLines...)
	if dropped > 0 {
		out = append(out, "... ("+itoa(dropped)+" log lines dropped) ...")
	}
	out = append(out, kept...)
	out = append(out, lastLines...)
	return strings.Join(out, "\n")
}

// compressAllTraces walks the payload, finds every tool
// message content, and compresses any stack traces it
// finds in that content. The structure is: locate the
// messages array, then for each tool message run
// compressToolMessage. If the payload isn't parseable as
// a chat-completion request we return it unchanged.
func (h *LogCompressorHook) compressAllTraces(payload []byte) []byte {
	// Find the "messages":[ ... ] array.
	idx := bytesIndex(payload, []byte(`"messages"`))
	if idx < 0 {
		return payload
	}
	i := idx + len(`"messages"`)
	for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' || payload[i] == '\n' || payload[i] == '\r') {
		i++
	}
	if i >= len(payload) || payload[i] != ':' {
		return payload
	}
	i++
	for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' || payload[i] == '\n' || payload[i] == '\r') {
		i++
	}
	if i >= len(payload) || payload[i] != '[' {
		return payload
	}
	arrStart := i
	out := make([]byte, 0, len(payload))
	out = append(out, payload[:arrStart+1]...)
	i = arrStart + 1
	for i < len(payload) {
		if payload[i] == ']' {
			out = append(out, payload[i:i+1]...)
			i++
			break
		}
		if payload[i] != '{' {
			out = append(out, payload[i:i+1]...)
			i++
			continue
		}
		objEnd, ok := findObjectEnd(payload, i)
		if !ok {
			return payload
		}
		obj := payload[i : objEnd+1]
		if isToolMessage(obj) {
			compressed := h.compressToolMessage(obj)
			out = append(out, compressed...)
		} else {
			out = append(out, obj...)
		}
		i = objEnd + 1
		for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' || payload[i] == '\n' || payload[i] == '\r') {
			out = append(out, payload[i:i+1]...)
			i++
		}
		if i < len(payload) && payload[i] == ',' {
			out = append(out, ',')
			i++
			for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' || payload[i] == '\n' || payload[i] == '\r') {
				out = append(out, payload[i:i+1]...)
				i++
			}
		}
	}
	if i < len(payload) {
		out = append(out, payload[i:]...)
	}
	return out
}

// isToolMessage returns true if the JSON object has a
// "role":"tool" field.
func isToolMessage(obj []byte) bool {
	roleIdx := bytesIndex(obj, []byte(`"role"`))
	if roleIdx < 0 {
		return false
	}
	i := roleIdx + len(`"role"`)
	for i < len(obj) && (obj[i] == ' ' || obj[i] == '\t') {
		i++
	}
	if i >= len(obj) || obj[i] != ':' {
		return false
	}
	i++
	for i < len(obj) && (obj[i] == ' ' || obj[i] == '\t') {
		i++
	}
	if i >= len(obj) || obj[i] != '"' {
		return false
	}
	i++
	j := i
	for j < len(obj) && obj[j] != '"' {
		j++
	}
	if j >= len(obj) {
		return false
	}
	return string(obj[i:j]) == "tool"
}

// compressToolMessage returns a new byte slice equal to
// obj but with the "content" field's stack traces
// truncated.
func (h *LogCompressorHook) compressToolMessage(obj []byte) []byte {
	cs, ce, ok := findStringField(obj, "content")
	if !ok {
		return obj
	}
	raw := obj[cs+1 : ce]
	decoded := unescapeJSONString(raw)
	compressed := h.compressTraceString(decoded)
	if compressed == decoded {
		return obj
	}
	reencoded := jsonEncodeString(compressed)
	out := make([]byte, 0, len(obj)-len(raw)+len(reencoded))
	out = append(out, obj[:cs]...)
	out = append(out, reencoded...)
	out = append(out, obj[ce+1:]...)
	return out
}

// findStringField finds the position of the opening and
// closing quotes around the string value of a top-level
// "key":"value" field.
func findStringField(obj []byte, key string) (int, int, bool) {
	needle := `"` + key + `"`
	idx := bytesIndex(obj, []byte(needle))
	if idx < 0 {
		return 0, 0, false
	}
	i := idx + len(needle)
	for i < len(obj) && (obj[i] == ' ' || obj[i] == '\t') {
		i++
	}
	if i >= len(obj) || obj[i] != ':' {
		return 0, 0, false
	}
	i++
	for i < len(obj) && (obj[i] == ' ' || obj[i] == '\t') {
		i++
	}
	if i >= len(obj) || obj[i] != '"' {
		return 0, 0, false
	}
	openQuote := i
	i++
	for i < len(obj) {
		if obj[i] == '\\' {
			i += 2
			continue
		}
		if obj[i] == '"' {
			return openQuote, i, true
		}
		i++
	}
	return 0, 0, false
}

// unescapeJSONString decodes standard JSON escape sequences
// in a string literal's body (without the surrounding
// quotes).
func unescapeJSONString(s []byte) string {
	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			out = append(out, c)
			i++
			continue
		}
		esc := s[i+1]
		switch esc {
		case '"':
			out = append(out, '"')
		case '\\':
			out = append(out, '\\')
		case '/':
			out = append(out, '/')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			if i+6 <= len(s) {
				out = append(out, s[i:i+6]...)
				i += 4
			} else {
				out = append(out, c, esc)
			}
		default:
			out = append(out, c, esc)
		}
		i += 2
	}
	return string(out)
}

// jsonEncodeString encodes a Go string as a JSON string
// literal (with surrounding quotes). The inverse of
// unescapeJSONString above.
func jsonEncodeString(s string) []byte {
	var out []byte
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if c < 0x20 {
				out = append(out, '\\', 'u', '0', '0')
				const hex = "0123456789abcdef"
				out = append(out, hex[c>>4], hex[c&0xF])
			} else {
				out = append(out, c)
			}
		}
	}
	out = append(out, '"')
	return out
}

// compressTraceString is the main transform. It detects
// whether the string contains a stack trace, splits
// chained exceptions, and compresses each trace.
func (h *LogCompressorHook) compressTraceString(s string) string {
	if !looksLikeAnyTrace(s) {
		return s
	}
	traces := splitOnChainedException(s)
	out := make([]string, 0, len(traces))
	for _, trace := range traces {
		out = append(out, h.compressSingleTrace(trace))
	}
	return strings.Join(out, "\n")
}

// looksLikeAnyTrace returns true if s contains any of the
// three trace headers.
func looksLikeAnyTrace(s string) bool {
	return pythonTraceStart.MatchString(s) ||
		jsTraceStart.MatchString(s) ||
		rustPanicStart.MatchString(s)
}

// splitOnChainedException splits a multi-trace payload on
// the chaining marker line. The marker line is included
// in the second trace.
func splitOnChainedException(s string) []string {
	loc := chainedExceptionMarker.FindStringIndex(s)
	if loc == nil {
		return []string{s}
	}
	return []string{
		strings.TrimRight(s[:loc[0]], "\n"),
		strings.TrimLeft(s[loc[0]:], "\n"),
	}
}

// compressSingleTrace compresses a single trace. The
// body (header + everything after) is split into:
//   - preFrame: non-frame context lines between the
//     header and the first frame
//   - frames: the actual frame lines
//   - postFrame: the error message and trailing context
//
// The frames block is truncated to keepFirst + keepLast
// if it has more than that. The preFrame and postFrame
// blocks are preserved verbatim.
func (h *LogCompressorHook) compressSingleTrace(trace string) string {
	lines := strings.Split(trace, "\n")

	// Find the trace header.
	headerIdx := -1
	for i, line := range lines {
		if pythonTraceStart.MatchString(line) ||
			jsTraceStart.MatchString(line) ||
			rustPanicStart.MatchString(line) {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return trace
	}

	// preFrame: everything from the start of the trace up
	// to (but not including) the first frame. This
	// includes the header (which we render separately) and
	// any non-frame context lines between the header and
	// the first frame (e.g. "During handling of..." at the
	// start of a chained trace, or "stack backtrace:" in
	// Rust panics). The first frame itself is the
	// boundary.
	firstFrame := headerIdx + 1
	for firstFrame < len(lines) {
		l := lines[firstFrame]
		if strings.TrimSpace(l) != "" && traceFrameLine.MatchString(l) {
			break
		}
		firstFrame++
	}
	// Find the last frame in the body (search backwards).
	lastFrame := firstFrame - 1
	for i := len(lines) - 1; i >= firstFrame; i-- {
		l := lines[i]
		if strings.TrimSpace(l) != "" && traceFrameLine.MatchString(l) {
			lastFrame = i
			break
		}
	}

	// preFrame is everything between the start of the
	// trace and the first frame. This includes the header
	// line (which we'll re-emit separately) and any
	// pre-frame context. We render the header explicitly
	// later so we can skip the header in preFrame.
	preFrame := lines[0:firstFrame]
	frames := lines[firstFrame : lastFrame+1]
	postFrame := lines[lastFrame+1:]

	// Drop the header from preFrame since we emit it
	// explicitly.
	if headerIdx < len(preFrame) {
		preFrame = append(preFrame[:headerIdx], preFrame[headerIdx+1:]...)
	}

	// Dedupe adjacent identical frames.
	frames = dedupeAdjacent(frames, dedupRunMin)

	// Truncate the middle frames block.
	var kept []string
	if len(frames) >= keepFirstFrames+keepLastFrames {
		// Account for preFrame taking one of the "first"
		// slots so a 7-frame trace with a preFrame
		// context still gets truncated.
		firstForKeep := keepFirstFrames - len(preFrame)
		if firstForKeep < 0 {
			firstForKeep = 0
		}
		kept = keepFirstAndLast(frames, firstForKeep, keepLastFrames)
	} else {
		kept = frames
	}

	out := make([]string, 0, 1+len(preFrame)+len(kept)+len(postFrame))
	out = append(out, lines[headerIdx])
	if len(preFrame) > 0 {
		out = append(out, preFrame...)
	}
	out = append(out, kept...)
	if len(postFrame) > 0 {
		out = append(out, postFrame...)
	}
	return strings.Join(out, "\n")
}

// dedupeAdjacent collapses runs of identical adjacent
// lines into a single line plus a count marker.
func dedupeAdjacent(lines []string, min int) []string {
	if len(lines) == 0 || min < 2 {
		return lines
	}
	out := make([]string, 0, len(lines))
	i := 0
	for i < len(lines) {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		runLen := j - i
		if runLen >= min {
			out = append(out, lines[i])
			out = append(out, "... ("+itoa(runLen-1)+" identical lines) ...")
		} else {
			for k := i; k < j; k++ {
				out = append(out, lines[k])
			}
		}
		i = j
	}
	return out
}

// keepFirstAndLast returns the first `first` lines, a
// drop marker, and the last `last` lines.
func keepFirstAndLast(frames []string, first, last int) []string {
	if len(frames) <= first+last {
		return frames
	}
	out := make([]string, 0, first+last+1)
	out = append(out, frames[:first]...)
	middle := len(frames) - first - last
	out = append(out, "... ("+itoa(middle)+" middle frames dropped) ...")
	out = append(out, frames[len(frames)-last:]...)
	return out
}

// itoa is a tiny local int-to-string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// init registers the hook.
func init() {
	RegisterHook(&LogCompressorHook{})
	log.Printf("[hooks] registered LogCompressorHook at priority 770")
}
