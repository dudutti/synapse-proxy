package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"synapse-proxy/internal/db"
	"synapse-proxy/optiagent"
)

func TestMCPInitialize(t *testing.T) {
	s := NewServer(TierFree, "sk-opti-testkey", "")
	req := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("initialize failed with error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{} result, got %T", resp.Result)
	}

	if resMap["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion '2024-11-05', got %v", resMap["protocolVersion"])
	}

	serverInfo, ok := resMap["serverInfo"].(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string serverInfo, got %T", resMap["serverInfo"])
	}

	if serverInfo["name"] != "synapse-proxy" {
		t.Errorf("expected server name 'synapse-proxy', got %v", serverInfo["name"])
	}
}

func TestMCPToolsList_Free(t *testing.T) {
	s := NewServerWithDefaults(TierFree, "sk-opti-testkey", "")
	req := Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %s", resp.Error.Message)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	toolsSlice, ok := resMap["tools"].([]Tool)
	if !ok {
		// If unmarshalling didn't produce a []Tool yet (since handle returns Go structures),
		// it might be a slice of interface or Tool depending on registration.
		// Let's marshal/unmarshal it into a []Tool so we check type safety correctly.
		toolData, _ := json.Marshal(resMap["tools"])
		if err := json.Unmarshal(toolData, &toolsSlice); err != nil {
			t.Fatalf("expected []Tool tools, got error: %v", err)
		}
	}

	// Should expose exactly 7 free tools
	if len(toolsSlice) != 7 {
		t.Errorf("expected 7 free tools, got %d", len(toolsSlice))
	}

	// Verify all tools are free tools (e.g. synapse_chat_completions)
	names := make(map[string]bool)
	for _, tool := range toolsSlice {
		names[tool.Name] = true
	}

	expectedTools := []string{
		"synapse_chat_completions",
		"synapse_list_models",
		"synapse_cache_stats",
		"synapse_savings_summary",
		"synapse_inspect_ccr_store",
		"synapse_get_ccr_value",
		"synapse_optimize_prompt",
	}
	for _, expected := range expectedTools {
		if !names[expected] {
			t.Errorf("expected tool %s not found in list", expected)
		}
	}
}

func TestMCPToolsList_Full(t *testing.T) {
	s := NewServerWithDefaults(TierFull, "sk-opti-testkey", "https://synapse-proxy.com")
	req := Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/list",
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %s", resp.Error.Message)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	var toolsSlice []Tool
	toolData, _ := json.Marshal(resMap["tools"])
	if err := json.Unmarshal(toolData, &toolsSlice); err != nil {
		t.Fatalf("expected []Tool tools, got error: %v", err)
	}

	// Should expose 17 tools (7 free + 10 paid)
	if len(toolsSlice) != 17 {
		t.Errorf("expected 17 tools in full tier, got %d", len(toolsSlice))
	}
}

func TestMCPToolsCall_RequiresPaidPlanGating(t *testing.T) {
	// Under free tier, a call to a paid tool (even if configured) should fail with CodeRequiresPaidPlan
	s := NewServer(TierFree, "sk-opti-testkey", "")
	// Explicitly register a dummy tool marked as paid = true
	s.Register(Tool{
		Name:        "synapse_run_benchmark",
		Description: "Dummy paid tool",
	}, func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "success", nil
	}, true) // paid = true
	
	params := map[string]interface{}{
		"name": "synapse_run_benchmark",
		"arguments": map[string]interface{}{
			"models": []string{"gpt-4o", "claude-3-5-sonnet"},
			"prompt": "Test benchmark prompt",
		},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error == nil {
		t.Fatalf("expected error for paid tool under free tier, got success")
	}

	if resp.Error.Code != CodeRequiresPaidPlan {
		t.Errorf("expected error code %d (RequiresPaidPlan), got %d (%s)", 
			CodeRequiresPaidPlan, resp.Error.Code, resp.Error.Message)
	}
}

func TestMCPToolsCall_PaidForwarding(t *testing.T) {
	// Mock dashboard HTTP server
	mockResp := map[string]interface{}{
		"winner": "gpt-4o",
		"scores": map[string]float64{
			"gpt-4o":            8.5,
			"claude-3-5-sonnet": 8.2,
		},
	}

	serverCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/api/keys/session-benchmark" {
			t.Errorf("expected path /api/keys/session-benchmark, got %s", r.URL.Path)
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer sk-opti-testkey" {
			t.Errorf("expected Authorization header 'Bearer sk-opti-testkey', got %s", authHeader)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer ts.Close()

	s := NewServerWithDefaults(TierFull, "sk-opti-testkey", ts.URL)

	params := map[string]interface{}{
		"name": "synapse_run_benchmark",
		"arguments": map[string]interface{}{
			"models": []string{"gpt-4o", "claude-3-5-sonnet"},
			"prompt": "Test benchmark prompt",
		},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("benchmark tool call failed: %v", resp.Error)
	}

	if !serverCalled {
		t.Fatalf("expected mock dashboard server to be called, but it was not")
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	content, ok := resMap["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map content in result, got %T", resMap["content"])
	}

	if content["winner"] != "gpt-4o" {
		t.Errorf("expected winner to be 'gpt-4o', got %v", content["winner"])
	}
}

