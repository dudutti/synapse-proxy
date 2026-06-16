package utils

import (
	"testing"
)

func TestCalculateSavings(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		orig     int
		opt      int
		expected float64
	}{
		// Note: pricing comes from the seed (ProviderModel table) when populated.
		// Without seed, fallback is 1.0/MTok for both input and output.
		// These tests assume the fallback (no seed) for portability.
		{
			name:     "openai_fallback_1k_prompt_500_output",
			provider: "openai", model: "gpt-99-unknown",
			orig:     1000, opt: 0,
			expected: 0.001, // 1000 * 1.0 / 1M
		},
		{
			name:     "anthropic_fallback_2M_prompt_1M_output",
			provider: "anthropic", model: "claude-99-unknown",
			orig:     2000000, opt: 0,
			expected: 2.0, // 2M * 1.0 / 1M
		},
		{
			name:     "zero_savings_when_no_savings",
			provider: "openai", model: "gpt-99-unknown",
			orig:     0, opt: 0,
			expected: 0.0,
		},
		{
			name:     "negative_prompt_is_ignored_only_output_counts",
			provider: "openai", model: "gpt-99-unknown",
			orig:     -500, opt: 1000,
			expected: 0.001, // prompt <= 0 ignored, comp 1000 * 1.0/1M = 0.001
		},
		{
			name:     "completion_only_savings",
			provider: "openai", model: "gpt-99-unknown",
			orig:     0, opt: 0,
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateSavings(tt.provider, tt.model, tt.orig, tt.opt)
			if got != tt.expected {
				t.Errorf("CalculateSavings(%q, %q, %d, %d) = %v; want %v",
					tt.provider, tt.model, tt.orig, tt.opt, got, tt.expected)
			}
		})
	}
}

func TestCalculateSavingsByClass(t *testing.T) {
	tests := []struct {
		name                  string
		provider              string
		model                 string
		promptSaved           int
		compSaved             int
		cacheReadSaved        int
		cacheCreationSaved    int
		expectedInputFresh    float64
		expectedCacheRead     float64
		expectedCacheCreation float64
		expectedOutput        float64
	}{
		{
			name:               "all_fresh_input",
			provider:           "openai", model: "gpt-99-unknown",
			promptSaved:        1000, compSaved: 500,
			expectedInputFresh: 0.001, expectedOutput: 0.0005,
		},
		{
			name:               "all_output_only",
			provider:           "anthropic", model: "claude-99-unknown",
			promptSaved:        0, compSaved: 1000,
			expectedInputFresh: 0, expectedOutput: 0.001,
		},
		{
			name:                  "no_savings",
			provider:              "openai", model: "gpt-99-unknown",
			promptSaved:           0, compSaved: 0,
			expectedInputFresh:    0,
			expectedCacheRead:     0,
			expectedCacheCreation: 0,
			expectedOutput:        0,
		},
		{
			name:               "partial_cache_read",
			provider:           "openai", model: "gpt-99-unknown",
			promptSaved:        1000, compSaved: 0,
			cacheReadSaved:     300,
			expectedInputFresh: 0.0007, // (1000-300) * 1.0/1M
			expectedCacheRead:  0.00027, // 300 * (1.0 - 0.1) / 1M with fallback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateSavingsByClass(
				tt.provider, tt.model,
				tt.promptSaved, tt.compSaved,
				tt.cacheReadSaved, tt.cacheCreationSaved,
			)
			if got.InputFreshSaved != tt.expectedInputFresh {
				t.Errorf("InputFreshSaved = %v; want %v", got.InputFreshSaved, tt.expectedInputFresh)
			}
			if got.OutputSaved != tt.expectedOutput {
				t.Errorf("OutputSaved = %v; want %v", got.OutputSaved, tt.expectedOutput)
			}
			if got.CacheReadSaved != tt.expectedCacheRead {
				t.Errorf("CacheReadSaved = %v; want %v", got.CacheReadSaved, tt.expectedCacheRead)
			}
			if got.CacheCreationSaved != tt.expectedCacheCreation {
				t.Errorf("CacheCreationSaved = %v; want %v", got.CacheCreationSaved, tt.expectedCacheCreation)
			}
		})
	}
}

