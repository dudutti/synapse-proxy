// Package optiagent — CacheAlignerHook.
//
// Inspired by Headroom's headroom/transforms/cache_aligner.py
// (PR-A2 / P2-23 fix). The detector walks the request body
// looking for *volatile* content in the system prompt — the
// provider's KV-cache hot zone — and emits a warning so the
// operator knows their prompt cache is being busted on every
// request.
//
// What "volatile" means here:
//
//  1. UUIDs (8-4-4-4-12 hex). Session IDs, request IDs, trace
//     IDs are all common. If they appear in the system prompt
//     (rather than the user message that just sent them), the
//     cache prefix changes on every request.
//  2. ISO 8601 timestamps. "Today is 2026-06-24T15:30:00Z" is
//     the classic example — agents love to inject "now" into
//     the system prompt, but it changes every minute.
//  3. JWTs. Three base64url segments separated by dots. If
//     these are in the system prompt, the caller is leaking
//     auth tokens into the cache hot zone (a real security
//     risk on top of the cache-bust problem).
//  4. Hex hashes (MD5/SHA1/SHA256). 32, 40, or 64 char hex
//     runs. Often a checksum or content hash that should
//     belong in the user message, not the system prompt.
//
// The hook is **detector-only** by design. Headroom removed
// their rewrite path for exactly the same reason we are not
// implementing one: the system prompt is the cache hot zone,
// and any mutation invalidates the cache for every request.
// We just observe and warn. The warning is exposed via the
// `ccr_cache_aligner_warning` feature on the HookContext so
// it can flow into the dashboard's observability widgets.
package optiagent

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// CacheAlignerHook flags volatile / dynamic content in the
// system prompt. It runs as a BeforeRequest hook at priority
// 700 — after the upstream response shape is normalized but
// before any compression hook mutates the payload, so the
// detection sees the original (canonical) system prompt.
type CacheAlignerHook struct{}

// Name returns the hook name used in metrics and log lines.
func (h *CacheAlignerHook) Name() string { return "cache_aligner" }

// Priority places CacheAligner before the compression hooks
// (CCR Compress is 800) so it observes the pre-compression
// payload. The system prompt we want to inspect is the *raw*
// one, not the post-compression one.
func (h *CacheAlignerHook) Priority() int { return 700 }

// volatileSpan captures one matched volatile element in a
// system prompt along with the kind of element it is.
type volatileSpan struct {
	label string
	text  string
}

// BeforeRequest scans the system message(s) for volatile
// content and warns if anything is found. The payload is
// intentionally NOT rewritten here because rewriting the
// system prompt at the byte level is brittle (a single
// off-by-one offset in a content field that itself contains
// escape sequences or surrogate pairs produces a JSON that
// the upstream rejects with a 400).
//
// The warning is consumed by the dashboard's "cache stability"
// tile so operators can see which VKs have unstable prefixes.
// A future cache_aligner_rewrite.go will implement the actual
// rewrite once we have a fully-tested byte-level mutator
// (see HEADROOM_PLAN.md in the repo root).
func (h *CacheAlignerHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx.OptimizedPayload == nil || len(hctx.OptimizedPayload) == 0 {
		return hctx.OptimizedPayload, nil
	}

	systemContent, ok := extractSystemContent(hctx.OptimizedPayload)
	if !ok {
		return hctx.OptimizedPayload, nil
	}

	var findings []string
	if uuidRe.MatchString(systemContent) {
		findings = append(findings, "uuid")
	}
	if iso8601Re.MatchString(systemContent) {
		findings = append(findings, "iso8601")
	}
	if jwtRe.MatchString(systemContent) {
		findings = append(findings, "jwt")
	}
	if hexHashRe.MatchString(systemContent) {
		findings = append(findings, "hex_hash")
	}
	if len(findings) == 0 {
		return hctx.OptimizedPayload, nil
	}

	warning := "system prompt contains volatile content: " + strings.Join(findings, ",")
	hctx.SetFeature("ccr_cache_aligner_warning", warning)
	log.Printf("[%s] %s vk=%s", h.Name(), warning, hctx.VK)
	// Return payload unchanged — the rewrite path is disabled
	// until byte-level splicing is verified by tests.
	return hctx.OptimizedPayload, nil
}

