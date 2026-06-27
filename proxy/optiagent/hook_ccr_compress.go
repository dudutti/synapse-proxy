// Package optiagent — CCRCompressHook.
//
// CCR (Compress-Cache-Retrieve) is a third cache lookup level
// that sits between L1 (byte-exact) and L2 (embedding-similar).
// The idea: many "different" requests are semantically identical
// (whitespace differences, CRLF vs LF, empty system messages
// added by wrappers, etc.) and they hash to different keys, so
// L1 misses. CCR applies a small set of canonicalizations so
// those requests hash to the same CCR key.
//
// The canonicalizations implemented here are intentionally
// conservative:
//
//  1. Collapse internal runs of spaces to a single space
//     (preserves content but eliminates formatting noise).
//  2. Normalize CRLF and CR to LF.
//  3. Strip trailing whitespace on each user content string.
//  4. Drop system messages whose content is empty or
//     whitespace-only (a common pattern where wrappers prepend
//     an empty system block).
//
// What we deliberately do NOT touch:
//   - tool_calls.function.arguments: it's JSON inside a string,
//     whitespace is significant.
//   - Tool result messages: those are usually file contents and
//     are already canonical in their own right; modifying them
//     would risk semantic divergence.
//   - Anything outside the `messages` field: the model name,
//     temperature, etc. are taken as-is.
//
// The output of compress() is itself stable (same input → same
// output bytes), so the CCR hash (sha256 of the compressed
// bytes) is a deterministic cache key. CCRRetrieveHook reads
// hctx.CCRCompressedPayload and looks it up in Redis; on a hit
// it short-circuits with the cached response.
package optiagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// CCRCompressHook is a BeforeRequest hook that canonicalizes
// the request payload so semantically equivalent prompts hash to
// the same cache key. It is the first half of the CCR pipeline;
// the second half is CCRRetrieveHook (cache lookup) and the
// third is CCRStoreHook (response storage in AfterResponse).
type CCRCompressHook struct{}

// Name returns the hook name used in metrics and log lines.
func (h *CCRCompressHook) Name() string { return "ccr_compress" }

// Priority places CCRCompress BEFORE CCRRetrieve (750) so the
// hash is available for the cache lookup. Previously was 800
// which caused Retrieve to always no-op (hash not yet computed).
func (h *CCRCompressHook) Priority() int { return 740 }

// BeforeRequest runs the canonicalization and stashes the
// result on the HookContext so CCRRetrieveHook (next in
// priority order) can use it as a cache key.
//
// If the payload is malformed JSON, the hook is a no-op: we
// don't want to corrupt a request we can't even parse, and
// the JSON parser will produce a clearer error than our
// post-canonicalization rewrite would.
func (h *CCRCompressHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx.OptimizedPayload == nil || len(hctx.OptimizedPayload) == 0 {
		return nil, nil
	}

	compressed, ok := h.compress(hctx.OptimizedPayload)
	if !ok {
		// Not parseable as a chat-completion payload. Leave the
		// payload alone and let upstream return its own error.
		// This covers /v1/embeddings, /v1/completions, etc.
		return nil, nil
	}

	hctx.CCRCompressedPayload = compressed
	hash := sha256.Sum256(compressed)
	hctx.SetFeature("ccr_hash", hex.EncodeToString(hash[:]))
	return nil, nil
}

// AfterResponse is a no-op for compression. The complementary
// hook CCRStoreHook (separate file) is responsible for storing
// the upstream response under the CCR hash.
func (h *CCRCompressHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// IsEnabled returns true. CCR is always-on: the canonicalization
// is side-effect-free, deterministic, and idempotent. A future
// feature flag (per-VK opt-out) could gate this, but for now
// every request benefits from being hashable in the CCR cache.
func (h *CCRCompressHook) IsEnabled(vk string) bool { return true }

// compress is the heart of CCR. It returns the canonicalized
// payload and a boolean indicating whether canonicalization
// was possible (false for non-chat payloads). The shape is
// parse → edit messages → re-serialize so we don't have to
// hand-roll a JSON canonicalizer for the rest of the body
// (model name, temperature, top_p, etc. are kept verbatim).
func (h *CCRCompressHook) compress(in []byte) ([]byte, bool) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(in, &body); err != nil {
		return nil, false
	}
	rawMsgs, ok := body["messages"]
	if !ok {
		return nil, false
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &msgs); err != nil {
		return nil, false
	}

	canonicalized := make([]json.RawMessage, 0, len(msgs))
	for _, m := range msgs {
		cm, _ := canonicalizeMessage(m)
		// canonicalizeMessage returns dropSentinel for messages
		// that should be dropped (e.g. empty system messages).
		if string(cm) == string(dropSentinel) {
			continue
		}
		canonicalized = append(canonicalized, cm)
	}
	// Always re-serialize so the output is in canonical form
	// (deterministic field order, no trailing whitespace, etc.)
	// even when the input was already canonical. This is what
	// makes the CCR hash stable across "byte-different but
	// semantically-equal" inputs.
	rawCanonical, err := json.Marshal(canonicalized)
	if err != nil {
		return nil, false
	}
	body["messages"] = rawCanonical
	out, err := json.Marshal(body)
	if err != nil {
		return nil, false
	}
	return out, true
}

