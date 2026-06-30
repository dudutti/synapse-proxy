// Package compress — L3 payload compression for the local-client
// proxy.
//
// The standalone local-client proxy forwards requests to a
// user-chosen upstream (Ollama, LM Studio, OpenAI, Anthropic,
// DeepSeek, Minimax, etc.). Most of those upstreams don't
// have a provider-side prompt cache, so the only savings we
// can pass through are byte-level: trim stale tool outputs,
// strip chain-of-thought blocks, drop repeated tool results,
// and preserve a todo-list carve-out so multi-turn agent
// plans stay intact.
//
// Two public entry points:
//
//   - Compress(payload) -> bytes
//       Unmarshal the payload, apply the L3 rules, remarshal.
//       Returns the original payload unchanged on parse error.
//
//   - CompressBytePreserving(payload) -> (bytes, savings)
//       Unmarshal the payload to identify message boundaries
//       and content field offsets, then replace just those
//       content fields in the ORIGINAL payload bytes. Keeps
//       key order, whitespace, and surrounding structure
//       byte-identical so the upstream's provider-side cache
//       (Anthropic, OpenAI automatic, Minimax cache-read)
//       keeps hitting across requests.
//
// Carve-out: any tool payload whose content begins with an
// obvious todo/tasks pattern ({"status":"in_progress"...} etc.)
// is preserved verbatim so Hermes-style multi-turn agents
// don't lose track of their plan.
package compress

import (
	"bytes"
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"unicode/utf8"
)

const toolOutputKeepBytes = 200

var thinkingBlockRe = regexp.MustCompile(`(?s)<thought>.*?</thought>|<thinking>.*?</thinking>|<scratchpad>.*?</scratchpad>`)

var todoListAnchors = []string{
	`"status":"pending"`,
	`"status":"in_progress"`,
	`"status":"completed"`,
	`"todos":`,
	`"tasks":`,
	`"checklist":`,
	`"todoList":`,
	`- [ ]`,
	`- [x]`,
}

func looksLikeTodoList(content string) bool {
	if content == "" {
		return false
	}
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	for _, sig := range todoListAnchors {
		if strings.Contains(head, sig) {
			return true
		}
	}
	return false
}

// Compress applies the L3 rules by unmarshalling and
// re-marshalling. Returns the original payload unchanged if
// anything goes wrong. Total bytes saved are returned too so
// the caller can record them in local analytics.
func Compress(payload []byte) ([]byte, int) {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload, 0
	}
	messagesRaw, _ := body["messages"].([]interface{})
	if messagesRaw == nil {
		return payload, 0
	}
	msgCount := len(messagesRaw)

	for i, msgIntf := range messagesRaw {
		msg, ok := msgIntf.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		var contentStr string
		var hasContent bool
		isContentArray := false

		if c, ok := msg["content"].(string); ok {
			contentStr = c
			hasContent = true
		} else if cArr, _ := msg["content"].([]interface{}); len(cArr) > 0 {
			isContentArray = true
			for _, blockIntf := range cArr {
				if block, ok := blockIntf.(map[string]interface{}); ok {
					if t, _ := block["type"].(string); t == "text" {
						if text, ok := block["text"].(string); ok {
							contentStr += text
							hasContent = true
						}
					}
				}
			}
		}

		_, _ = msg["name"].(string)
		isRecent := i >= msgCountMinus2(msgCount)

		if role == "assistant" && !isRecent && hasContent {
			if isContentArray {
				cArr := msg["content"].([]interface{})
				for j, blockIntf := range cArr {
					if block, ok := blockIntf.(map[string]interface{}); ok {
						if t, _ := block["type"].(string); t == "text" {
							if text, ok := block["text"].(string); ok {
								block["text"] = thinkingBlockRe.ReplaceAllString(text, "[Pruned Thought Process]")
								cArr[j] = block
							}
						}
					}
				}
				msg["content"] = cArr
			} else {
				msg["content"] = thinkingBlockRe.ReplaceAllString(contentStr, "[Pruned Thought Process]")
			}
		}

		if role == "assistant" && !isRecent {
			if _, has := msg["reasoning_content"]; has {
				delete(msg, "reasoning_content")
			}
			if _, has := msg["reasoning"]; has {
				delete(msg, "reasoning")
			}
		}

		if role == "tool" || role == "function" {
			isTodo := looksLikeTodoList(contentStr)
			if !isTodo && !isRecent && hasContent && len(contentStr) > 200 {
				if len(contentStr) > 200+50 {
					if isContentArray {
						cArr := msg["content"].([]interface{})
						for j, blockIntf := range cArr {
							if block, ok := blockIntf.(map[string]interface{}); ok {
								if t, _ := block["type"].(string); t == "text" {
									if text, ok := block["text"].(string); ok && len(text) > 200 {
										block["text"] = text[:200] + "\n[…truncated by Synapse L3…]"
										cArr[j] = block
										msg["content"] = cArr[:j+1]
										break
									}
								}
							}
						}
					} else {
						msg["content"] = contentStr[:200] + "\n[…truncated by Synapse L3…]"
					}
				}
			}
		}
	}

	out, err := marshalDeterministic(body)
	if err != nil {
		return payload, 0
	}
	saved := len(payload) - len(out)
	if saved < 0 {
		saved = 0
	}
	return out, saved
}