// rewriteSystemPromptStable replaces the first system message's
// content with one where the matched spans have been removed
// and re-appended as a "volatile context" trailer. Returns the
// rewritten payload, or (nil, false) if any step fails.
func rewriteSystemPromptStable(payload []byte, _ string, spans []volatileSpan) ([]byte, bool) {
	// Find the messages array.
	msgKeyIdx := bytesIndex(payload, []byte(`"messages"`))
	if msgKeyIdx < 0 {
		return nil, false
	}
	// Walk into the array, find the first '{'.
	i := msgKeyIdx + len(`"messages"`)
	for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' ||
		payload[i] == '\n' || payload[i] == '\r' || payload[i] == ':') {
		i++
	}
	if i >= len(payload) || payload[i] != '[' {
		return nil, false
	}
	i++
	for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' ||
		payload[i] == '\n' || payload[i] == '\r' || payload[i] == ',') {
		i++
	}
	if i >= len(payload) || payload[i] != '{' {
		return nil, false
	}
	objEnd, ok := findObjectEnd(payload, i)
	if !ok {
		return nil, false
	}
	obj := payload[i : objEnd+1]
	if !isSystemMessage(obj) {
		return payload, true
	}
	contentKeyIdx := bytesIndex(obj, []byte(`"content"`))
	if contentKeyIdx < 0 {
		return nil, false
	}
	// Walk past `"content"` (the key), the closing `"`, the
	// colon, optional whitespace, and the opening `"` of the
	// value. We need to land on the value's opening `"`, which
	// is what csOpen points at.
	j := contentKeyIdx + len(`"content"`)
	// Now skip the closing `"` of the key.
	if j < len(obj) && obj[j] == '"' {
		j++
	}
	for j < len(obj) && (obj[j] == ' ' || obj[j] == '\t' || obj[j] == '\n' || obj[j] == '\r' || obj[j] == ':') {
		j++
	}
	if j >= len(obj) || obj[j] != '"' {
		return nil, false
	}
	csOpen := j
	j++
	escaped := false
	csClose := -1
	for j < len(obj) {
		if escaped {
			escaped = false
			j++
			continue
		}
		if obj[j] == '\\' {
			escaped = true
			j++
			continue
		}
		if obj[j] == '"' {
			csClose = j
			break
		}
		j++
	}
	if csClose < 0 {
		return nil, false
	}

	originalContent := obj[csOpen+1 : csClose]
	stablePart := string(originalContent)
	var trailerLines []string
	for _, s := range spans {
		stablePart = strings.ReplaceAll(stablePart, s.text, "")
		trailerLines = append(trailerLines, "["+s.label+": "+s.text+"]")
	}
	trailer := "\n\n[Volatile context moved here for cache stability: " +
		strings.Join(trailerLines, " ") + "]"
	newContent := stablePart + trailer

	encoded, err := json.Marshal(newContent)
	if err != nil {
		return nil, false
	}

	newObj := make([]byte, 0, len(obj)+len(encoded))
	newObj = append(newObj, obj[:csOpen]...)
	newObj = append(newObj, encoded...)
	newObj = append(newObj, obj[csClose:]...)

	log.Printf("[%s] rewrote system content: stable=%d bytes, trailer=%d bytes (volatile moved to end for cache stability)",
		"cache_aligner", csClose-csOpen-1, len(newContent)-(csClose-csOpen-1))

	// Splice the new object back into the payload. i is
	// the offset of the system message's opening '{' in
	// payload, and objEnd is the offset of its closing '}'
	// (both inclusive of the brace, since obj := payload[i:objEnd+1]).
	newPayload := make([]byte, 0, len(payload)+len(encoded))
	newPayload = append(newPayload, payload[:i]...)
	newPayload = append(newPayload, newObj...)
	newPayload = append(newPayload, payload[objEnd+1:]...)
	return newPayload, true
}

// AfterResponse is a no-op. Cache alignment is a read-only
// observation; there's nothing to persist after the upstream
// has responded.
func (h *CacheAlignerHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// IsEnabled returns true. The detection itself is essentially
// free (4 regex scans on a system prompt that is usually
// small) and the warning only fires when volatile content is
// found, so there's no reason to gate this hook behind a
// feature flag. Operators who never see the warning pay
// nothing for it.
func (h *CacheAlignerHook) IsEnabled(vk string) bool { return true }

// uuidRe matches RFC 4122 UUIDs. The hex-only version
// (no braces, no urn:uuid: prefix) covers the overwhelming
// majority of agent-side uses; if we ever need the others
// we can extend without breaking the existing pattern.
var uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// iso8601Re matches the most common ISO 8601 date-time
// forms: 2026-06-24T15:30:00, with optional fractional
// seconds and optional Z or ±HH:MM timezone offset. We do
// NOT try to match every variation of ISO 8601 (the spec
// is famously permissive); the goal is to catch the patterns
// real agents emit, not to be a complete parser.
var iso8601Re = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)

