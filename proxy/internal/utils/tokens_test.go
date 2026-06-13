package utils

import (
	"testing"
)

func TestExtractUsage(t *testing.T) {
	jsonPayload := []byte(`{
		"usage": {
			"prompt_tokens": 50,
			"completion_tokens": 100
		},
		"choices": []
	}`)

	p, c := ExtractUsage(jsonPayload)
	if p != 50 || c != 100 {
		t.Errorf("ExtractUsage() = %d, %d; want 50, 100", p, c)
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
	p, c := ExtractUsage(jsonPayload)
	if p != 0 {
		t.Errorf("Expected 0 prompt tokens for fallback, got %d", p)
	}
	if c != 9 {
		t.Errorf("Expected 9 completion tokens for fallback, got %d", c)
	}
}
