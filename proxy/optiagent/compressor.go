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
		// assistant turn as input context, which is pure waste — observed
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
		if role == "tool" || role == "function" {
			if !isRecentMessage && hasContent && len(contentStr) > 200 {
				msg["content"] = `{"status": "success", "_opti_pruned": true, "note": "Huge older tool output pruned to save context"}`
			}
			
			if name == lastToolName && name != "" {
				consecutiveToolCount++
				if consecutiveToolCount > 2 && !isRecentMessage {
					msg["content"] = `{"status": "compacted_repeated_tool"}`
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

	// Re-encode preserving all original fields
	return json.Marshal(genericPayload)
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
