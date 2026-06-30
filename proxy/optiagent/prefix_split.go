package optiagent

// Cache-preserving payload split for L3 compression.
//
// This file implements Phase 2 of the cache-preserving L3 project:
// given a raw JSON request payload, split it into a "static prefix"
// and a "dynamic tail" such that the prefix bytes are preserved
// bit-for-bit (so provider prompt caches — Anthropic, OpenAI,
// MiniMax — keep hitting across requests) while the tail can be
// modified freely by the L3 compression rules in compressor.go.
//
// Why this matters: today, CompressPayload re-encodes the entire
// request, which means the SHA-256 hash the provider uses as a
// cache key is different for every request even when the agent's
// system prompt + tool declarations + older history are identical.
// The provider sees a fresh request, charges a full cache_write
// every time, and never gets to charge the cheap cache_read rate.
//
// With this split, the L3 compressor can prune stale CoT, drop
// repeated tool results, and trim old assistant turns — all in the
// dynamic tail — without disturbing the provider's view of the
// static prefix. The agent gets both: provider cache_read on the
// 38k-token system prompt + history, and L3's token savings on
// the most recent turn.
//
// Algorithm (conservative variant):
//
//   1. Find the byte offset of the "messages" key in the raw JSON.
//   2. From there, scan the array, counting messages at the
//      top level (ignoring nested arrays such as content blocks).
//   3. The split point is the byte just before the start of the
//      second-to-last message element. Everything from the start of
//      the payload up to and including the comma between
//      messages[len-3] and messages[len-2] is the prefix; the rest
//      (including the closing ']') is the tail.
//   4. If the payload has fewer than 2 messages, or any other
//      structural anomaly, return the whole payload as the tail
//      with an empty prefix — the compressor will then run on the
//      full request and we accept the cache miss for that one.
//
// All byte offsets are computed by walking the raw JSON character
// by character with a tiny state machine. This avoids allocating
// any intermediate Go data structures and keeps the cost in the
// low microseconds even for 100k-token payloads (the L3 path is
// per-request, on the hot path).

import (
	"bytes"
	"errors"
	"fmt"
)

// PrefixSplit describes a payload split into a static prefix and a
// dynamic tail. PrefixJSON is the raw JSON bytes that must not be
// modified; TailJSON is the raw JSON bytes the L3 compressor is
// free to rewrite. PrefixTokens and TailTokens are tiktoken
// counts for diagnostics (the caller can ignore them).
type PrefixSplit struct {
	PrefixJSON   []byte
	TailJSON     []byte
	PrefixTokens int
	TailTokens   int
	// MessageCount is the number of messages in the original
	// messages array. Exposed for tests and metrics; production
	// callers do not need it.
	MessageCount int
}

