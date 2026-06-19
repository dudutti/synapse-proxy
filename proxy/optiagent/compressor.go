package optiagent

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

type ChatMessage struct {
	Role    string      `json:"role"`
	Content string      `json:"content"`
	Name    string      `json:"name,omitempty"`
}

type ChatPayload struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

var (
	// Hermes / Claude / General chain-of-thought blocks
	thoughtRegex = regexp.MustCompile(`(?s)<thought>.*?</thought>|<thinking>.*?</thinking>|<scratchpad>.*?</scratchpad>`)
	
	// OpenClaw / Agentic framework massive tool outputs (base64, huge JSONs)
	// We'll use a length threshold for older tool outputs
)

// CompressPayload applies agent-specific compression for frameworks like OpenClaw
func CompressPayload(payload []byte) ([]byte, error) {
	// Unmarshal into generic map to preserve all fields (tools, temperature, etc.)
	var genericPayload map[string]interface{}
	if err := json.Unmarshal(payload, &genericPayload); err != nil {
		return nil, err
	}

	messagesRaw, ok := genericPayload["messages"].([]interface{})
	if !ok {
		return payload, nil
	}

	var lastToolName string
	var consecutiveToolCount int
	msgCount := len(messagesRaw)

	for i, msgIntf := range messagesRaw {
		msg, ok := msgIntf.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		contentStr, hasContent := msg["content"].(string)
		name, _ := msg["name"].(string)
		isRecentMessage := i >= msgCount-2

		// 1. Hermes / Claude Chain-of-Thought Pruning
		if role == "assistant" && !isRecentMessage && hasContent {
			msg["content"] = thoughtRegex.ReplaceAllString(contentStr, "[Pruned Thought Process]")
		}

		// 1b. Strip reasoning_content from previous assistant turns.
		// Thinking models (DeepSeek-R1, MiniMax M3, Qwen QwQ, Gemma) return
		// reasoning as a sibling field of content. Clients re-send the whole
		// assistant turn as input context, which is pure waste â€” observed
		// at 89% of input tokens on agentic workloads. Only prune turns that
		// are not the most recent assistant message.
		if role == "assistant" && !isRecentMessage {
			if _, hasReasoning := msg["reasoning_content"]; hasReasoning {
				delete(msg, "reasoning_content")
			}
			if _, hasReasoning := msg["reasoning"]; hasReasoning {
				delete(msg, "reasoning")
			}
		}

		// 2. OpenClaw / Stale Tool Compaction
		// CRITICAL: do NOT replace tool output with a synthetic stub
		// message. The agent's safety filter reads the tool result and
		// treats anything that looks like a system message ("_opti_pruned",
		// "compacted_repeated_tool", "Respond as helpfully as possible",
		// "JAILBREAK", etc.) as a prompt-injection attempt, then refuses
		// to use the result. We must keep the *shape* of the original
		// content and only shrink it.
		if role == "tool" || role == "function" {
			if !isRecentMessage && hasContent && len(contentStr) > 200 {
				// Keep the first N chars + a trailing ellipsis. The agent
				// can still see the head of the result; the tail is just
				// gone (we already injected the [Synapse Proxy] compaction
				// hint at the very first system turn, so the model knows
				// older tool results may be truncated).
				const keep = 200
				if len(contentStr) > keep+50 {
					msg["content"] = contentStr[:keep] + "\n[â€¦truncated by Synapse Proxy L3â€¦]"
				}
			}

			if name == lastToolName && name != "" {
				consecutiveToolCount++
				// Drop repeated tool results beyond the first 2 â€” just
				// replace with a minimal valid JSON that the agent can
				// parse without flagging as injection.
				if consecutiveToolCount > 2 && !isRecentMessage {
					msg["content"] = ""
				}
			} else {
				lastToolName = name
				consecutiveToolCount = 1
			}
		} else {
			lastToolName = ""
			consecutiveToolCount = 0
		}
	}

	// Re-encode preserving all original fields.
	//
	// We use the deterministic encoder (not stdlib json.Marshal) so
	// that two calls to CompressPayload with the same input produce
	// byte-identical output. This is required for provider prompt
	// caching: Anthropic / OpenAI / MiniMax hash the request
	// bytes for cache lookup, and any whitespace / key-order drift
	// would invalidate the cache silently. See
	// marshal_deterministic.go for the rationale.
	return marshalDeterministic(genericPayload)
}

// ModelRouting rewrites payload to a budget-friendly model based on heuristics
func ModelRouting(req *ChatPayload) {
	// Simple predictive routing heuristic
	// If the task involves "format" or "sort" and is using a heavy model, downgrade it.
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(strings.ToLower(lastMsg), "format this as json") || 
		   strings.Contains(strings.ToLower(lastMsg), "sort this list") {
			if req.Model == "gpt-4o" {
				req.Model = "gpt-4o-mini"
				log.Println("Routed gpt-4o -> gpt-4o-mini to save cost")
			}
		}
	}
}
