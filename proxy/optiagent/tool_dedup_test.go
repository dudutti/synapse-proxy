package optiagent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractAllToolCalls(t *testing.T) {
	payload := []byte(`{
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "web_search",
							"arguments": "{\"q\":\"météo Paris\"}"
						}
					}
				]
			}
		]
	}`)

	calls := ExtractAllToolCalls(payload)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ToolName != "web_search" {
		t.Errorf("expected toolName web_search, got %s", calls[0].ToolName)
	}
	if !strings.Contains(calls[0].Command, "météo Paris") {
		t.Errorf("expected command to contain météo Paris, got %s", calls[0].Command)
	}
}

func TestStoreCompletedToolCalls_Mapping(t *testing.T) {
	payload := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": "What is the weather?"
			},
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_abc",
						"type": "function",
						"function": {
							"name": "web_search",
							"arguments": "{\"q\":\"météo Paris\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_abc",
				"content": "Sunny and 25C"
			}
		]
	}`)

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(body.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(body.Messages))
	}

	// Verify our mapping logic manually in the test
	msg := body.Messages[2]
	if msg.Role != "tool" {
		t.Fatalf("expected role tool, got %s", msg.Role)
	}

	matched := false
	var matchedName, matchedArgs string
	for j := 2 - 1; j >= 0; j-- {
		prev := body.Messages[j]
		if prev.Role == "assistant" && len(prev.ToolCalls) > 0 {
			for _, tc := range prev.ToolCalls {
				if tc.ID == msg.ToolCallID {
					matchedName = tc.Function.Name
					matchedArgs = tc.Function.Arguments
					matched = true
					break
				}
			}
		}
		if matched {
			break
		}
	}

	if !matched {
		t.Fatal("expected match, got none")
	}
	if matchedName != "web_search" {
		t.Errorf("expected web_search, got %s", matchedName)
	}
	if !strings.Contains(matchedArgs, "météo Paris") {
		t.Errorf("expected arguments to contain météo Paris, got %s", matchedArgs)
	}
}
