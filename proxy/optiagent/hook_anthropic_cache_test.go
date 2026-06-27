package optiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicCache_Injection(t *testing.T) {
	// Standard request payload with system prompt, messages, and tools
	payloadStr := `{
		"model": "claude-3-5-sonnet",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello!"},
			{"role": "assistant", "content": "Hi there! How can I help you today?"},
			{"role": "user", "content": "I need help with prompt caching."}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get current weather"
				}
			}
		]
	}`

	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(payloadStr),
	}

	hook := &AnthropicCacheHook{}
	resPayload, err := hook.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}

	// Parse result
	var result map[string]interface{}
	if err := json.Unmarshal(resPayload, &result); err != nil {
		t.Fatalf("Failed to unmarshal result payload: %v", err)
	}

	// Verify system message cache injection
	messages, ok := result["messages"].([]interface{})
	if !ok || len(messages) != 4 {
		t.Fatalf("Expected 4 messages, got: %v", messages)
	}

	// Check system message content (should be an array of blocks now)
	sysMsg, _ := messages[0].(map[string]interface{})
	sysContent, ok := sysMsg["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected system message content to be block array, got: %T", sysMsg["content"])
	}
	sysBlock, _ := sysContent[0].(map[string]interface{})
	if sysBlock["type"] != "text" || sysBlock["text"] != "You are a helpful assistant." {
		t.Errorf("Unexpected block content in system: %v", sysBlock)
	}
	sysCache, _ := sysBlock["cache_control"].(map[string]interface{})
	if sysCache["type"] != "ephemeral" {
		t.Errorf("Expected ephemeral cache_control on system, got: %v", sysCache)
	}

	// Check assistant message content (should be block array with cache_control)
	astMsg, _ := messages[2].(map[string]interface{})
	astContent, ok := astMsg["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected assistant message content to be block array, got: %T", astMsg["content"])
	}
	astBlock, _ := astContent[0].(map[string]interface{})
	astCache, _ := astBlock["cache_control"].(map[string]interface{})
	if astCache["type"] != "ephemeral" {
		t.Errorf("Expected ephemeral cache_control on assistant, got: %v", astCache)
	}

	// Verify tools list cache injection
	tools, ok := result["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got: %v", tools)
	}
	tool, _ := tools[0].(map[string]interface{})
	fn, _ := tool["function"].(map[string]interface{})
	toolCache, _ := fn["cache_control"].(map[string]interface{})
	if toolCache["type"] != "ephemeral" {
		t.Errorf("Expected ephemeral cache_control on tool, got: %v", toolCache)
	}
}

func TestAnthropicCache_NonClaudeModel(t *testing.T) {
	// Standard request payload targeting non-Claude model
	payloadStr := `{
		"model": "gpt-4o-mini",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."}
		]
	}`

	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(payloadStr),
	}

	hook := &AnthropicCacheHook{}
	resPayload, err := hook.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}

	// Output should be exactly same (no injection)
	if !strings.Contains(string(resPayload), "You are a helpful assistant.") {
		t.Errorf("Expected unmodified content, got: %s", string(resPayload))
	}
	if strings.Contains(string(resPayload), "cache_control") {
		t.Errorf("Expected no cache_control for non-Claude model")
	}
}