func TestMCPToolsCall_ChatCompletionsLocalMock(t *testing.T) {
	mockProxyResp := map[string]interface{}{
		"id":      "chatcmpl-mock",
		"object":  "chat.completion",
		"created": 1677858253,
		"model":   "gpt-4o-mini",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": "Hello, I am a mock assistant!",
				},
				"finish_reason": "stop",
			},
		},
	}

	// Override the proxyHandlerFunc with a mock that returns the pre-canned response
	oldHandler := proxyHandlerFunc
	proxyHandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer sk-opti-testkey" {
			t.Errorf("expected Authorization header 'Bearer sk-opti-testkey', got %s", authHeader)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-SynapseProxy-Cache", "L1")
		w.Header().Set("X-SynapseProxy-Tokens-Saved", "250")
		w.Header().Set("X-SynapseProxy-Cost-Saved", "0.000375")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockProxyResp)
	}
	defer func() { proxyHandlerFunc = oldHandler }()

	s := NewServerWithDefaults(TierFree, "sk-opti-testkey", "")
	
	params := map[string]interface{}{
		"name": "synapse_chat_completions",
		"arguments": map[string]interface{}{
			"model": "gpt-4o-mini",
			"messages": []map[string]string{
				{"role": "user", "content": "Hello!"},
			},
		},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("chat completions tool call failed: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	content, ok := resMap["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map content in result, got %T", resMap["content"])
	}

	if content["id"] != "chatcmpl-mock" {
		t.Errorf("expected id 'chatcmpl-mock', got %v", content["id"])
	}

	enrichment, ok := content["synapse_enrichment"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected synapse_enrichment in response content, got %T", content["synapse_enrichment"])
	}

	if enrichment["cache_level"] != "L1" {
		t.Errorf("expected cache_level 'L1', got %v", enrichment["cache_level"])
	}
	if enrichment["tokens_saved"] != "250" {
		t.Errorf("expected tokens_saved '250', got %v", enrichment["tokens_saved"])
	}
}

func TestMCPToolsCall_InspectCCRStore(t *testing.T) {
	// Setup in-memory store for test
	store := optiagent.NewInMemoryCompressionStore()
	store.Save("hash123", []byte("original payload"))
	
	oldStore := optiagent.GetGlobalCompressionStore()
	optiagent.SetGlobalCompressionStore(store)
	defer optiagent.SetGlobalCompressionStore(oldStore)

	s := NewServerWithDefaults(TierFree, "sk-opti-testkey", "")
	params := map[string]interface{}{
		"name":      "synapse_inspect_ccr_store",
		"arguments": map[string]interface{}{},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      101,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("inspect ccr store failed: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	content, ok := resMap["content"].(map[string]any)
	if !ok {
		t.Fatalf("expected map content, got %T: %+v", resMap["content"], resMap)
	}
	count, _ := content["count"].(int)
	if count != 1 {
		t.Errorf("expected 1 entry, got %v (raw content: %+v)", count, content)
	}
}

func TestMCPToolsCall_GetCCRValue(t *testing.T) {
	store := optiagent.NewInMemoryCompressionStore()
	store.Save("hash456", []byte("original content secret"))
	
	oldStore := optiagent.GetGlobalCompressionStore()
	optiagent.SetGlobalCompressionStore(store)
	defer optiagent.SetGlobalCompressionStore(oldStore)

	s := NewServerWithDefaults(TierFree, "sk-opti-testkey", "")
	params := map[string]interface{}{
		"name": "synapse_get_ccr_value",
		"arguments": map[string]interface{}{
			"key": "hash456",
		},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      102,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("get ccr value failed: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	content, _ := resMap["content"].(map[string]interface{})
	val, _ := content["value"].(string)
	if val != "original content secret" {
		t.Errorf("expected original content secret, got %v", val)
	}
}

func TestMCPToolsCall_OptimizePrompt(t *testing.T) {
	s := NewServerWithDefaults(TierFree, "sk-opti-testkey", "")
	params := map[string]interface{}{
		"name": "synapse_optimize_prompt",
		"arguments": map[string]interface{}{
			"model": "gpt-4o-mini",
			"messages": []map[string]string{
				{"role": "system", "content": "You are a helpful assistant."},
			},
		},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      103,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("optimize prompt failed: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	content, ok := resMap["content"].(map[string]any)
	if !ok {
		t.Fatalf("expected map content, got %T: %+v", resMap["content"], resMap)
	}
	optMsgsVal, exists := content["optimized_messages"]
	if !exists {
		t.Fatalf("missing optimized_messages key in: %+v", content)
	}
	// Note: in-process return has concrete type []optiagent.Message, NOT []interface{}!
	optMsgs, ok := optMsgsVal.([]optiagent.Message)
	if !ok {
		// Try generic interface slice in case of serialization
		var interfaceSlice []interface{}
		interfaceSlice, ok = optMsgsVal.([]interface{})
		if ok {
			if len(interfaceSlice) != 1 {
				t.Errorf("expected 1 optimized message, got %d (interface slice: %+v)", len(interfaceSlice), interfaceSlice)
			}
		} else {
			t.Fatalf("expected []optiagent.Message or []interface{}, got %T: %+v", optMsgsVal, optMsgsVal)
		}
	} else {
		if len(optMsgs) != 1 {
			t.Errorf("expected 1 optimized message, got %d (msgs: %+v)", len(optMsgs), optMsgs)
		}
	}
}

// Optional Integration tests that run if DB is connected
func TestMCPToolsCall_DatabaseIntegration(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping database integration tests")
	}

	db.InitPostgres()
	
	// Test synapse_list_models
	s := NewServerWithDefaults(TierFree, "sk-opti-testkey", "")
	
	params := map[string]interface{}{
		"name":      "synapse_list_models",
		"arguments": map[string]interface{}{},
	}
	req := Request{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "tools/call",
		Params:  params,
	}
	raw, _ := json.Marshal(req)

	resp := s.handle(context.Background(), raw)
	if resp.Error != nil {
		t.Fatalf("list models failed: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	content, ok := resMap["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected content map, got %T", resMap["content"])
	}

	if _, exists := content["models"]; !exists {
		t.Errorf("expected 'models' key in response content")
	}
}
