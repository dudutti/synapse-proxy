package optiagent

import (
	"encoding/json"
)

// CompactionHint is a short, neutral reminder prepended to the first
// system message. Keep it short and dry — long marketing-style text is
// flagged by downstream safety filters as a possible prompt injection,
// and the agent will refuse to trust the conversation context.
const CompactionHint = "(Earlier tool results in this transcript may be truncated.)"

// InjectCompactionHint walks the messages list and prepends CompactionHint
// to the first system message. If there is no system message, it creates
// one. The returned payload is a new byte slice; the original is left
// untouched.
//
// Returns the input unchanged if it cannot be parsed as JSON.
func InjectCompactionHint(payload []byte) []byte {
	var body struct {
		Messages []map[string]interface{} `json:"messages"`
		System   string                   `json:"system"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}

	hint := CompactionHint

	if len(body.Messages) > 0 {
		inserted := false
		if body.Messages[0]["role"] == "system" {
			body.Messages[0] = prependToSystemMessage(body.Messages[0], hint)
			inserted = true
		}
		if !inserted {
			newSys := map[string]interface{}{
				"role":    "system",
				"content": hint,
			}
			body.Messages = append([]map[string]interface{}{newSys}, body.Messages...)
		}
	} else if body.System != "" {
		body.System = hint + "\n\n" + body.System
	} else {
		body.Messages = []map[string]interface{}{
			{"role": "system", "content": hint},
		}
	}

	out, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return out
}

func prependToSystemMessage(m map[string]interface{}, hint string) map[string]interface{} {
	switch c := m["content"].(type) {
	case string:
		m["content"] = hint + "\n\n" + c
	case []interface{}:
		newPart := map[string]interface{}{"type": "text", "text": hint}
		m["content"] = append([]interface{}{newPart}, c...)
	default:
		m["content"] = hint
	}
	return m
}
