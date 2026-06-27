package optiagent

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

var tke *tiktoken.Tiktoken

func init() {
	var err error
	tke, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		log.Printf("optiagent: failed to load tiktoken: %v", err)
	}
}

func countTokens(text string) int {
	if tke != nil {
		return len(tke.Encode(text, nil, nil))
	}
	return len(text) / 4
}


type OptimizationResult struct {
	Payload              []byte
	PayloadHash          string
	Vector               []float32
	CacheHitLevel        string // "NONE", "L1", "L2", "L3"
	PromptTokensOrig     int
	CompletionTokensOrig int
	PromptTokensOpt      int
	CompletionTokensOpt  int
	HitResponse          []byte
}

func ExtractTextForEmbedding(payload []byte) (string, bool, bool) {
	var body struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
	}
	hasImage := false
	nonSystemCount := 0
	if err := json.Unmarshal(payload, &body); err == nil && len(body.Messages) > 0 {
		var text string
		for _, msg := range body.Messages {
			if msg.Role == "system" {
				continue
			}
			nonSystemCount++
			if str, ok := msg.Content.(string); ok {
				text += str + "\n"
			}
			// basic image check for array content
			if arr, ok := msg.Content.([]interface{}); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]interface{}); ok {
						if m["type"] == "image_url" {
							hasImage = true
						}
					}
				}
			}
		}

		// Disable L2 (semantic) cache when the request looks like part
		// of a long agentic trajectory: the embeddings of consecutive
		// user prompts in a coding / agentic loop are very close
		// (same verbs: "ajoute", "refactor", "test", …) so the L2
		// cache will return responses from a *different* turn and
		// break the agent's context. We let L3 (compression) handle
		// these requests instead.
		//
		// Heuristic: more than 1 non-system message = the agent is
		// already in a multi-turn loop. We disable L2 (semantic
		// cache) at this point because consecutive user prompts in
		// a coding/agentic trajectory have very close embeddings
		// (same verbs, same nouns) and L2 would return a response
		// from a *different* turn, corrupting the agent's state and
		// causing the upstream provider to return malformed SSE
		// ("empty stream with no finish_reason"). L3 (compression)
		// is the right tool here.
		disableL2 := nonSystemCount > 1

		return strings.TrimSpace(text), hasImage, disableL2
	}
	return string(payload), false, false
}

// EscapeRedisTag escapes special characters for Redis TAG field queries
func EscapeRedisTag(s string) string {
	special := []byte{'.', '-', '@', '_', '+', '=', '/', '\\', ':', '|', '!', '(', ')', '{', '}', '[', ']', '^', '"', '~', '*', '?', '&', '%', '#', '$', ';', ','}
	result := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		for _, c := range special {
			if s[i] == c {
				result = append(result, '\\')
				break
			}
		}
		result = append(result, s[i])
	}
	return string(result)
}