// CompressBytePreserving applies the L3 rules at the byte
// level. It first parses the payload to find each message's
// top-level "content" field offset, then replaces only those
// offsets in the original payload. Key order, whitespace, and
// surrounding structure stay byte-identical so the upstream
// provider cache (or a parallel cache_control layer) keeps
// hitting.
//
// Returns the new payload and the bytes saved (0 if nothing
// changed). On parse errors the original payload is returned
// unchanged.
func CompressBytePreserving(payload []byte) ([]byte, int) {
	// Use json.Unmarshal to find message indices, then walk
	// the raw bytes to locate each content field's offset.
	var body struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload, 0
	}
	if len(body.Messages) == 0 {
		return payload, 0
	}

	// Locate each message object's raw bytes in the payload.
	messageRanges := findTopLevelMessageRanges(payload)
	if len(messageRanges) != len(body.Messages) {
		return payload, 0
	}

	var repls []byteReplacement

	for i, raw := range body.Messages {
		if i >= msgCountMinus2(len(body.Messages)) {
			break
		}
		r := messageRanges[i]
		var msg struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			Name       string          `json:"name"`
			ToolCallID string          `json:"tool_call_id"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		objBytes := payload[r.Start : r.End+1]
		cStart, cEnd, ok := findTopLevelContentField(objBytes)
		if !ok {
			continue
		}
		absCS := r.Start + cStart
		absCE := r.Start + cEnd
		rawContent := payload[absCS+1 : absCE]

		switch msg.Role {
		case "assistant":
			str, ok1 := unquoteJSONString(rawContent)
			if !ok1 {
				continue
			}
			newStr := thinkingBlockRe.ReplaceAllString(str, "")
			if newStr == str {
				continue
			}
			repls = append(repls, byteReplacement{absCS, absCE, jsonEscape(newStr)})
		case "tool", "function":
			head := rawContent
			if len(head) > 512 {
				head = head[:512]
			}
			todo := false
			for _, anchor := range todoListAnchors {
				if bytes.Contains(head, []byte(anchor)) {
					todo = true
					break
				}
			}
			if todo {
				continue
			}
			str, ok1 := unquoteJSONString(rawContent)
			if !ok1 {
				continue
			}
			if utf8.RuneCountInString(str) <= toolOutputKeepBytes+50 {
				continue
			}
			truncated := truncateRunes(str, toolOutputKeepBytes) +
				"\n[…truncated by Synapse L3…]"
			repls = append(repls, byteReplacement{absCS, absCE, jsonEscape(truncated)})
		}
	}

	blankRepls := blankRepeatedToolResults(payload, messageRanges, body.Messages)
	repls = append(repls, blankRepls...)

	totalSaved := 0
	for i := len(repls) - 1; i >= 0; i-- {
		r := repls[i]
		if r.end >= len(payload) {
			log.Printf("[compress] replacement out of bounds: end=%d len=%d", r.end, len(payload))
			continue
		}
		newOut := make([]byte, 0, len(payload)-(r.end-r.start+1)+len(r.with))
		newOut = append(newOut, payload[:r.start]...)
		newOut = append(newOut, r.with...)
		newOut = append(newOut, payload[r.end+1:]...)
		payload = newOut
		totalSaved += (r.end - r.start + 1) - len(r.with)
	}
	if totalSaved <= 0 {
		return payload, 0
	}
	return payload, totalSaved
}

// msgCountMinus2 returns the index of the last non-recent
// message (i.e. n-2). For n=0, returns 0.
func msgCountMinus2(n int) int {
	if n < 2 {
		return 0
	}
	return n - 2
}

// findTopLevelMessageRanges returns the [start, end]
// (inclusive) byte offsets of each top-level message object
// in the `messages` array. Returns nil if the array is not
// found or the payload is malformed.
func findTopLevelMessageRanges(payload []byte) []struct{ Start, End int } {
	idx := bytes.Index(payload, []byte(`"messages"`))
	if idx < 0 {
		return nil
	}
	i := idx + len(`"messages"`)
	for i < len(payload) && (payload[i] == ' ' || payload[i] == '\t' ||
		payload[i] == '\n' || payload[i] == '\r' || payload[i] == ':') {
		i++
	}
	if i >= len(payload) || payload[i] != '[' {
		return nil
	}
	i++
	depth := 1
	var result []struct{ Start, End int }
	for i < len(payload) {
		c := payload[i]
		if c == '"' {
			k := i + 1
			for k < len(payload) {
				if payload[k] == '\\' {
					k += 2
					continue
				}
				if payload[k] == '"' {
					break
				}
				k++
			}
			i = k + 1
			continue
		}
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return result
			}
		case '{':
			if depth == 1 {
				objStart := i
				depth2 := 1
				k := i + 1
				for k < len(payload) {
					ck := payload[k]
					if ck == '"' {
						kk := k + 1
						for kk < len(payload) {
							if payload[kk] == '\\' {
								kk += 2
								continue
							}
							if payload[kk] == '"' {
								break
							}
							kk++
						}
						k = kk + 1
						continue
					}
					switch ck {
					case '{':
						depth2++
					case '}':
						depth2--
						if depth2 == 0 {
							result = append(result, struct{ Start, End int }{objStart, k})
							i = k
							goto continueOuter
						}
					}
					k++
				}
			}
		}
	continueOuter:
		i++
	}
	return result
}

// findTopLevelContentField returns the offsets of the
// opening and closing quote of the top-level "content" string
// field inside objBytes (a single message object). Returns
// ok=false if the field is absent or is a non-string value.
//
// Algorithm: find each `"content"` occurrence. The
// occurrence is at the TOP level of the message object when
// (1) the byte that immediately precedes the opening `"`
// of `"content"` is either `{` (first field) or `,`
// (subsequent field) at the message root, AND (2) there is no
// unmatched `{` or `[` between that delimiter and the needle
// (which would mean we're inside a nested object or array).
//
// We don't need full string-aware parsing here because the
// only characters that can appear between a `,` (or `{`)
// and `"content"` are whitespace characters (which we skip
// over). If the byte preceding the needle is something other
// than `{` or `,`, the needle is inside a string body (e.g.
// a tool_call.function.arguments value containing the literal
// substring "content"), and we skip it.
func findTopLevelContentField(objBytes []byte) (int, int, bool) {
	const needle = `"content"`
	searchFrom := 0
	for {
		idx := bytes.Index(objBytes[searchFrom:], []byte(needle))
		if idx < 0 {
			return 0, 0, false
		}
		absIdx := searchFrom + idx

		// Walk backward from absIdx to find the most recent
		// non-whitespace character. That character is the
		// field delimiter (`{` or `,`).
		k := absIdx - 1
		for k >= 0 && (objBytes[k] == ' ' || objBytes[k] == '\t' ||
			objBytes[k] == '\n' || objBytes[k] == '\r') {
			k--
		}
		if k < 0 {
			return 0, 0, false
		}
		if objBytes[k] != '{' && objBytes[k] != ',' {
			// Not a field delimiter (probably a string body
			// containing the substring "content"). Skip and
			// look for the next occurrence.
			searchFrom = absIdx + len(needle)
			continue
		}
		// Check there is no unmatched `{` or `[` between
		// k+1 and absIdx. (Whitespace-only between, so this
		// is trivially true. Kept for future-proofing.)
		hasNested := false
		for j := k + 1; j < absIdx; j++ {
			if objBytes[j] == '{' || objBytes[j] == '[' {
				hasNested = true
				break
			}
		}
		if hasNested {
			searchFrom = absIdx + len(needle)
			continue
		}
		// Top-level content. Walk to the value's opening
		// quote.
		i := absIdx + len(needle)
		if i < len(objBytes) && objBytes[i] == '"' {
			i++
		}
		for i < len(objBytes) && (objBytes[i] == ' ' || objBytes[i] == '\t' ||
			objBytes[i] == '\n' || objBytes[i] == '\r' || objBytes[i] == ':') {
			i++
		}
		if i >= len(objBytes) || objBytes[i] != '"' {
			return 0, 0, false
		}
		qStart := i
		i++
		for i < len(objBytes) {
			c := objBytes[i]
			if c == '\\' {
				i += 2
				continue
			}
			if c == '"' {
				return qStart, i, true
			}
			i++
		}
		return 0, 0, false
	}
}

// blankRepeatedToolResults returns content-replacement actions
// that blank the content of the 3rd+ consecutive same-name
// tool result, so the conversation history doesn't grow
// unbounded. Each replacement is an empty `""` string.
func blankRepeatedToolResults(
	payload []byte,
	ranges []struct{ Start, End int },
	messages []json.RawMessage,
) []byteReplacement {
	var out []byteReplacement
	prevName := ""
	count := 0
	n := len(messages)
	for i, raw := range messages {
		if i >= n-2 {
			// Don't blank the last 2 messages (recent).
			break
		}
		var msg struct {
			Role string `json:"role"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Role != "tool" && msg.Role != "function" {
			prevName = ""
			count = 0
			continue
		}
		name := msg.Name
		if name == "" {
			name = prevName
		}
		if name == prevName && prevName != "" {
			count++
			if count > 2 {
				r := ranges[i]
				objBytes := payload[r.Start : r.End+1]
				cStart, cEnd, ok := findTopLevelContentField(objBytes)
				if ok {
					absCS := r.Start + cStart
					absCE := r.Start + cEnd
					out = append(out, byteReplacement{absCS, absCE, []byte(`""`)})
				}
			}
		} else {
			prevName = name
			count = 1
		}
	}
	return out
}

// byteReplacement is a single byte-level splice: replace
// payload[start..end] (inclusive) with `with`. Multiple
// replacements are applied from last to first so byte offsets
// stay valid.
type byteReplacement struct {
	start, end int
	with       []byte
}

func unquoteJSONString(raw []byte) (string, bool) {
	var s string
	wrapped := make([]byte, 0, len(raw)+2)
	wrapped = append(wrapped, '"')
	wrapped = append(wrapped, raw...)
	wrapped = append(wrapped, '"')
	if err := json.Unmarshal(wrapped, &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonEscape(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}

func marshalDeterministic(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	count := 0
	for i := range s {
		if count == maxRunes {
			return s[:i]
		}
		count++
	}
	return s
}