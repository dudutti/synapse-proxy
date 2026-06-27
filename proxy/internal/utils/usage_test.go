// Tests for ExtractUsage: parses OpenAI / Anthropic /
// Gemini shapes (plus SSE streams) into the unified
// Usage struct.

package utils

import "testing"

func TestExtractUsage_OpenAI(t *testing.T) {
	body := []byte(`{"id":"x","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50}}`)
	u := ExtractUsage(body)
	if u.PromptTokens != 100 {
		t.Errorf("prompt: %d", u.PromptTokens)
	}
	if u.CompletionTokens != 50 {
		t.Errorf("completion: %d", u.CompletionTokens)
	}
	if u.Source != "openai_or_anthropic" {
		t.Errorf("source: %s", u.Source)
	}
}

func TestExtractUsage_OpenAI_CachedTokens(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":30}}}`)
	u := ExtractUsage(body)
	if u.CacheReadTokens != 30 {
		t.Errorf("cache_read: %d", u.CacheReadTokens)
	}
}

func TestExtractUsage_Anthropic(t *testing.T) {
	body := []byte(`{"id":"x","content":[],"usage":{"input_tokens":200,"output_tokens":80,"cache_read_input_tokens":40,"cache_creation_input_tokens":20}}`)
	u := ExtractUsage(body)
	if u.PromptTokens != 200 {
		t.Errorf("input -> prompt: %d", u.PromptTokens)
	}
	if u.CompletionTokens != 80 {
		t.Errorf("output -> completion: %d", u.CompletionTokens)
	}
	if u.CacheReadTokens != 40 {
		t.Errorf("cache_read: %d", u.CacheReadTokens)
	}
	if u.CacheCreationTokens != 20 {
		t.Errorf("cache_creation: %d", u.CacheCreationTokens)
	}
}

func TestExtractUsage_Gemini(t *testing.T) {
	body := []byte(`{"candidates":[],"usageMetadata":{"promptTokenCount":300,"candidatesTokenCount":120,"cachedContentTokenCount":50,"thoughtsTokenCount":15}}`)
	u := ExtractUsage(body)
	if u.PromptTokens != 300 {
		t.Errorf("prompt: %d", u.PromptTokens)
	}
	if u.CompletionTokens != 120 {
		t.Errorf("completion: %d", u.CompletionTokens)
	}
	if u.CacheReadTokens != 50 {
		t.Errorf("cache_read: %d", u.CacheReadTokens)
	}
	if u.ReasoningTokens != 15 {
		t.Errorf("thoughts -> reasoning: %d", u.ReasoningTokens)
	}
	if u.Source != "gemini" {
		t.Errorf("source: %s", u.Source)
	}
}

func TestExtractUsage_SSE(t *testing.T) {
	body := []byte("data: {\"id\":\"x\",\"choices\":[]}\n\ndata: {\"id\":\"x\",\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n")
	u := ExtractUsage(body)
	if u.PromptTokens != 10 {
		t.Errorf("SSE prompt: %d", u.PromptTokens)
	}
	if u.CompletionTokens != 5 {
		t.Errorf("SSE completion: %d", u.CompletionTokens)
	}
}

func TestExtractUsage_Empty(t *testing.T) {
	if u := ExtractUsage(nil); u.PromptTokens != 0 || u.Source != "" {
		t.Errorf("empty: %+v", u)
	}
}

func TestExtractUsage_NoUsage(t *testing.T) {
	body := []byte(`{"id":"x","choices":[{"delta":{"content":"hi"}}]}`)
	u := ExtractUsage(body)
	if u.PromptTokens != 0 {
		t.Errorf("no usage: %+v", u)
	}
}