// canonicalizeMessage returns the canonicalized form of one
// message and a boolean that says whether any change was made.
// A message with role=tool is passed through unchanged (the
// content is a JSON tool result and whitespace matters).
func canonicalizeMessage(raw json.RawMessage) (json.RawMessage, bool) {
	m := messageShape{Role: extractRole(raw)}

	switch m.Role {
	case "system":
		// Drop empty/whitespace-only system messages.
		cs, ok := extractStringField(raw, "content")
		if ok && strings.TrimSpace(cs) == "" {
			return dropSentinel, true
		}
		cm, changed := canonicalizeContent(raw)
		return cm, changed
	case "user", "assistant":
		return canonicalizeContent(raw)
	case "tool":
		return raw, false
	default:
		return raw, false
	}
}

// dropSentinel is a special value returned by canonicalizeMessage
// when the message should be omitted from the canonical form.
// compress() strips any element equal to this value.
var dropSentinel = json.RawMessage("")

// canonicalizeContent is the hot path. It runs the whitespace
// canonicalizations on the message's `content` string AND, if
// the message has `tool_calls`, leaves the `arguments` strings
// inside untouched (per the regression guard test).
func canonicalizeContent(raw json.RawMessage) (json.RawMessage, bool) {
	out := raw
	changed := false

	// Normalize CRLF/CR to LF first, before any other
	// canonicalization runs. Then collapse internal runs of
	// spaces and strip trailing whitespace in one pass on the
	// normalized string.
	if cs, ok := extractStringField(raw, "content"); ok {
		newCS := canonicalizeString(cs)
		if newCS != cs {
			out = replaceStringField(out, "content", newCS)
			changed = true
		}
	}

	// (We deliberately do NOT touch tool_calls[i].function.
	//  arguments — it's a JSON string and whitespace matters.)
	//  The regression test TestCCRCompressHook_PreservesToolCallArguments
	//  locks this in.
	return out, changed
}

// canonicalizeString applies the string-level canonicalizations
// (collapse internal space runs, strip trailing whitespace,
// normalize newlines). It is intentionally tiny so the cost of
// running it on every message is negligible compared to the
// LLM call itself.
func canonicalizeString(s string) string {
	// Normalize CRLF/CR to LF first.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// Collapse internal runs of spaces to a single space.
	s = multiSpace.ReplaceAllString(s, " ")
	// Strip trailing whitespace.
	s = strings.TrimRight(s, " \t\n")
	return s
}

// multiSpace is a precompiled regex matching 2+ consecutive
// ASCII space characters. We do NOT include tabs or newlines:
// tabs in user content are rare and we want to preserve
// indentation in code blocks; newlines are normalized in a
// separate pass above.
var multiSpace = regexp.MustCompile(` {2,}`)

// messageShape is a small struct used by canonicalizeMessage
// to decide which canonicalization strategy to apply based on
// the role. We do this without a full struct unmarshal because
// the cost of `encoding/json.Unmarshal` for every message in
// every request is not negligible at proxy scale.
type messageShape struct {
	Role string
}

