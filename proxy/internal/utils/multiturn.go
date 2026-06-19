package utils

import (
	"crypto/sha1"
	"encoding/hex"
)

// MultiturnSignature describes what we know about a request's place
// in a multi-turn conversation. We compute this on every request so
// the proxy can:
//   - Group consecutive requests that share the same conversation
//     signature (system prompt + tool set) into one "natural session"
//     even when the agent never sends an X-Session-Id header.
//   - Show "Tour N" in the dashboard per row, so the user can see
//     where they are in a conversation.
//   - Detect agent drift: a session where turnCount grows fast is
//     suspicious (the agent is retrying without making progress).
//
// The signature is intentionally cheap (just a hash of the system
// prompt + tool names) so we can compute it on every request without
// slowing down the proxy.
type MultiturnSignature struct {
	// TurnCount is the number of user/assistant exchanges in this
	// request's messages array. 0 means the request is a single
	// one-shot call (no prior context); 1 means first follow-up; etc.
	// System messages are not counted as turns.
	TurnCount int

	// ConvSignature is a short hash of (system prompt + tool names).
	// Two requests with the same ConvSignature are very likely part
	// of the same conversation, even if no explicit sessionId is
	// present. Format: "conv-<8 hex chars>".
	ConvSignature string
}

// MultiturnSign walks the messages array and the system/tool fields
// to compute the signature. Returns a zero-value MultiturnSignature
// if the body has no recognizable structure.
//
// The counting rule is "1 turn = 1 user message + 1 assistant
// message in the history". A request with messages =
// [system, user] has TurnCount=0 (it's the first turn, no prior
// assistant). A request with messages = [system, user, assistant,
// user] has TurnCount=1 (one full exchange already done, this is
// the second user turn). And so on.
//
// We intentionally count "completed" exchanges (user + assistant
// pairs) rather than user messages alone. The proxy's value-add is
// to make the *conversation* visible, and a half-finished exchange
// (user said X, no assistant reply yet) doesn't represent prior
// progress in the same way.
func MultiturnSign(body map[string]any) MultiturnSignature {
	if body == nil {
		return MultiturnSignature{}
	}

	// 1. Walk messages to count user/assistant exchanges.
	messages, _ := body["messages"].([]any)
	turnCount := 0
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role == "assistant" {
			turnCount++ // each assistant reply = one completed exchange
		}
	}

	// 2. Build the conversation signature from the parts that
	// don't change between turns of the same conversation:
	//   - System prompt (top-level "system" string in Anthropic
	//     style, or first message with role=system in OpenAI style)
	//   - Tool definitions (each tool's name + first ~200 chars of
	//     description is enough — descriptions are stable per agent)
	sysPrompt := extractSystemPrompt(body)
	tools := extractToolNames(body)

	// Hash the concatenation. We use sha1 (not crypto/sha256) because
	// the result is only used for grouping, not for security — sha1
	// is fine and faster.
	hasher := sha1.New()
	hasher.Write([]byte(sysPrompt))
	hasher.Write([]byte{0})
	hasher.Write([]byte(tools))
	sum := hasher.Sum(nil)
	sig := "conv-" + hex.EncodeToString(sum[:4]) // 8 hex chars

	return MultiturnSignature{
		TurnCount:     turnCount,
		ConvSignature: sig,
	}
}

// extractSystemPrompt finds the system prompt in either the
// Anthropic-style top-level "system" string or the OpenAI-style
// first message with role=system. Returns "" if neither is set.
func extractSystemPrompt(body map[string]any) string {
	if s, ok := body["system"].(string); ok && len(s) > 0 {
		return s
	}
	if msgs, ok := body["messages"].([]any); ok {
		for _, m := range msgs {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			role, _ := mm["role"].(string)
			if role == "system" {
				if c, ok := mm["content"].(string); ok {
					return c
				}
				// Anthropic passes content as a list of blocks;
				// concatenate the text parts.
				if arr, ok := mm["content"].([]any); ok {
					out := ""
					for _, b := range arr {
						if bm, ok := b.(map[string]any); ok {
							if t, _ := bm["type"].(string); t == "text" {
								if txt, ok := bm["text"].(string); ok {
									out += txt
								}
							}
						}
					}
					if out != "" {
						return out
					}
				}
			}
		}
	}
	return ""
}

// extractToolNames returns a stable string of the tool/function
// names defined in the request, sorted alphabetically. Two
// requests with the same tools but different argument lists still
// produce the same output (we only look at the schema, not the
// current call).
func extractToolNames(body map[string]any) string {
	out := ""
	tools, ok := body["tools"].([]any)
	if !ok {
		return out
	}
	names := []string{}
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// OpenAI: {"type":"function","function":{"name":"...","description":"..."}}
		if fn, ok := tm["function"].(map[string]any); ok {
			if n, ok := fn["name"].(string); ok {
				names = append(names, n)
				continue
			}
		}
		// Anthropic-style: {"name":"..."}
		if n, ok := tm["name"].(string); ok {
			names = append(names, n)
		}
	}
	// Sort for stability (Go map iteration is randomized).
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, n := range names {
		out += n + ","
	}
	return out
}