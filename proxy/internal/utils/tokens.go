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

// ExtractUsage parses an LLM JSON response and extracts prompt/completion token usage
func ExtractUsage(respBytes []byte) (int, int) {
	var body struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBytes, &body); err == nil {
		pToks := body.Usage.PromptTokens
		cToks := body.Usage.CompletionTokens
		
		// Fallback token estimation if usage is missing (common in SSE streaming)
		if cToks == 0 && len(body.Choices) > 0 {
			cToks = CountTokens(body.Choices[0].Message.Content)
			if cToks == 0 && len(body.Choices[0].Message.Content) > 0 {
				cToks = 1
			}
		}
		return pToks, cToks
	}
	return 0, 0
}