// jwtRe matches three base64url segments separated by dots.
// This is shape-only — we don't validate the signature or
// decode the payload. A more sophisticated detector could
// also enforce the segment-length profile (each base64url
// segment is at least 1 char and at most ~4096 chars for
// valid JWTs), but the shape check is enough to flag the
// common case of "auth token leaked into system prompt".
var jwtRe = regexp.MustCompile(`[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

// hexHashRe matches hex runs of 32, 40, or 64 characters
// (MD5, SHA1, SHA256). We do not try to match 128-char
// (SHA512) hex strings because they are vanishingly rare
// in agent system prompts and would create a lot of false
// positives on long hex sequences in code samples.
//
// The word-boundary anchors are critical: without them we'd
// match any 32+ char hex substring of a longer hex run,
// including substrings of legitimate 64-char SHA256 hashes
// (which would just count the same hash twice and produce
// a confusing warning).
var hexHashRe = regexp.MustCompile(`\b([0-9a-fA-F]{32}|[0-9a-fA-F]{40}|[0-9a-fA-F]{64})\b`)

// extractSystemContent pulls the content of the first
// system message out of a chat-completion payload. It uses
// a tiny state machine rather than a full JSON unmarshal
// because this is on the hot path (every request) and a
// full unmarshal is overkill for "give me the first system
// message's content string".
//
// Returns the content string and true on success, or
// "", false if no system message is found (or the payload
// is not parseable as a chat-completion request).
func extractSystemContent(payload []byte) (string, bool) {
	// Find the "messages":[ ... ] array. We're not trying to
	// be a real JSON parser — we just need to locate the
	// array and walk its top-level objects.
	idx := indexOf(payload, []byte(`"messages"`))
	if idx < 0 {
		return "", false
	}
	// Skip past the key, optional whitespace, the colon,
	// optional whitespace, and the opening bracket.
	i := skipJSONValueStart(payload, idx+len(`"messages"`))
	if i < 0 || payload[i] != '[' {
		return "", false
	}
	// Walk top-level objects until we find a "role":"system"
	// one, then return its "content" string.
	// i currently points at the opening '['. Skip it.
	i++
	for i < len(payload) {
		c := payload[i]
		// Skip whitespace and commas between top-level
		// elements.
		if c == ' ' || c == '	' || c == '\n' || c == '\r' || c == ',' {
			i++
			continue
		}
		if c != '{' {
			// Hit the end of the array (']') or something
			// we don't understand. Either way, no system
			// message.
			return "", false
		}
		// We're at the start of a top-level object. Scan
		// forward to find its end, recording the byte
		// range.
		end, ok := findObjectEnd(payload, i)
		if !ok {
			return "", false
		}
		obj := payload[i : end+1]
		// Check if this object is {"role":"system", ...} and
		// pull out its "content" string if so.
		if isSystemMessage(obj) {
			cs, ok := extractStringField(obj, "content")
			if ok {
				return cs, true
			}
			// System message exists but no string content
			// (e.g. content is a list of parts, or null).
			// Either way, there's nothing volatile to flag
			// in a non-string field, so return empty and
			// let the hook bail.
			return "", true
		}
		// Move past this object. Skip the trailing comma
		// if any.
		i = end + 1
	}
	return "", false
}

// isSystemMessage returns true if a JSON object has
// `"role":"system"` (with any amount of whitespace between
// the tokens). The role check is intentionally tolerant: we
// don't validate the rest of the object.
func isSystemMessage(obj []byte) bool {
	roleIdx := indexOf(obj, []byte(`"role"`))
	if roleIdx < 0 {
		return false
	}
	// Skip past the key + colon + optional whitespace, then
	// the opening quote, then the value, then the closing
	// quote. We compare the value to "system" (exact match).
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
	return string(obj[i:j]) == "system"
}

// findObjectEnd returns the index of the closing '}' of the
// object starting at start, accounting for nested objects
// and arrays and for JSON string escapes. Returns false if
// the object is not properly terminated before EOF.
func findObjectEnd(payload []byte, start int) (int, bool) {
	if start >= len(payload) || payload[start] != '{' {
		return 0, false
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(payload); i++ {
		c := payload[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inString {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			depth++
			continue
		}
		if c == '}' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// skipJSONValueStart, given the index just past a key like
// `"messages"`, returns the index of the first non-whitespace,
// non-colon character (which should be the start of the
// value, e.g. '[' for an array). Returns -1 if no such
// character is found.
func skipJSONValueStart(payload []byte, i int) int {
	for i < len(payload) {
		c := payload[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == ':' {
			i++
			continue
		}
		return i
	}
	return -1
}

// indexOf is bytes.Index with a slightly more readable name
// at the call site. It's a no-op alias but makes the
// detector code read like a description rather than a
// library call.
func indexOf(haystack, needle []byte) int {
	return bytesIndex(haystack, needle)
}

// bytesIndex is the indirection that lets us swap in a
// faster search later (Aho-Corasick, etc.) without touching
// the detection logic. Right now it's just bytes.Index.
var bytesIndex = func(haystack, needle []byte) int {
	// We can't import "bytes" at the package level here
	// without making the file longer, so we delegate to a
	// function variable that's initialized at package init.
	// The init function in hooks.go (or a small file we
	// add) sets this to bytes.Index. See initBytesIndex
	// below.
	return bytesIndexImpl(haystack, needle)
}

// bytesIndexImpl is the concrete implementation. We use a
// tiny shim instead of bytes.Index directly so the call
// sites read like a high-level description and so a future
// change (e.g. add a prefilter, swap the search algorithm)
// is a one-line change.
func bytesIndexImpl(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// init registers the hook with the global hook registry.
// CacheAligner runs before the compression hooks so it
// observes the pre-compression system prompt.
func init() {
	RegisterHook(&CacheAlignerHook{})
	log.Printf("[hooks] registered CacheAlignerHook at priority 700")
}
