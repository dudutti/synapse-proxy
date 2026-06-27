// Package utils — ExtractUsage: real implementation.
//
// Parses a model response body and returns the token
// usage. Recognizes the OpenAI / Anthropic / Gemini
// shapes (any of the fields is optional).
//
// OpenAI:    {"usage":{"prompt_tokens":N,"completion_tokens":N,"prompt_tokens_details":{"cached_tokens":N}}}
// Anthropic: {"usage":{"input_tokens":N,"output_tokens":N,"cache_read_input_tokens":N,"cache_creation_input_tokens":N}}}
// Gemini:    {"usageMetadata":{"promptTokenCount":N,"candidatesTokenCount":N,"cachedContentTokenCount":N,"thoughtsTokenCount":N}}
//
// We map all of these to a single Usage struct so the
// rest of the proxy only needs to look at one place.
package utils

import (
	"encoding/json"
	"strings"
)

// Usage holds token counts extracted from a model
// response. Fields map to the canonical OpenAI shape
// (prompt_tokens, completion_tokens, reasoning_tokens,
// cache_read_tokens, cache_creation_tokens) regardless
// of which provider emitted them.
type Usage struct {
	PromptTokens        int    `json:"prompt_tokens"`
	CompletionTokens    int    `json:"completion_tokens"`
	ReasoningTokens     int    `json:"reasoning_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
	CacheHitTokens      int    `json:"cache_hit_tokens"`
	CacheMissTokens     int    `json:"cache_miss_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	Source              string `json:"source,omitempty"`
}

// ExtractUsage parses a model response body and returns
// the token usage. If the body has no recognized usage
// block, returns Usage{} (caller falls back to tiktoken
// counts).
func ExtractUsage(body []byte) Usage {
	if len(body) == 0 {
		return Usage{}
	}

	// Try OpenAI / Anthropic shape first.
	var openAIShape struct {
		Usage *struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			ReasoningTokens     int `json:"reasoning_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &openAIShape); err == nil && openAIShape.Usage != nil {
			u := openAIShape.Usage
			cached := 0
			if u.PromptTokensDetails != nil {
				cached = u.PromptTokensDetails.CachedTokens
			}
			return Usage{
				PromptTokens:        pickNonZero(u.PromptTokens, u.InputTokens),
				CompletionTokens:    pickNonZero(u.CompletionTokens, u.OutputTokens),
				ReasoningTokens:     u.ReasoningTokens,
				CacheReadTokens:     pickNonZero(cached, u.CacheReadInputTokens),
				CacheCreationTokens: u.CacheCreationInputTokens,
				Source:              "openai_or_anthropic",
			}
		}

	// Try Gemini shape.
	var geminiShape struct {
		UsageMetadata *struct {
			PromptTokenCount        int `json:"promptTokenCount"`
			CandidatesTokenCount    int `json:"candidatesTokenCount"`
			CachedContentTokenCount int `json:"cachedContentTokenCount"`
			ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &geminiShape); err == nil && geminiShape.UsageMetadata != nil {
		u := geminiShape.UsageMetadata
		return Usage{
			PromptTokens:        u.PromptTokenCount,
			CompletionTokens:    u.CandidatesTokenCount,
			ReasoningTokens:     u.ThoughtsTokenCount,
			CacheReadTokens:     u.CachedContentTokenCount,
			CacheCreationTokens: 0,
			Source:              "gemini",
		}
	}

	// SSE stream: extract usage from the last "data: ..." line
	// that contains "usage". Streaming OpenAI / Claude responses
	// look like: data: {...}\n\n repeated, the last one (or one
	// near the end) containing the usage block.
	if idx := strings.LastIndex(string(body), `"usage"`); idx > 0 {
		start := strings.LastIndex(string(body[:idx]), "data: ")
		if start >= 0 && start < idx {
			end := idx + 200
			if end > len(body) {
				end = len(body)
			}
			return ExtractUsage(body[start+6 : end])
		}
	}

	return Usage{}
}

func pickNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}