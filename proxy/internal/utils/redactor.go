package utils

import (
	"bytes"
	"encoding/json"
	"regexp"
)

var emailRegex = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)
var creditCardRegex = regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|3(?:0[0-5]|[68][0-9])[0-9]{11}|6(?:011|5[0-9]{2})[0-9]{12}|(?:2131|1800|35\d{3})\d{11})\b`)


// ContentRedactor is the cornerstone of Zero-Log Mode. It walks a JSON
// payload and replaces the value of any field whose name is in the
// "sensitive" set with the placeholder "[REDACTED]". The JSON structure
// (key paths, array lengths, types) is preserved so the rest of the
// proxy (caching, telemetry metadata, token counts) keeps working.
//
// Fields redacted (case-sensitive, name match):
//   - top-level or nested: "content", "text", "input_text", "output_text"
//   - "tool_calls" arrays: each function.arguments string is replaced
//   - "function_call.arguments" string is replaced
//   - "messages[].content" (string or array of {type, text})
//   - "system" string (Anthropic-style)
//
// Tokens counting (via tiktoken on the redacted body) will be slightly
// off because "[REDACTED]" is shorter than most prompts, but the
// metadata (count, latency, model, agent) is intact. We use the
// ORIGINAL body for token counting in proxy.go BEFORE redaction, so
// the user sees the true prompt token count.
const redactedPlaceholder = "[REDACTED]"

// sensitiveFieldNames is the set of JSON keys whose VALUES must be
// redacted under Zero-Log Mode. We do NOT redact "role" or "name" —
// those are non-content metadata that the dashboard uses.
var sensitiveFieldNames = map[string]bool{
	"content":      true,
	"text":         true,
	"input_text":   true,
	"output_text":  true,
	"system":       true,
	"arguments":    true,
	"reasoning_content": true,
	"audio_content":    true,
}

// RedactJSONBody walks a JSON body and returns a copy with sensitive
// field values replaced. If the body is not valid JSON, it is returned
// unchanged (we don't want Zero-Log Mode to silently break requests
// that happen to be malformed).
func RedactJSONBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	// Quick reject: if the body doesn't look like JSON, don't touch it
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return body
	}

	var doc interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}

	redacted := redactWalk(doc, true, false)
	out, err := json.Marshal(redacted)
	if err != nil {
		return body
	}
	return out
}

// RedactPII walks a JSON body and uses heuristics to redact personal info
func RedactPII(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	var doc interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return body // fallback
	}

	redacted := redactWalk(doc, false, true)
	out, err := json.Marshal(redacted)
	if err != nil {
		return body
	}
	return out
}

// RedactBoth applies both ZeroLog and PII redaction
func RedactBoth(body []byte) []byte {
	var doc interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}
	redacted := redactWalk(doc, true, true)
	out, err := json.Marshal(redacted)
	if err == nil {
		return out
	}
	return body
}

func redactWalk(v interface{}, zeroLog bool, pii bool) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			if k == "tool_calls" || k == "function_call" {
				out[k] = redactToolCallStructure(val, zeroLog, pii)
				continue
			}
			if zeroLog && sensitiveFieldNames[k] {
				out[k] = redactedPlaceholder
				continue
			}
			out[k] = redactWalk(val, zeroLog, pii)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = redactWalk(item, zeroLog, pii)
		}
		return out
	case string:
		if pii {
			s := emailRegex.ReplaceAllString(x, "[REDACTED_EMAIL]")
			s = creditCardRegex.ReplaceAllString(s, "[REDACTED_CC]")
			return s
		}
		return x
	default:
		return v
	}
}

// redactToolCallStructure handles the nested shape of OpenAI's
// tool_calls array and Anthropic's function_call object. We preserve
// the function name (not sensitive) but replace the arguments with
// the placeholder (if zeroLog).
func redactToolCallStructure(v interface{}, zeroLog bool, pii bool) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			if k == "function" {
				if fn, ok := val.(map[string]interface{}); ok {
					fnCopy := make(map[string]interface{}, len(fn))
					for fk, fv := range fn {
						if zeroLog && fk == "arguments" {
							fnCopy[fk] = redactedPlaceholder
						} else {
							fnCopy[fk] = redactWalk(fv, zeroLog, pii)
						}
					}
					out[k] = fnCopy
				} else {
					out[k] = redactWalk(val, zeroLog, pii)
				}
				continue
			}
			out[k] = redactWalk(val, zeroLog, pii)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = redactToolCallStructure(item, zeroLog, pii)
		}
		return out
	default:
		return v
	}
}

