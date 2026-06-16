package optiagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
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

func extractTextForEmbedding(payload []byte) (string, bool, bool) {
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

		// If there are multiple non-system messages, it's a multi-turn conversation.
		// Semantic caching on full chat histories is dangerous because the vast context
		// dilutes the embedding of the newest message, causing false positive cache hits.
		// We disable L2 cache for multi-turn conversations.
		disableL2 := nonSystemCount > 1

		return strings.TrimSpace(text), hasImage, disableL2
	}
	return string(payload), false, false
}

func ProcessRequest(ctx context.Context, rdb *redis.Client, payload []byte, semanticTolerance float64, virtualKey string, isolateCache bool) (OptimizationResult, error) {
	if isolateCache {
		var payloadMap map[string]interface{}
		if err := json.Unmarshal(payload, &payloadMap); err == nil {
			if userStr, ok := payloadMap["user"].(string); ok && userStr != "" {
				virtualKey = virtualKey + ":" + userStr
			}
		}
	}

	embeddingText, hasImage, disableL2 := extractTextForEmbedding(payload)

	// Calculate tokens over the entire payload (not just embeddingText which skips system prompts)
	origTokens := countTokens(string(payload))
	if origTokens == 0 {
		origTokens = 1
	}

	hash := sha256.Sum256(payload)
	hashStr := hex.EncodeToString(hash[:])

	// Cache key prefix scoped to user's virtual key
	l1Key := "optitoken:l1cache:" + virtualKey + ":" + hashStr
	l2Prefix := "optitoken:l2cache:" + virtualKey + ":"

	// 1. L1 Cache (Exact Match) — per user
	cachedResp, err := rdb.Get(ctx, l1Key).Bytes()
	if err == nil && len(cachedResp) > 0 {
		return OptimizationResult{
			Payload:         payload,
			PayloadHash:     hashStr,
			CacheHitLevel:   "L1",
			PromptTokensOrig: origTokens,
			PromptTokensOpt:  0,
			HitResponse:     cachedResp,
		}, nil
	}

	var payloadVector []float32

	// 2. L2 Cache (Semantic Search via ONNX + Redis VSS) — filtered per user via TAG
	onnxUrl := os.Getenv("ONNX_API_URL")
	if onnxUrl != "" && !hasImage && !disableL2 { // BYPASS L2 if payload contains an image or is a multi-turn conversation
		reqBody, _ := json.Marshal(map[string]string{"text": embeddingText})
		resp, err := http.Post(onnxUrl, "application/json", bytes.NewBuffer(reqBody))
		if err == nil {
			defer resp.Body.Close()
			var onnxRes struct {
				Vector []float32 `json:"vector"`
			}
			if json.NewDecoder(resp.Body).Decode(&onnxRes) == nil && len(onnxRes.Vector) > 0 {
				payloadVector = onnxRes.Vector

				// Convert float32 array to byte array for Redis
				buf := new(bytes.Buffer)
				if err := binary.Write(buf, binary.LittleEndian, onnxRes.Vector); err == nil {
					vectorBytes := buf.Bytes()

					// Escape special chars in virtualKey for Redis TAG filter
					escapedVK := escapeRedisTag(virtualKey)

					// Search Redis VSS filtered by user's virtual key
					query := "(@vk:{" + escapedVK + "})=>[KNN 1 @vector $query_vec AS score]"
					res, err := rdb.Do(ctx, "FT.SEARCH", "idx:l2cache", query, "PARAMS", "2", "query_vec", vectorBytes, "RETURN", "2", "score", "response", "DIALECT", "2").Result()

					if err == nil {
						resArr := res.([]interface{})
						// FT.SEARCH returns [number_of_results, doc_id, [fields]]
						if len(resArr) > 2 {
							fields := resArr[2].([]interface{})
							var score float64
							var hitResponse string
							for i := 0; i < len(fields); i += 2 {
								key := fields[i].(string)
								if key == "score" {
									score, _ = strconv.ParseFloat(fields[i+1].(string), 64)
								} else if key == "response" {
									hitResponse = fields[i+1].(string)
								}
							}

							// If Cosine Distance < semanticTolerance (e.g. 0.15 = Similarity > 85%)
							if score < semanticTolerance && hitResponse != "" {
								return OptimizationResult{
									Payload:          payload,
									PayloadHash:      hashStr,
									Vector:           payloadVector,
									CacheHitLevel:    "L2",
									PromptTokensOrig: origTokens,
									PromptTokensOpt:  0,
									HitResponse:      []byte(hitResponse),
								}, nil
							}
						}
					}
				}
			}
		}
	}

	// Ensure l2Prefix is used (suppress unused warning) — used by main.go for cache population
	_ = l2Prefix

	// 3. L3 Cache (Compression / Payload Optimization)
	compressedPayload, err := CompressPayload(payload)
	if err != nil || compressedPayload == nil {
		compressedPayload = payload
	}
	optTokens := countTokens(string(compressedPayload))
	if optTokens == 0 {
		optTokens = 1
	}

	cacheHitLevel := "NONE"
	if len(compressedPayload) < len(payload) {
		cacheHitLevel = "L3"
	}

	return OptimizationResult{
		Payload:         compressedPayload,
		PayloadHash:     hashStr,
		Vector:          payloadVector,
		CacheHitLevel:   cacheHitLevel,
		PromptTokensOrig: origTokens,
		PromptTokensOpt:  optTokens,
		HitResponse:     nil, // Need to forward upstream
	}, nil
}

// escapeRedisTag escapes special characters for Redis TAG field queries
func escapeRedisTag(s string) string {
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
