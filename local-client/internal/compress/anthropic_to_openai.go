// Package compress — Anthropic → OpenAI response translator
// (local copy of proxy/optiagent/anthropic_to_openai.go, kept
// in sync).
//
// When the local-client forwards to /v1/messages (Anthropic
// shape) the upstream replies in Anthropic message format.
// We translate the response back into the OpenAI
// chat-completion shape so the caller (Hermes, LM Studio
// client, etc.) doesn't have to know.
//
// Also exposes the Anthropic-specific counters (cache_read,
// cache_creation) as OpenAI prompt_tokens_details.cached_tokens
// so the dashboard can attribute the savings.
package compress

import (
	"encoding/json"
	"fmt"
	"strings"
)

type anthropicResp struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Role       string                   `json:"role"`
	Model      string                   `json:"model"`
	Content    []map[string]interface{} `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationTokens      int `json:"cache_creation_input_tokens"`
		CacheHitTokens           int `json:"cache_hit_tokens"`
		CacheMissTokens          int `json:"cache_miss_tokens"`
	} `json:"usage"`
}

type openAIResp struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []map[string]interface{} `json:"choices"`
	Usage   map[string]interface{}   `json:"usage"`
}

// AnthropicToOpenAI converts a raw Anthropic /v1/messages
// response body into the OpenAI chat-completion shape. now is
// the unix timestamp for the `created` field, clientModel is
// the model name the client asked for (so we re-stamp the
// model field in the response).
func AnthropicToOpenAI(body []byte, now int64, clientModel string) ([]byte, error) {
	var src anthropicResp
	if err := json.Unmarshal(body, &src); err != nil {
		return nil, fmt.Errorf("AnthropicToOpenAI: not valid JSON: %w", err)
	}
	out := openAIResp{
		ID:      src.ID,
		Object:  "chat.completion",
		Created: now,
		Model:   clientModel,
	}
	contentStr, toolCalls, hasToolUse := anthropicContentToOpenAI(src.Content)
	choice := map[string]interface{}{
		"index": 0,
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": contentStr,
		},
		"finish_reason": anthropicStopReasonToOpenAI(src.StopReason, hasToolUse),
	}
	if hasToolUse {
		choice["message"].(map[string]interface{})["tool_calls"] = toolCalls
	}
	out.Choices = []map[string]interface{}{choice}
	billed := src.Usage.InputTokens
	cachedRead := src.Usage.CacheReadInputTokens
	cacheCreated := src.Usage.CacheCreationTokens
	usage := map[string]interface{}{
		"prompt_tokens":     billed + cachedRead + cacheCreated,
		"completion_tokens": src.Usage.OutputTokens,
		"prompt_tokens_details": map[string]interface{}{
			"cached_tokens":          cachedRead,
			"cache_creation_tokens": cacheCreated,
		},
		"completion_tokens_details": map[string]interface{}{
			"reasoning_tokens": 0,
		},
		"total_tokens": billed + cachedRead + cacheCreated + src.Usage.OutputTokens,
	}
	out.Usage = usage
	return json.Marshal(out)
}

func anthropicStopReasonToOpenAI(reason string, hasToolUse bool) string {
	switch reason {
	case "end_turn":
		if hasToolUse {
			return "tool_calls"
		}
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		if hasToolUse {
			return "tool_calls"
		}
		if reason == "" {
			return "stop"
		}
		return reason
	}
}

func anthropicContentToOpenAI(blocks []map[string]interface{}) (content string, toolCalls []map[string]interface{}, hasToolUse bool) {
	var textParts []string
	for _, block := range blocks {
		t, _ := block["type"].(string)
		switch t {
		case "text":
			if s, ok := block["text"].(string); ok {
				textParts = append(textParts, s)
			}
		case "thinking":
			if s, ok := block["thinking"].(string); ok {
				textParts = append(textParts, "[thinking]\n"+s+"\n[/thinking]")
			}
		case "tool_use":
			hasToolUse = true
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			input, _ := block["input"]
			argsJSON, _ := json.Marshal(input)
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":       id,
				"type":     "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": json.RawMessage(argsJSON),
				},
			})
		}
	}
	content = strings.Join(textParts, "\n\n")
	return
}