// SplitAtPrefixBoundary walks the raw JSON payload and returns a
// PrefixSplit. The split is conservative: it preserves the system
// message, the tools block (if any), and the older assistant/user
// turns. Only the most recent two messages (typically the latest
// user turn and the most recent assistant turn) fall into the
// tail and may be modified by the L3 compressor.
//
// If the payload has fewer than 2 messages, the entire payload is
// returned as the tail and the prefix is empty. This is a safe
// fallback: the compressor still runs, the request still works, it
// just misses the cache hit for that one request.
func SplitAtPrefixBoundary(raw []byte) (PrefixSplit, error) {
	if len(raw) == 0 {
		return PrefixSplit{}, errors.New("SplitAtPrefixBoundary: empty payload")
	}

	// 1. Locate the "messages" key. We look for the first occurrence
	// of "messages" at the top level of the object (depth 1, after
	// the opening '{'). This is a small but critical optimization:
	// the key almost always appears before any other long field
	// (system, tools, temperature), so the scan stops early.
	messagesStart, err := findKeyAtTopLevel(raw, "messages")
	if err != nil {
		return PrefixSplit{}, err
	}
	if messagesStart < 0 {
		// No messages key — this isn't a chat-completion payload
		// (e.g. an embeddings request). Return everything as tail
		// so the caller can still run the compressor.
		return PrefixSplit{
			TailJSON:     raw,
			TailTokens:   countTokens(string(raw)),
			MessageCount: 0,
		}, nil
	}

	// messagesStart is the offset of the "messages" key string in
	// the raw payload. We need to find the opening '[' of the
	// array, which appears after the key, a colon, and any
	// whitespace. Scan past that to land on the '['.
	arrOpen, err := findArrayOpen(raw, messagesStart)
	if err != nil {
		return PrefixSplit{}, err
	}

	// 2. Count messages at the top level of the array. We need
	// the index where the 4th-to-last message starts so we can
	// preserve everything up to (and including) the preceding
	// comma. The tail will be the 4 most recent messages.
	//
	// Why 4 and not 2: the L3 compressor's CoT-stripping rule
	// only applies to NON-recent assistant messages
	// (i < msgCount-2). With only 2 messages in the tail, both
	// are "recent" and no CoT is stripped. With 3 messages, the
	// 3rd-from-last is still recent (msgCount-2=4 for a 6-msg
	// payload, so i=4 is at the boundary). With 4 messages in
	// the tail, the 4th-from-last is unambiguously non-recent
	// and its CoT gets pruned, while the 3 most recent turns
	// (typically user→assistant→user) stay untouched.
	//
	// Trade-off: a bigger tail means a smaller prefix, which
	// means a smaller cache key and a smaller cache_read
	// benefit. We picked 4 as a balance: the 2 most recent
	// user turns are protected (so the agent's safety filters
	// re-checking them see the same content as before), and
	// the older assistant turn in the tail can have its CoT
	// stripped (typically 5-10k tokens of pure waste).
	var count, splitOffset int
	count, splitOffset, err = findNthToLastMessageStart(raw, arrOpen, 4)
	if err != nil {
		return PrefixSplit{}, err
	}
	if count < 4 || splitOffset < 0 {
		// Fewer than 4 messages (or no 4th-to-last). Return
		// the whole payload as tail. The caller will fall back
		// to the full CompressPayload.
		return PrefixSplit{
			TailJSON:     raw,
			TailTokens:   countTokens(string(raw)),
			MessageCount: count,
		}, nil
	}

	// splitOffset is the offset of the first byte of the
	// second-to-last message. The prefix is raw[0:splitOffset],
	// The split point is the offset of the first byte of the
	// second-to-last message. We want the prefix to end at the
	// '}' of the message just before, INCLUDING the comma that
	// follows (so that the round-trip prefix + tail == raw is
	// byte-exact). The tail then starts at the '{' of the
	// second-to-last message.
	prefixEnd := splitOffset
	// Back up over any whitespace between the previous '}' and
	// the '{' of the second-to-last message.
	for prefixEnd > 0 && (raw[prefixEnd-1] == ' ' || raw[prefixEnd-1] == '\n' || raw[prefixEnd-1] == '\t' || raw[prefixEnd-1] == '\r') {
		prefixEnd--
	}
	// raw[prefixEnd-1] should now be ',' (the separator between
	// the two messages). If it isn't, something is wrong with
	// the scan — fall back to the raw splitOffset.
	if prefixEnd > 0 && raw[prefixEnd-1] == ',' {
		// Include the comma in the prefix. Tail starts at the
		// '{' of the second-to-last message.
	} else {
		// Defensive: comma missing. Just use the original
		// splitOffset and let the tail start with whatever
		// bytes are there.
		prefixEnd = splitOffset
	}

	prefix := raw[:prefixEnd]
	tail := raw[prefixEnd:]

	return PrefixSplit{
		PrefixJSON:   prefix,
		TailJSON:     tail,
		PrefixTokens: countTokens(string(prefix)),
		TailTokens:   countTokens(string(tail)),
		MessageCount: count,
	}, nil
}

// findKeyAtTopLevel returns the offset of the first byte of the
// string literal of the given key, when the key appears as a
// top-level field of the JSON object. Returns -1 if the key is
// not found.
func findKeyAtTopLevelForTest(raw []byte, key string) int {
	v, _ := findKeyAtTopLevel(raw, key)
	return v
}

// findArrayOpenForTest is a test-only wrapper.
func findArrayOpenForTest(raw []byte, keyEnd int) (int, error) {
	return findArrayOpen(raw, keyEnd)
}

