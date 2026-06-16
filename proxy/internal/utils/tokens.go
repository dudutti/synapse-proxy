package utils

import (
	"encoding/json"
	"log"

	"github.com/pkoukk/tiktoken-go"
)

var tke *tiktoken.Tiktoken

// InitTiktoken initializes the BPE tokenizer for cl100k_base (OpenAI)
func InitTiktoken() {
	var err error
	tke, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		log.Printf("optiagent: failed to load tiktoken: %v", err)
	}
}

// CountTokens returns the exact number of tokens in a text string using tiktoken
func CountTokens(text string) int {
	if tke != nil {
		return len(tke.Encode(text, nil, nil))
	}
	// Fallback estimation
	return len(text) / 4
}

// UsageResult is the multi-format output of ExtractUsage.
// Source reports which strategy produced the numbers so we can audit
// unusual telemetry rows.
type UsageResult struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	Source           string // "openai", "anthropic", "google", "deepseek", "discovered", "estimated"

	// Cache prompt tokens — provider-specific (see plan)
	// CacheCreationTokens: Anthropic cache_creation_input_tokens
	// CacheReadTokens: Anthropic cache_read_input_tokens + OpenAI cached_tokens
	// CacheHitTokens: DeepSeek prompt_cache_hit_tokens + Google cachedContentTokenCount
	// CacheMissTokens: DeepSeek prompt_cache_miss_tokens
	CacheCreationTokens int
	CacheReadTokens     int
	CacheHitTokens      int
	CacheMissTokens     int
}

// ExtractUsage parses an LLM JSON response (or a reconstructed SSE payload)
// and extracts prompt/completion/reasoning token usage.
//
// Detection order is shape-based to avoid misattribution (e.g. labeling
// an Anthropic response as "openai" because it has a partial usage block):
//  1. usageMetadata.* (Google native — never present in OpenAI/Anthropic)
//  2. usage with prompt_cache_hit_tokens / prompt_cache_miss_tokens (DeepSeek)
//  3. usage with input_tokens + output_tokens and no prompt_tokens (Anthropic native + proxies)
//  4. usage with prompt_tokens + completion_tokens (OpenAI / DeepInfra / Together / etc.)
//  5. Recursive discovery via field_discoverer (proprietary providers)
//  6. Fallback: CountTokens over the first choice content.
func ExtractUsage(respBytes []byte) UsageResult {
	var generic map[string]interface{}
	if err := json.Unmarshal(respBytes, &generic); err != nil {
		return UsageResult{Source: "estimated"}
	}

	// 1. Google native usageMetadata
	if meta, ok := generic["usageMetadata"].(map[string]interface{}); ok {
		if p, c, r, cached := readGoogleUsage(meta); p > 0 || c > 0 {
			return UsageResult{
				PromptTokens:     p,
				CompletionTokens: c,
				ReasoningTokens:  r,
				CacheHitTokens:   cached,
				Source:           "google",
			}
		}
	}

	if usage, ok := generic["usage"].(map[string]interface{}); ok {
		// 2. DeepSeek shape: prompt_cache_hit_tokens + prompt_cache_miss_tokens
		//    Must be checked BEFORE OpenAI shape because DeepSeek also returns prompt_tokens (= hit + miss).
		if _, hasHit := usage["prompt_cache_hit_tokens"]; hasHit {
			if p, c, r, hit, miss := readDeepSeekUsage(usage); p > 0 || c > 0 {
				return UsageResult{
					PromptTokens:     p,
					CompletionTokens: c,
					ReasoningTokens:  r,
					CacheHitTokens:   hit,
					CacheMissTokens:  miss,
					Source:           "deepseek",
				}
			}
		}

		// 3. Anthropic shape: input_tokens/output_tokens present, no prompt_tokens
		if _, hasInput := usage["input_tokens"]; hasInput {
			if _, hasOpenAIPrompt := usage["prompt_tokens"]; !hasOpenAIPrompt {
				p, c, r := readAnthropicUsage(usage)
				if p > 0 || c > 0 {
					return UsageResult{
						PromptTokens:        p,
						CompletionTokens:    c,
						ReasoningTokens:     r,
						CacheCreationTokens: asInt(usage["cache_creation_input_tokens"]),
						CacheReadTokens:     asInt(usage["cache_read_input_tokens"]),
						Source:              "anthropic",
					}
				}
			}
		}
		// 4. OpenAI / OpenAI-compatible shape
		if _, hasOpenAIPrompt := usage["prompt_tokens"]; hasOpenAIPrompt {
			p, c, r, cached := readOpenAIUsage(usage)
			return UsageResult{
				PromptTokens:     p,
				CompletionTokens: c,
				ReasoningTokens:  r,
				CacheReadTokens:  cached,
				Source:           "openai",
			}
		}
		// Fallback: usage block with neither shape — try OpenAI fields and
		// let estimateCompletion handle the rest.
		p, c, r, cached := readOpenAIUsage(usage)
		if p > 0 || c > 0 {
			return UsageResult{
				PromptTokens:     p,
				CompletionTokens: c,
				ReasoningTokens:  r,
				CacheReadTokens:  cached,
				Source:           "openai",
			}
		}
	}

	// 5. Recursive discovery (custom fields in proprietary providers)
	if mapping, ok := discoverUsage(respBytes); ok {
		if p, c, r := applyMapping(generic, mapping); p > 0 || c > 0 {
			return UsageResult{PromptTokens: p, CompletionTokens: c, ReasoningTokens: r, Source: "discovered"}
		}
	}

	// 6. Fallback: tiktoken on the first choice content
	return UsageResult{
		CompletionTokens: estimateCompletion(generic),
		Source:           "estimated",
	}
}

