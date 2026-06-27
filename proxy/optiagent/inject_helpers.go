// Package optiagent — shared injection helpers.
//
// This file holds utilities used by multiple hooks to mutate the
// request payload in a cache-friendly way (preserving the JSON
// shape and the byte-stable prefix when possible).
//
// Functions here MUST be pure (no Redis, no globals) so they're
// trivially unit-testable.

package optiagent

import (
	"encoding/json"
	"fmt"
)

// InjectSystemWarningCompat is the hook-callable equivalent of the
// legacy internal/handlers/proxy.go:injectSystemWarning() function.
//
// It appends a [SYSTEM WARNING: ...] block to the last message of
// the payload, nudging the LLM out of a tool loop without
// triggering the kill switch. Returns nil if the payload is not
// modifiable (parse failure, no messages).
//
// Behavioural note: the text is byte-stable across releases to keep
// the Anthropic / OpenAI prompt-cache prefix unchanged when the
// warning is repeated. Any edit to the warning text = cache bust.
//
// Used by:
//   - hook_fingerprint.go (soft-loop injection)
//   - future hooks (kill switch self-correction hint)
func InjectSystemWarningCompat(payload []byte, toolName string) []byte {
	if len(payload) == 0 {
		return nil
	}

	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil
	}

	messagesRaw, ok := body["messages"].([]interface{})
	if !ok || len(messagesRaw) == 0 {
		return nil
	}

	lastMsgRaw := messagesRaw[len(messagesRaw)-1]
	lastMsg, ok := lastMsgRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	warningText := fmt.Sprintf("\n\n[SYSTEM WARNING: The proxy intercepted your request because you are caught in a loop. You have repeated the tool '%s' with identical arguments too many times. You MUST change your strategy immediately. Do not repeat the same action.]", toolName)

	if contentStr, ok := lastMsg["content"].(string); ok {
		lastMsg["content"] = contentStr + warningText
	} else if contentArr, ok := lastMsg["content"].([]interface{}); ok {
		contentArr = append(contentArr, map[string]interface{}{
			"type": "text",
			"text": warningText,
		})
		lastMsg["content"] = contentArr
	} else {
		warningMsg := map[string]interface{}{
			"role":    "user",
			"content": warningText,
		}
		body["messages"] = append(messagesRaw, warningMsg)
	}

	newPayload, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	return newPayload
}