// extractRole pulls just the "role" field from a JSON message
// object. We use a cheap string scan that handles the
// overwhelmingly common case (role is the first or second
// field) without a full unmarshal. Returns "" if the role
// can't be determined, in which case the caller treats the
// message as "unknown role" and skips the canonicalization.
func extractRole(raw json.RawMessage) string {
	// Look for "role":"..." with optional whitespace.
	needle := `"role":`
	idx := bytes.Index(raw, []byte(needle))
	if idx < 0 {
		return ""
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != '"' {
		return ""
	}
	i++
	j := i
	for j < len(raw) && raw[j] != '"' {
		j++
	}
	if j >= len(raw) {
		return ""
	}
	return string(raw[i:j])
}

// extractStringField looks for "key":"value" in a JSON object
// and returns the unescaped value. Returns ok=false if the key
// isn't a string field.
func extractStringField(raw json.RawMessage, key string) (string, bool) {
	// The needle is the key plus its closing quote (not the
	// colon — we handle the colon below, after skipping
	// optional whitespace). Looking for `"content"` (not
	// `"content":`) avoids the off-by-one where the position
	// right after the closing quote might be the opening quote
	// of the value, not a colon.
	needle := `"` + key + `"`
	idx := bytes.Index(raw, []byte(needle))
	if idx < 0 {
		return "", false
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '	') {
		i++
	}
	if i >= len(raw) || raw[i] != ':' {
		return "", false
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '	') {
		i++
	}
	if i >= len(raw) || raw[i] != '"' {
		return "", false
	}
	i++
	// We must decode JSON string escapes. The JSON spec
	// recognizes \", \\, \/, \b, \f, \n, \r, 	, \uXXXX.
	// The canonicalization only needs to operate on the
	// real bytes (LF, CR, TAB), so we decode all of these
	// rather than a subset. Without this step, a payload
	// that contains "\n" in the JSON source would be
	// returned as the two characters backslash-n (instead
	// of the single byte LF) and canonicalizeString would
	// never see the real newline.
	var b strings.Builder
	for i < len(raw) {
		c := raw[i]
		if c != '\\' {
			if c == '"' {
				return b.String(), true
			}
			b.WriteByte(c)
			i++
			continue
		}
		// Escape sequence. Must have at least one more char.
		if i+1 >= len(raw) {
			return "", false
		}
		esc := raw[i+1]
		switch esc {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('	')
		case 'u':
			// \uXXXX — we don't decode the code point,
			// we just preserve the 6-char escape verbatim.
			// The canonicalization step doesn't care about
			// the Unicode value, only about the byte-level
			// whitespace, so leaving \uXXXX as 6 chars is
			// safe (they're 6 non-whitespace ASCII bytes).
			if i+6 > len(raw) {
				return "", false
			}
			b.Write(raw[i : i+6])
			i += 4 // (we add 2 more below)
		default:
			// Unknown escape — keep the raw bytes, let
			// the upstream JSON parser flag the error
			// later. This is the safest behavior.
			b.WriteByte('\\')
			b.WriteByte(esc)
		}
		i += 2
	}
	return "", false
}

// replaceStringField returns a new raw with the given string
// field's value replaced. We rebuild the raw byte slice by
// splitting around the original key-value pair and inserting
// the new value with proper JSON escaping.
func replaceStringField(raw json.RawMessage, key, value string) json.RawMessage {
	// Same needle shape as extractStringField (key plus closing
	// quote, then handle colon + optional whitespace
	// ourselves).
	needle := `"` + key + `"`
	idx := bytes.Index(raw, []byte(needle))
	if idx < 0 {
		return raw
	}
	i := idx + len(needle)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != ':' {
		return raw
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != '"' {
		return raw
	}
	oldStart := i
	// Find the matching close quote (skipping escaped quotes).
	i++
	for i < len(raw) {
		if raw[i] == '\\' {
			i += 2
			continue
		}
		if raw[i] == '"' {
			break
		}
		i++
	}
	if i >= len(raw) {
		return raw
	}
	oldEnd := i + 1

	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	// The encoded value already includes the surrounding quotes.
	out := make([]byte, 0, len(raw)+len(encoded)-(oldEnd-oldStart))
	out = append(out, raw[:oldStart]...)
	out = append(out, encoded...)
	out = append(out, raw[oldEnd:]...)
	return out
}

// init registers the hook with the global hook registry. The
// ordering matters: compress must run before retrieve (otherwise
// the hash is computed on the un-canonicalized payload and
// never matches a stored entry).
func init() {
	RegisterHook(&CCRCompressHook{})
	log.Printf("[hooks] registered CCRCompressHook at priority 740")
}