func readOpenAIUsage(usage map[string]interface{}) (int, int, int, int) {
	p := asInt(usage["prompt_tokens"])
	c := asInt(usage["completion_tokens"])
	r := 0
	if details, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
		r = asInt(details["reasoning_tokens"])
	}
	if r == 0 {
		// Some providers put it at the top level of usage
		r = asInt(usage["reasoning_tokens"])
	}
	cached := 0
	if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
		cached = asInt(details["cached_tokens"])
	}
	return p, c, r, cached
}

func readAnthropicUsage(usage map[string]interface{}) (int, int, int) {
	p := asInt(usage["input_tokens"])
	c := asInt(usage["output_tokens"])
	r := asInt(usage["reasoning_tokens"])
	if r == 0 {
		// Anthropic sometimes nests it
		if details, ok := usage["thinking"].(map[string]interface{}); ok {
			r = asInt(details["tokens"])
		}
	}
	return p, c, r
}

// readDeepSeekUsage extracts DeepSeek usage: prompt_tokens (= hit + miss), completion_tokens,
// and the cache split (hit/miss). DeepSeek does not expose reasoning_tokens today.
func readDeepSeekUsage(usage map[string]interface{}) (int, int, int, int, int) {
	hit := asInt(usage["prompt_cache_hit_tokens"])
	miss := asInt(usage["prompt_cache_miss_tokens"])
	p := hit + miss
	if explicitPrompt, ok := usage["prompt_tokens"]; ok && asInt(explicitPrompt) > 0 {
		p = asInt(explicitPrompt)
	}
	c := asInt(usage["completion_tokens"])
	r := 0
	if details, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
		r = asInt(details["reasoning_tokens"])
	}
	return p, c, r, hit, miss
}

func readGoogleUsage(meta map[string]interface{}) (int, int, int, int) {
	p := asInt(meta["promptTokenCount"])
	c := asInt(meta["candidatesTokenCount"]) + asInt(meta["toolUsePromptTokenCount"])
	r := asInt(meta["thoughtsTokenCount"])
	cached := asInt(meta["cachedContentTokenCount"])
	return p, c, r, cached
}

func estimateCompletion(generic map[string]interface{}) int {
	choices, ok := generic["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return 0
	}
	first, ok := choices[0].(map[string]interface{})
	if !ok {
		return 0
	}
	if msg, ok := first["message"].(map[string]interface{}); ok {
		if s, ok := msg["content"].(string); ok && s != "" {
			t := CountTokens(s)
			if t == 0 {
				return 1
			}
			return t
		}
	}
	if delta, ok := first["delta"].(map[string]interface{}); ok {
		if s, ok := delta["content"].(string); ok && s != "" {
			return CountTokens(s)
		}
	}
	return 0
}

func asInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
