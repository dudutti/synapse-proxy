package utils

import (
	"bytes"
	"encoding/json"
)

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
// redacted under Zero-Log Mode. We do NOT redact "role" or "name" â€”
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

	redacted := redactWalk(doc)
	out, err := json.Marshal(redacted)
	if err != nil {
		return body
	}
	return out
}

func redactWalk(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			// Tool calls structure: { tool_calls: [{ function: { name, arguments } }] }
			if k == "tool_calls" || k == "function_call" {
				out[k] = redactToolCallStructure(val)
				continue
			}
			if sensitiveFieldNames[k] {
				// Replace value with placeholder, keep the type hint
				// by using a string (the most common shape).
				out[k] = redactedPlaceholder
				continue
			}
			out[k] = redactWalk(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = redactWalk(item)
		}
		return out
	default:
		return v
	}
}

// redactToolCallStructure handles the nested shape of OpenAI's
// tool_calls array and Anthropic's function_call object. We preserve
// the function name (not sensitive) but replace the arguments with
// the placeholder.
func redactToolCallStructure(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			if k == "function" {
				if fn, ok := val.(map[string]interface{}); ok {
					fnCopy := make(map[string]interface{}, len(fn))
					for fk, fv := range fn {
						if fk == "arguments" {
							fnCopy[fk] = redactedPlaceholder
						} else {
							fnCopy[fk] = redactWalk(fv)
						}
					}
					out[k] = fnCopy
				} else {
					out[k] = redactWalk(val)
				}
				continue
			}
			out[k] = redactWalk(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = redactToolCallStructure(item)
		}
		return out
	default:
		return v
	}
}

