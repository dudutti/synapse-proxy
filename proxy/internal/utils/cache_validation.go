package utils

import (
	"bytes"
	"encoding/json"
	"strings"
)

// IsCachedResponseAnError peeks at a cached upstream response and returns
// true if it looks like an error rather than a successful completion.
//
// Two providers are sniffed:
//   - MiniMax / Anthropic / OpenAI-style: top-level `error` object
//   - MiniMax-style: `base_resp.status_code != 0`
//
// Used by the proxy to invalidate poisoned cache entries that would
// otherwise be replayed forever to clients (Hermes etc.) that interpret
// an empty content field as a fatal failure.
func IsCachedResponseAnError(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	// Fast path: look for the literal error markers without a full unmarshal
	if bytes.Contains(body, []byte(`"error":`)) {
		// Distinguish from a body that mentions "error" in a normal field
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(body, &probe); err == nil {
			if _, hasErr := probe["error"]; hasErr {
				return true
			}
		}
	}
	// MiniMax base_resp.status_code != 0
	if bytes.Contains(body, []byte(`"status_code":2013`)) ||
		bytes.Contains(body, []byte(`"status_code":-1`)) ||
		bytes.Contains(body, []byte(`"status_code":401`)) ||
		bytes.Contains(body, []byte(`"status_code":429`)) {
		return true
	}
	// SSE error frames: data: {"error": ...}
	if bytes.HasPrefix(body, []byte("data:")) && bytes.Contains(body, []byte(`"error":`)) {
		return true
	}
	// Empty content with finish_reason=stop is a soft failure that
	// breaks agent loops: Hermes interprets it as a hard error.
	if bytes.Contains(body, []byte(`"content":""`)) &&
		bytes.Contains(body, []byte(`"finish_reason":"stop"`)) {
		// Heuristic: empty content is almost always a model-side error.
		// Make sure there are no tool_calls or audio_content indicating a
		// legitimate empty response.
		if !bytes.Contains(body, []byte(`"tool_calls"`)) &&
			!bytes.Contains(body, []byte(`"audio_content":"`)) {
			return true
		}
	}
	// Generic substring safety net
	if strings.Contains(string(body), `"status_msg":"invalid params"`) {
		return true
	}
	return false
}

// RestampModel replaces the top-level "model" field in a JSON response
// body with the given client-facing model name. It operates on raw bytes
// to keep streaming responses as close to upstream as possible (no full
// unmarshal → no re-marshalling loss of unknown fields).
//
// Behaviour:
//   - Single JSON object: rewrites "model":"..." inline.
//   - SSE stream: rewrites every "model":"..." inside the `data:` payloads.
//   - No-op if the model name is already correct or if the body has no
//     model field.
//
// The returned byte slice is safe to write to the response directly.
func RestampModel(body []byte, newModel string) []byte {
	if len(body) == 0 || newModel == "" {
		return body
	}
	// Quick reject: if the body already mentions the target model, return
	// as-is to avoid pointless allocation in the hot path.
	if bytes.Contains(body, []byte(`"model":"`+newModel+`"`)) {
		return body
	}

	// Find every "model":"<value>" occurrence and replace <value> with
	// newModel. The regex is kept simple on purpose: model names do not
	// contain " or \, so a raw byte scan is safe and far cheaper than
	// json.Unmarshal.
	var out bytes.Buffer
	out.Grow(len(body) + 32)
	i := 0
	for i < len(body) {
		// Look for the next "model":"  occurrence
		idx := bytes.Index(body[i:], []byte(`"model":"`))
		if idx < 0 {
			out.Write(body[i:])
			break
		}
		out.Write(body[i : i+idx])
		out.WriteString(`"model":"`)
		out.WriteString(newModel)
		// Skip past the old value (find next unescaped quote)
		j := i + idx + len(`"model":"`)
		for j < len(body) {
			if body[j] == '\\' && j+1 < len(body) {
				j += 2
				continue
			}
			if body[j] == '"' {
				j++
				break
			}
			j++
		}
		i = j
	}
	return out.Bytes()
}
