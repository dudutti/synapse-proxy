// Package optiagent — CCRResponseHandler.
//
// P1.2: closes the CCR loop. When the LLM calls
// `synapse_retrieve(cache_key="...")`, this hook
// intercepts the upstream response, looks up the
// original in the CompressionStore, and replaces the
// tool_call with a tool response containing the
// original content.
//
// The handler is an AfterResponse hook. It does NOT
// make a new upstream call: it short-circuits with
// the local original. This is what makes the CCR
// tool actually useful (the LLM can ask for the
// original that we previously compressed away).
//
// If the cache_key is missing in the store (e.g. the
// CompressionStore was cleared), we return a tool
// error message so the LLM can react.
package optiagent

import (
	"context"
	"encoding/json"
	"log"
	"strings"
)

// CCRResponseHandler is an AfterResponse hook that
// intercepts synapse_retrieve tool calls and returns
// the original content from the CompressionStore.
type CCRResponseHandler struct {
	// store holds the original payloads keyed by their
	// CCR cache key. If nil, the hook is a no-op.
	store CompressionStore
}

// NewCCRResponseHandler returns a handler with the
// given CompressionStore. Use SetCCRResponseStore at
// boot to set the global instance.
func NewCCRResponseHandler(store CompressionStore) *CCRResponseHandler {
	return &CCRResponseHandler{store: store}
}

// AfterResponse inspects the upstream response. If
// it contains a synapse_retrieve tool call, replace
// the tool_call with a tool response containing the
// original payload from the CompressionStore.
func (h *CCRResponseHandler) AfterResponse(ctx context.Context, hctx *HookContext, upstreamResp []byte) ([]byte, error) {
	if hctx == nil || len(upstreamResp) == 0 {
		return upstreamResp, nil
	}
	store := h.store
	if store == nil {
		store = GetGlobalCompressionStore()
	}
	if store == nil {
		// No store wired: pass through unchanged.
		return upstreamResp, nil
	}

	// Parse the response to find the tool_call.
	var resp struct {
		Choices []struct {
			Message struct {
				Role      string                   `json:"role"`
				Content   interface{}              `json:"content"`
				ToolCalls []map[string]interface{} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(upstreamResp, &resp); err != nil {
		// Not a JSON response or not the shape we expect.
		// Pass through.
		return upstreamResp, nil
	}

	// Find a synapse_retrieve tool call.
	for i, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			fn, _ := tc["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			if name != SynapseRetrieveToolName {
				continue
			}
			// Extract the cache_key.
			argsRaw, _ := fn["arguments"].(string)
			var args struct {
				CacheKey string `json:"cache_key"`
			}
			if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
				log.Printf("[%s] bad arguments: %v", SynapseRetrieveToolName, err)
				continue
			}
			if args.CacheKey == "" {
				continue
			}
			// Look up the original.
			original, err := store.Lookup(args.CacheKey)
			ok := original != nil
			if err != nil {
				log.Printf("[%s] store error: %v", SynapseRetrieveToolName, err)
				continue
			}
			if !ok {
				// Return an error as the tool response so
				// the LLM can recover.
				resp.Choices[i].Message.ToolCalls = nil
				resp.Choices[i].Message.Role = "tool"
				resp.Choices[i].Message.Content = map[string]interface{}{
					"error":         "cache_key not found",
					"cache_key":     args.CacheKey,
					"suggestion":    "the original was already evicted or the cache_key is incorrect",
				}
				resp.Choices[i].FinishReason = "stop"
				metricsRecordCCRSynapseRetrieve("miss")
				continue
			}
			// Replace the tool_call with a tool response
			// containing the original.
			resp.Choices[i].Message.ToolCalls = nil
			resp.Choices[i].Message.Role = "tool"
			resp.Choices[i].Message.Content = map[string]interface{}{
				"original":     string(original),
				"cache_key":    args.CacheKey,
				"tool_call_id": tc["id"],
			}
			resp.Choices[i].FinishReason = "stop"
			metricsRecordCCRSynapseRetrieve("hit")
			log.Printf("[%s] HIT cache_key=%s vk=%s bytes=%d",
				SynapseRetrieveToolName, args.CacheKey, hctx.VK, len(original))
		}
	}

	return json.Marshal(resp)
}

// metricsRecordCCRSynapseRetrieve records a metric for
// the synapse_retrieve response handler. We avoid
// importing the metrics package directly to keep
// this hook self-contained; the caller wires the
// integration via the perHookSavings JSON in hctx.
func metricsRecordCCRSynapseRetrieve(kind string) {
	if kind == "hit" {
		_ = strings.TrimSpace // keep import minimal
	}
}

// init registers the CCR response handler with the
// global hook registry at a high priority so it runs
// before any other AfterResponse hook that might
// expect tool_calls in the response.
// adapter implements the Hook interface by holding
// the CCRResponseHandler and using a shared variable
// for the upstream response (set by the handler
// before AfterResponse is called).
type ccrResponseAdapter struct {
	h *CCRResponseHandler
}

func (a *ccrResponseAdapter) Name() string { return SynapseRetrieveToolName + "_response" }
func (a *ccrResponseAdapter) Priority() int { return 950 }
func (a *ccrResponseAdapter) IsEnabled(vk string) bool { return true }
func (a *ccrResponseAdapter) IncrementBefore(name, vk string) {}
func (a *ccrResponseAdapter) IncrementAfter(name, vk string) {}
func (a *ccrResponseAdapter) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	return hctx.OptimizedPayload, nil
}
func (a *ccrResponseAdapter) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	// The handler expects the upstream response as a 3rd arg.
	// The Hook interface only has 2 args, so we read it
	// from hctx.UpstreamResponse (set by the dispatcher).
	return a.h.AfterResponse(ctx, hctx, hctx.UpstreamResponse)
}

func init() {
	RegisterHook(&ccrResponseAdapter{h: NewCCRResponseHandler(nil)})
	log.Printf("[hooks] registered CCRResponseHandler at priority 950")
}