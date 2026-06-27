// Package optiagent — CCRToolInjectionHook.
//
// P1.1: when the proxy compresses content (L3, CCR
// Compress, LogCompressor, etc.), the original is
// stored in the CompressionStore under a cache_key.
// This hook adds a `synapse_retrieve` tool to the
// payload's tools[] array, so the LLM can call it
// to get the original back.
//
// The tool is a thin wrapper: the LLM calls it with
// the cache_key, the response handler (P1.2) intercepts
// the call, looks up the original, and returns it.
//
// Note on naming: we use `synapse_retrieve` (not
// `headroom_retrieve`) to make it clear the tool is
// Synapse-specific. The proxy is the boundary; the
// tool name should reflect the product.
//
// Reference: headroom/headroom/ccr/tool_injection.py.
// The Headroom Python module is more elaborate (it
// supports two injection modes: tool definition
// injection and system message injection). We start
// with tool definition injection only. System message
// injection is a Phase 2 addition.

package optiagent

import (
	"context"
	"encoding/json"
	"log"

	"synapse-proxy/internal/metrics"
)

// CCRToolInjectionHook is a BeforeRequest hook that
// adds a "synapse_retrieve" tool to the payload's
// tools[] array when the CompressionStore has an
// entry for this payload.
type CCRToolInjectionHook struct{}

// Name returns the hook name.
func (h *CCRToolInjectionHook) Name() string { return "ccr_tool_injection" }

// Priority places this hook LAST in the BeforeRequest
// chain. It must run after all compression hooks
// (LogCompressor, CCR Compress, etc.) so the
// CompressionStore has the latest cache_key. It runs
// after the cache_key has been set in hctx.Features
// by the LogCompressor (or any other compression
// hook).
func (h *CCRToolInjectionHook) Priority() int { return 900 }

// SynapseRetrieveToolName is the name of the tool we
// inject for CCR retrieval. Exported as a constant
// so the response handler (P1.2) and any other
// consumer can reference the same name without
// hardcoding the string.
const SynapseRetrieveToolName = "synapse_retrieve"

// BeforeRequest parses the payload as JSON, looks up
// the ccr_cache_key feature, and appends a
// synapse_retrieve tool to the tools[] array.
//
// Behavior:
//   - If ccr_cache_key is not set: no-op (no tool added).
//   - If the payload is not valid JSON: no-op (the
//     hook must never break a request).
//   - If tools[] already exists: append the CCR tool
//     to the existing list (user tools are preserved).
//   - If tools[] doesn't exist: create it with just
//     the CCR tool.
func (h *CCRToolInjectionHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx == nil || len(hctx.OptimizedPayload) == 0 {
		return hctx.OptimizedPayload, nil
	}
	// Only inject if a compression happened (the
	// compression hook set the ccr_cache_key feature).
	cacheKey, _ := hctx.GetFeature("ccr_cache_key").(string)
	if cacheKey == "" {
		return hctx.OptimizedPayload, nil
	}
	// Parse the payload as JSON. If it fails, return
	// the payload unchanged (the hook is defensive).
	var parsed map[string]interface{}
	if err := json.Unmarshal(hctx.OptimizedPayload, &parsed); err != nil {
		log.Printf("[%s] payload is not JSON, skipping: %v", h.Name(), err)
		return hctx.OptimizedPayload, nil
	}
	// Get the existing tools[] array, or create one.
	tools, _ := parsed["tools"].([]interface{})
	// Build the synapse_retrieve tool definition.
	ccrTool := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": SynapseRetrieveToolName,
			"description": "Retrieve the original (uncompressed) content of a tool output that was compressed by the Synapse proxy. The cache_key parameter identifies which compressed content to retrieve. Use this when the compressed version is missing context you need to answer the user's question.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cache_key": map[string]interface{}{
						"type":        "string",
						"description": "The cache_key of the compressed content, returned in the synapse_enrichment block of the previous turn.",
					},
				},
				"required": []string{"cache_key"},
			},
		},
	}
	// Inject the cache_key into the description so the
	// LLM knows which key to call with.
	fn := ccrTool["function"].(map[string]interface{})
	fn["description"] = fn["description"].(string) + " (current cache_key: " + cacheKey + ")"
	tools = append(tools, ccrTool)
	parsed["tools"] = tools
	// Re-serialize.
	out, err := json.Marshal(parsed)
	if err != nil {
		log.Printf("[%s] re-serialize failed: %v", h.Name(), err)
		return hctx.OptimizedPayload, nil
	}
	log.Printf("[%s] injected %s tool vk=%s key=%s", h.Name(), SynapseRetrieveToolName, hctx.VK, cacheKey)
	// P1.5 DASHBOARD FIRST: bump the per-hook metric so
	// the dashboard shows the number of tool
	// injections per request.
	metrics.RecordSynapseRetrieveInjected()
	return out, nil
}

// AfterResponse is a no-op.
func (h *CCRToolInjectionHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// IsEnabled returns true.
func (h *CCRToolInjectionHook) IsEnabled(vk string) bool { return true }

// init registers the hook.
func init() {
	RegisterHook(&CCRToolInjectionHook{})
	log.Printf("[hooks] registered CCRToolInjectionHook at priority 900")
}
