package utils

import (
	"testing"
)

func TestExtractUsage_OpenAIStandard(t *testing.T) {
	jsonPayload := []byte(`{
		"usage": {
			"prompt_tokens": 50,
			"completion_tokens": 100,
			"completion_tokens_details": {
				"reasoning_tokens": 25
			}
		},
		"choices": []
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d; want 50", got.PromptTokens)
	}
	if got.CompletionTokens != 100 {
		t.Errorf("CompletionTokens = %d; want 100", got.CompletionTokens)
	}
	if got.ReasoningTokens != 25 {
		t.Errorf("ReasoningTokens = %d; want 25", got.ReasoningTokens)
	}
	if got.Source != "openai" {
		t.Errorf("Source = %q; want \"openai\"", got.Source)
	}
}

func TestExtractUsage_AnthropicFormat(t *testing.T) {
	jsonPayload := []byte(`{
		"usage": {
			"input_tokens": 200,
			"output_tokens": 80
		},
		"content": [{"type": "text", "text": "hi"}]
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 200 {
		t.Errorf("PromptTokens = %d; want 200", got.PromptTokens)
	}
	if got.CompletionTokens != 80 {
		t.Errorf("CompletionTokens = %d; want 80", got.CompletionTokens)
	}
	if got.Source != "anthropic" {
		t.Errorf("Source = %q; want \"anthropic\"", got.Source)
	}
}

func TestExtractUsage_GoogleFormat(t *testing.T) {
	jsonPayload := []byte(`{
		"usageMetadata": {
			"promptTokenCount": 30,
			"candidatesTokenCount": 40,
			"thoughtsTokenCount": 15
		}
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 30 {
		t.Errorf("PromptTokens = %d; want 30", got.PromptTokens)
	}
	if got.CompletionTokens != 40 {
		t.Errorf("CompletionTokens = %d; want 40", got.CompletionTokens)
	}
	if got.ReasoningTokens != 15 {
		t.Errorf("ReasoningTokens = %d; want 15", got.ReasoningTokens)
	}
	if got.Source != "google" {
		t.Errorf("Source = %q; want \"google\"", got.Source)
	}
}

func TestExtractUsage_Fallback(t *testing.T) {
	jsonPayload := []byte(`{
		"choices": [{
			"message": {
				"content": "Hello world this is a test response."
			}
		}]
	}`)

	// Without initialization, tiktoken is nil, so it falls back to len/4.
	// "Hello world this is a test response." has 36 chars. 36 / 4 = 9 tokens.
	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 0 {
		t.Errorf("Expected 0 prompt tokens for fallback, got %d", got.PromptTokens)
	}
	if got.CompletionTokens != 9 {
		t.Errorf("Expected 9 completion tokens for fallback, got %d", got.CompletionTokens)
	}
	if got.Source != "estimated" {
		t.Errorf("Source = %q; want \"estimated\"", got.Source)
	}
}

func TestExtractUsage_DiscoveredProprietary(t *testing.T) {
	// A made-up provider that nests usage under a custom name.
	jsonPayload := []byte(`{
		"meta": {
			"usage": {
				"request_tokens": 11,
				"response_tokens": 22
			}
		}
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 11 {
		t.Errorf("PromptTokens = %d; want 11 (discovered)", got.PromptTokens)
	}
	if got.CompletionTokens != 22 {
		t.Errorf("CompletionTokens = %d; want 22 (discovered)", got.CompletionTokens)
	}
}

func TestExtractUsage_AnthropicCacheTokens(t *testing.T) {
	jsonPayload := []byte(`{
		"usage": {
			"input_tokens": 200,
			"output_tokens": 100,
			"cache_creation_input_tokens": 200,
			"cache_read_input_tokens": 1500
		},
		"content": [{"type": "text", "text": "hi"}]
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 200 {
		t.Errorf("PromptTokens = %d; want 200", got.PromptTokens)
	}
	if got.CompletionTokens != 100 {
		t.Errorf("CompletionTokens = %d; want 100", got.CompletionTokens)
	}
	if got.CacheCreationTokens != 200 {
		t.Errorf("CacheCreationTokens = %d; want 200", got.CacheCreationTokens)
	}
	if got.CacheReadTokens != 1500 {
		t.Errorf("CacheReadTokens = %d; want 1500", got.CacheReadTokens)
	}
	if got.Source != "anthropic" {
		t.Errorf("Source = %q; want \"anthropic\"", got.Source)
	}
}

func TestExtractUsage_OpenAICachedTokens(t *testing.T) {
	jsonPayload := []byte(`{
		"usage": {
			"prompt_tokens": 2000,
			"completion_tokens": 300,
			"prompt_tokens_details": {
				"cached_tokens": 1920
			}
		},
		"choices": []
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 2000 {
		t.Errorf("PromptTokens = %d; want 2000", got.PromptTokens)
	}
	if got.CompletionTokens != 300 {
		t.Errorf("CompletionTokens = %d; want 300", got.CompletionTokens)
	}
	if got.CacheReadTokens != 1920 {
		t.Errorf("CacheReadTokens (OpenAI cached_tokens) = %d; want 1920", got.CacheReadTokens)
	}
	if got.Source != "openai" {
		t.Errorf("Source = %q; want \"openai\"", got.Source)
	}
}

func TestExtractUsage_DeepSeekCache(t *testing.T) {
	jsonPayload := []byte(`{
		"usage": {
			"prompt_cache_hit_tokens": 100,
			"prompt_cache_miss_tokens": 20,
			"completion_tokens": 50
		},
		"choices": []
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 120 {
		t.Errorf("PromptTokens = %d; want 120 (hit+miss)", got.PromptTokens)
	}
	if got.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d; want 50", got.CompletionTokens)
	}
	if got.CacheHitTokens != 100 {
		t.Errorf("CacheHitTokens = %d; want 100", got.CacheHitTokens)
	}
	if got.CacheMissTokens != 20 {
		t.Errorf("CacheMissTokens = %d; want 20", got.CacheMissTokens)
	}
	if got.Source != "deepseek" {
		t.Errorf("Source = %q; want \"deepseek\"", got.Source)
	}
}

func TestExtractUsage_GoogleCachedContent(t *testing.T) {
	jsonPayload := []byte(`{
		"usageMetadata": {
			"promptTokenCount": 200,
			"candidatesTokenCount": 80,
			"cachedContentTokenCount": 150
		}
	}`)

	got := ExtractUsage(jsonPayload)
	if got.PromptTokens != 200 {
		t.Errorf("PromptTokens = %d; want 200", got.PromptTokens)
	}
	if got.CompletionTokens != 80 {
		t.Errorf("CompletionTokens = %d; want 80", got.CompletionTokens)
	}
	if got.CacheHitTokens != 150 {
		t.Errorf("CacheHitTokens (Google cachedContentTokenCount) = %d; want 150", got.CacheHitTokens)
	}
	if got.Source != "google" {
		t.Errorf("Source = %q; want \"google\"", got.Source)
	}
}
