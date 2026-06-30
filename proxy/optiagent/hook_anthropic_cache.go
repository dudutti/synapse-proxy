// Package optiagent — Anthropic Prompt Caching Hook.
//
// Implements Phase 3: automatically injecting Anthropic-specific
// cache_control: {"type": "ephemeral"} tags in OpenAI-compatible payloads
// when targeting Claude models (claude-3-*) to maximize KV cache hits.
package optiagent

import (
	"context"
	"encoding/json"
	"log"
	"strings"
)

// AnthropicCacheHook detects Claude target models and automatically inserts
// cache_control checkpoints at the system prompt, tool definitions, and history turns.
type AnthropicCacheHook struct{}

// Name returns the hook name.
func (h *AnthropicCacheHook) Name() string { return "anthropic_cache" }

// Priority is 800: runs after other compression hooks (720-760) and before request forwarding.
func (h *AnthropicCacheHook) Priority() int { return 800 }

// IsEnabled always returns true.
func (h *AnthropicCacheHook) IsEnabled(vk string) bool { return true }

// BeforeRequest parses the request payload and injects cache_control block structures.
func (h *AnthropicCacheHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	payload := hctx.OptimizedPayload
	if payload == nil {
		payload = hctx.RawPayload
	}
	if len(payload) == 0 {
		return nil, nil
	}

	// 1. Decide if this request is going to an Anthropic-compatible
	// upstream. We prefer hctx.Provider (set by the proxy from the
	// virtual key's config), which is authoritative even when the
	// client sends a generic model name like "gpt-4o-mini" that
	// the proxy then re-stamps upstream. We fall back to sniffing
	// the payload's `model` field for cases where the hook runs
	// outside the proxy pipeline (e.g. a direct SDK user).
	isAnthropic := strings.EqualFold(hctx.Provider, "anthropic") ||
		strings.EqualFold(hctx.Provider, "claude")
	if !isAnthropic {
		var modelCheck struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(payload, &modelCheck); err == nil &&
			strings.Contains(strings.ToLower(modelCheck.Model), "claude") {
			isAnthropic = true
		}
	}
	if !isAnthropic {
		return payload, nil
	}

	// 2. Unmarshal full raw payload map to manipulate nested fields directly
	var requestMap map[string]interface{}
	if err := json.Unmarshal(payload, &requestMap); err != nil {
		return payload, nil
	}

	modified := false

	// A. Inject cache_control on system prompt message (typically the first message with role "system")
	messagesVal, ok := requestMap["messages"]
	if ok {
		if messages, ok := messagesVal.([]interface{}); ok && len(messages) > 0 {
			// Find system prompt (usually the first message)
			for idx, msgVal := range messages {
				if msg, ok := msgVal.(map[string]interface{}); ok {
					role, _ := msg["role"].(string)
					if role == "system" {
						contentVal := msg["content"]
						if contentStr, ok := contentVal.(string); ok && len(contentStr) > 0 {
							// Convert string content to block array with cache_control
							msg["content"] = []interface{}{
								map[string]interface{}{
									"type":          "text",
									"text":          contentStr,
									"cache_control": map[string]interface{}{"type": "ephemeral"},
								},
							}
							messages[idx] = msg
							modified = true
						}
						break // Only cache the first system prompt
					}
				}
			}

			// B. Inject cache_control on the last assistant message before the final user message
			// (Conversation history checkpoint)
			if len(messages) >= 2 {
				// The last message is usually the user's new prompt (role: "user").
				// The message just before it is usually the assistant's previous reply (role: "assistant").
				assistantIdx := len(messages) - 2
				if msg, ok := messages[assistantIdx].(map[string]interface{}); ok {
					role, _ := msg["role"].(string)
					if role == "assistant" {
						contentVal := msg["content"]
						if contentStr, ok := contentVal.(string); ok && len(contentStr) > 0 {
							msg["content"] = []interface{}{
								map[string]interface{}{
									"type":          "text",
									"text":          contentStr,
									"cache_control": map[string]interface{}{"type": "ephemeral"},
								},
							}
							messages[assistantIdx] = msg
							modified = true
						}
					}
				}
			}
		}
	}

	// C. Inject cache_control on the last tool definition (to cache the entire tool schema list)
	toolsVal, ok := requestMap["tools"]
	if ok {
		if tools, ok := toolsVal.([]interface{}); ok && len(tools) > 0 {
			lastToolIdx := len(tools) - 1
			if tool, ok := tools[lastToolIdx].(map[string]interface{}); ok {
				// Tool object structure: {"type": "function", "function": {"name": ..., "description": ...}}
				if functionVal, ok := tool["function"].(map[string]interface{}); ok {
					functionVal["cache_control"] = map[string]interface{}{"type": "ephemeral"}
					tool["function"] = functionVal
					tools[lastToolIdx] = tool
					modified = true
				}
			}
		}
	}

	if modified {
		newPayload, err := json.Marshal(requestMap)
		if err == nil {
			log.Printf("[AnthropicCache] successfully injected ephemeral cache_control tags for provider=%s model=%s",
				hctx.Provider, hctx.Model)
			return newPayload, nil
		}
	}

	return payload, nil
}

// AfterResponse is a no-op.
func (h *AnthropicCacheHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	return nil, nil
}

func init() {
	RegisterHook(&AnthropicCacheHook{})
	log.Printf("[hooks] registered AnthropicCacheHook at priority 800")
}
