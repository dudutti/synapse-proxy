// Tests for the real tokenizer wrapper. We use
// tiktoken-go (port of OpenAI tiktoken) instead of the
// naive bytes/4 approximation. The real token count
// matters because providers bill by tokens, not bytes.

package utils

import (
	"strings"
	"testing"
)

func TestCountTokens_Hello(t *testing.T) {
	n := CountTokens("Hello world")
	// cl100k_base: "Hello"=1 tok, " world"=1 tok => 2
	if n != 2 {
		t.Fatalf("Hello world: expected 2 tokens, got %d", n)
	}
}

func TestCountTokens_Empty(t *testing.T) {
	if n := CountTokens(""); n != 0 {
		t.Fatalf("empty: expected 0, got %d", n)
	}
}

func TestCountTokens_LongText(t *testing.T) {
	// 1000 chars of "a" repeated. ~250 tokens (cl100k_base).
	long := strings.Repeat("a", 1000)
	n := CountTokens(long)
	if n < 100 || n > 400 {
		t.Fatalf("1000 'a' chars: expected 200-400 tokens, got %d", n)
	}
}