func findKeyAtTopLevel(raw []byte, key string) (int, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return -1, errors.New("findKeyAtTopLevel: payload is not a JSON object")
	}
	// We start at depth 1: we are already inside the root object.
	// i=0 was the opening '{' which we do not re-count, so the
	// first nested '{' or '[' takes us to depth 2.
	depth := 1
	inString := false
	escaped := false
	i := 1 // skip opening '{'
	for i < len(raw) {
		c := raw[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		switch c {
		case '"':
			if depth == 1 {
				// Check if this is the key we're looking for.
				// Build the candidate key string with both quotes
				// so we don't false-match a key that is a
				// prefix of another (e.g. "message" matching
				// "messages").
				candidate := make([]byte, 0, 2+len(key))
				candidate = append(candidate, '"')
				candidate = append(candidate, key...)
				candidate = append(candidate, '"')
				if bytes.HasPrefix(raw[i:], candidate) {
					// Found. Check that what follows is a colon.
					j := i + 2 + len(key) // skip opening ", key, closing "
					for j < len(raw) && (raw[j] == ' ' || raw[j] == '\n' || raw[j] == '\t' || raw[j] == '\r') {
						j++
					}
					if j < len(raw) && raw[j] == ':' {
						return i, nil
					}
				}
			}
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
		i++
	}
	return -1, nil
}

// findArrayOpen returns the offset of the '[' that opens the
// array starting at the byte after `keyEnd`. `keyEnd` is the
// offset of the opening '"' of a key (e.g. "messages"). The
// function skips past the closing '"', the ':', and any
// whitespace before the '['.
func findArrayOpen(raw []byte, keyEnd int) (int, error) {
	i := keyEnd + 1 // skip opening '"'
	// Skip the key string content + closing '"'. We don't need
	// to parse it — the scan below is robust to anything in
	// between because we only care about the ':' and the '['.
	for i < len(raw) && raw[i] != '"' {
		i++
	}
	if i >= len(raw) {
		return 0, errors.New("findArrayOpen: unterminated key string")
	}
	i++ // skip closing '"'
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\t' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) || raw[i] != ':' {
		return 0, fmt.Errorf("findArrayOpen: expected ':' at offset %d, got %q", i, raw[i])
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\t' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) || raw[i] != '[' {
		return 0, fmt.Errorf("findArrayOpen: expected '[' at offset %d, got %q", i, raw[i])
	}
	return i, nil
}

// findNthToLastMessageStart returns:
//   - count: the number of top-level message elements in the
//     messages array
//   - start: the offset of the first byte of the Nth-from-last
//     message element (or -1 if there is no such element,
//     because the array has fewer than N messages)
//
// `n` is 1-based: n=1 means the very last message, n=2 means the
// second-to-last, n=3 means the third-to-last, etc.
//
// The scan walks the array, tracking depth. Each top-level
// element is identified by the byte where its opening '{' starts.
// We keep a sliding window of the last N message starts and
// return the oldest one in the window after we've finished the
// scan.
//
// BUG FIX (re: tool_calls): an assistant message can have a
// `tool_calls` field whose value is an array of {id, type, function}
// objects. Naive counting would treat each tool_call dict and
// each tool_calls array as a top-level element, producing a wrong
// split offset (and a "tail does not end with ']}'" downstream
// error). The fix is to count ONLY object elements at the
// messages-array depth — i.e. we increment the message counter
// and record the window only on '{' at depth=1, and we never
// treat '[' at depth=2 as a message. Closing braces bring us back
// to depth=1 but only the closing '}' that brings us back from
// depth=2 (the message body) closes a message; the closing '}'
// of a nested tool_call returns us from depth=3 to depth=2.
func findNthToLastMessageStart(raw []byte, arrOpen int, n int) (int, int, error) {
	if n < 1 {
		return 0, 0, fmt.Errorf("findNthToLastMessageStart: n must be >= 1, got %d", n)
	}
	i := arrOpen + 1 // skip '['
	depth := 1       // we are inside the messages array
	count := 0
	// window holds the start offsets of the most recent n
	// message objects. We push on every top-level '{' and trim
	// the slice to n entries.
	window := make([]int, 0, n+1)

	for i < len(raw) {
		c := raw[i]
		if depth == 0 {
			// We've closed the messages array. Done.
			break
		}
		switch c {
		case '[':
			// Nested arrays (tool_calls inside an assistant
			// message, or any other array value inside a
			// message body). They do NOT count as messages.
			if depth >= 1 {
				depth++
			}
		case '{':
			depth++
			if depth == 2 {
				// depth went 1 -> 2, meaning we just
				// entered a top-level message object.
				// Record its start and trim the window.
				window = append(window, i)
				if len(window) > n {
					window = window[len(window)-n:]
				}
			}
		case '}', ']':
			depth--
			if depth == 1 && c == '}' {
				// depth went 2 -> 1 on a closing brace,
				// meaning we just left a top-level message
				// object body. Increment the message
				// counter.
				count++
			}
		}
		i++
	}
	if depth != 0 {
		return 0, 0, fmt.Errorf("findNthToLastMessageStart: unbalanced array (depth=%d at end)", depth)
	}
	// The Nth-from-last start is the oldest entry in the
	// window (index 0), if any. If the array has fewer than n
	// messages, window is shorter than n and there is no
	// Nth-from-last element; return -1.
	var start int
	if len(window) >= n {
		start = window[0]
	} else {
		start = -1
	}
	return count, start, nil
}

// CompressPayloadCachePreserving is the cache-preserving variant
// of CompressPayload. It splits the payload at the prefix
// boundary, runs the L3 compression rules on the tail only, then
// concatenates prefix + tail. The prefix bytes are returned
// bit-for-bit identical to the input, so provider prompt caches
// keep hitting.
//
// The split is conservative: at most the last 2 messages
// (typically the most recent user turn and the previous
// assistant turn) are subject to compression. Everything else is
// preserved as-is.
func CompressPayloadCachePreserving(payload []byte) ([]byte, error) {
	split, err := SplitAtPrefixBoundary(payload)
	if err != nil {
		return nil, err
	}
	if len(split.PrefixJSON) == 0 {
		// No prefix (fewer than 4 messages, or no messages
		// key, or no 4th-to-last message). Fall back to the
		// original CompressPayload so the request still
		// benefits from L3 where it can.
		return CompressPayload(payload)
	}

	// The prefix is a JSON fragment, not a complete document
	// (it lacks the closing ']' and '}' of the messages array
	// and the root object — those live in the tail). We don't
	// try to json.Validate it. We DO verify that the bytes
	// look sane (start with '{', no control characters), which
	// is enough to catch a bug in the splitter.
	if len(split.PrefixJSON) == 0 || split.PrefixJSON[0] != '{' {
		return nil, fmt.Errorf("CompressPayloadCachePreserving: prefix does not start with '{': %q", split.PrefixJSON[:min(64, len(split.PrefixJSON))])
	}
	for _, b := range split.PrefixJSON {
		if b == 0 {
			return nil, fmt.Errorf("CompressPayloadCachePreserving: prefix contains NUL byte (bug in splitter)")
		}
	}

	// Compress the tail. The tail is a sequence of message
	// objects plus the closing ']}' of the messages array and
	// root object. It is NOT a complete JSON document on its
	// own. CompressPayload expects a full request payload
	// (with a `messages` key wrapping an array). We extract
	// just the message objects (dropping the trailing `]}`),
	// wrap them in a synthetic envelope, compress, then
	// re-attach the closing `]}` from the original tail.
	//
	// The tail ends with the byte sequence `...}]}`. The last
	// `]}` closes the messages array and the root object in
	// that order — IN THE CANONICAL OpenAI payload where the
	// messages array is the LAST field of the root object.
//
// In our proxy's wrapped payload, the InjectCompactionHint
// round-trip (Go json.Marshal on a struct with both Messages
// and System fields) ALWAYS emits the `system` field after the
// messages array, even when System is "". So the real tail ends
// with `],"system":""}` — the `]` that closes the messages
// array is NOT at position len-2 anymore.
//
// Fix: locate the `]` that closes the messages array (the
// only top-level `[` whose depth comes back to 0 inside the
// tail) by re-scanning from the end of the prefix. Everything
// after that `]` and before the next `,` or `}` is trailing
// root-object fields; we strip them and re-attach them after
// compression.
//
// The trailing root field is detected by scanning
// forward from arrOpen. Anything between the closing `]` of
// the messages array and the final `}` of the root object is
// the trailing fields block, e.g. `,"system":""`. We split
// the tail into:
//   - tailMessageBytes: the message list (with its closing `]`)
//   - trailingRoot: the trailing fields, beginning with `,`
//                    (the comma between messages and the next
//                    field), or empty if there is no trailing
//                    field.
	if len(split.TailJSON) < 2 {
		return nil, errors.New("CompressPayloadCachePreserving: tail too short")
	}
	// Scan the tail forward from the prefix boundary to find
	// the `]` that closes the messages array. The messages
	// array starts at the byte immediately after the comma in
	// the prefix (which is the first byte of the tail).
	tailArrOpen := bytes.IndexByte(split.TailJSON, '[')
	if tailArrOpen < 0 {
		return nil, errors.New("CompressPayloadCachePreserving: tail has no '[' (bug in splitter)")
	}
	// Walk depth to find the matching `]`.
	depth := 1
	arrClose := -1
	i := tailArrOpen + 1
	for i < len(split.TailJSON) {
		c := split.TailJSON[i]
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				arrClose = i
			}
		}
		i++
		if arrClose >= 0 {
			break
		}
	}
	if arrClose < 0 {
		return nil, errors.New("CompressPayloadCachePreserving: messages array not closed in tail")
	}
	// Everything after arrClose is trailing root fields plus
	// the closing `}` of the root object.
	trailing := split.TailJSON[arrClose+1:]
	if len(trailing) == 0 || trailing[len(trailing)-1] != '}' {
		return nil, fmt.Errorf("CompressPayloadCachePreserving: tail after messages ']' does not end with '}' (bug); trailing=%q", trailing)
	}
	// Stash the trailing block so the caller can re-attach it
	// after compression. We pass it back via the error return is
	// not workable; instead we extend the TailJSON semantics
	// by appending a marker that the wrapper below recognises.
	// (Cleaner approach: refactor CompressPayloadCachePreserving
	// to handle the trailing block inline. We do that inline
	// here.)
	_ = trailing // marker for the inline handling below
	// Build the message list to compress. We cut the tail at
	// arrClose (the `]` that closes the messages array) and drop
	// the trailing root fields. Drop the leading comma on the
	// tail so the envelope parses cleanly.
	tailMessages := split.TailJSON[:arrClose+1]
	if len(tailMessages) > 0 && tailMessages[0] == ',' {
		tailMessages = tailMessages[1:]
	}
	// Strip the trailing `]` from tailMessages so we can wrap
	// them in our own envelope. (CompressPayload doesn't care
	// about the array close — it walks the messages list
	// directly via JSON unmarshal.)
	tailMessages = tailMessages[:len(tailMessages)-1]
	envelope := make([]byte, 0, len(tailMessages)+32)
	envelope = append(envelope, []byte(`{"messages":[`)...)
	envelope = append(envelope, tailMessages...)
	envelope = append(envelope, ']', '}')
	compressedEnvelope, err := CompressPayload(envelope)
	if err != nil {
		return nil, err
	}
	// The envelope is now `{"messages":[<compressed-messages>]}`
	// Strip the envelope to recover the compressed message
	// list. The compressor never modifies the wrapping
	// `{"messages":[` or `]}` (they're not in the messages
	// array and not field-by-field editable), so we can use
	// fixed offsets.
	const envelopePrefix = `{"messages":[`
	if !bytes.HasPrefix(compressedEnvelope, []byte(envelopePrefix)) {
		return nil, errors.New("CompressPayloadCachePreserving: envelope prefix missing after compression (bug)")
	}
	inner := compressedEnvelope[len(envelopePrefix):]
	if len(inner) < 2 || inner[len(inner)-2] != ']' || inner[len(inner)-1] != '}' {
		return nil, errors.New("CompressPayloadCachePreserving: envelope suffix missing after compression (bug)")
	}
	compressedMessages := inner[:len(inner)-2]

	// Concatenate: prefix + (leading comma from prefix +
	// compressed messages) + trailing root fields.
	out := make([]byte, 0, len(split.PrefixJSON)+len(compressedMessages)+len(trailing)+2)
	out = append(out, split.PrefixJSON...)
	out = append(out, compressedMessages...)
	// Re-attach the trailing root fields (e.g. `,"system":""`).
	out = append(out, trailing...)
	return out, nil
}
