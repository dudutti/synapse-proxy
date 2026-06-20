package optiagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"synapse-proxy/cache"

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

		// Disable L2 (semantic) cache when the request looks like part
		// of a long agentic trajectory: the embeddings of consecutive
		// user prompts in a coding / agentic loop are very close
		// (same verbs: "ajoute", "refactor", "test", â€¦) so the L2
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

func ProcessRequest(ctx context.Context, rdb *redis.Client, payload []byte, semanticTolerance float64, virtualKey string, isolateCache bool, forceDisableL2 bool, enableL1 bool, enableL2 bool, enableL3 bool, limitExceeded bool, cacheTtl int, toolTtls string) (OptimizationResult, error) {
	if isolateCache {
		var payloadMap map[string]interface{}
		if err := json.Unmarshal(payload, &payloadMap); err == nil {
			if userStr, ok := payloadMap["user"].(string); ok && userStr != "" {
				virtualKey = virtualKey + ":" + userStr
			}
		}
	}

	embeddingText, hasImage, autoDisableL2 := extractTextForEmbedding(payload)

	// Caller (proxy.go) can force L2 off â€” used when a Record Session
	// is active or when the request body contains tool_calls (an
	// agent SDK is in the middle of a tool loop).
	disableL2 := autoDisableL2 || forceDisableL2

	// Calculate tokens over the entire payload (not just embeddingText which skips system prompts)
	origTokens := countTokens(string(payload))
	if origTokens == 0 {
		origTokens = 1
	}

	hash := sha256.Sum256(payload)
	hashStr := hex.EncodeToString(hash[:])

	// Cache key prefix scoped to user's virtual key
	l1Key := "synapse:l1cache:" + virtualKey + ":" + hashStr
	l2Prefix := "synapse:l2cache:" + virtualKey + ":"

	// 1. L1 Cache (Exact Match) — per user
	if enableL1 {
		cachedResp, err := rdb.Get(ctx, l1Key).Bytes()
		if err == nil && len(cachedResp) > 0 {
			if ShouldReuseCache(ctx, rdb, payload, l1Key, cacheTtl, toolTtls) {
				hitResponse := cachedResp
				if limitExceeded {
					hitResponse = nil // Do not return response to upstream, force proxy to hit provider
				}
				return OptimizationResult{
					Payload:         payload,
					PayloadHash:     hashStr,
					CacheHitLevel:   "L1",
					PromptTokensOrig: origTokens,
					PromptTokensOpt:  0,
					HitResponse:     hitResponse,
				}, nil
			} else {
				log.Printf("[optiagent] L1 cache hit for stateful tool call is stale or disabled, treating as miss")
			}
		}
	}

	var payloadVector []float32

	// 2. L2 Cache (Semantic Search via local CGO Rust Embedder + Redis VSS) — filtered per user via TAG
	if enableL2 && cache.GlobalEmbedder != nil && !hasImage && !disableL2 {
		vector, err := cache.GlobalEmbedder.GenerateEmbedding(embeddingText)
		if err == nil && len(vector) > 0 {
			payloadVector = vector

			// Convert float32 array to byte array for Redis
			buf := new(bytes.Buffer)
			if err := binary.Write(buf, binary.LittleEndian, vector); err == nil {
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
								docKey, _ := resArr[1].(string)
								if ShouldReuseCache(ctx, rdb, payload, docKey, cacheTtl, toolTtls) {
									var hitBytes []byte
									if !limitExceeded {
										hitBytes = []byte(hitResponse)
									}
									return OptimizationResult{
										Payload:          payload,
										PayloadHash:      hashStr,
										Vector:           payloadVector,
										CacheHitLevel:    "L2",
										PromptTokensOrig: origTokens,
										PromptTokensOpt:  0,
										HitResponse:      hitBytes,
										
									}, nil
								} else {
									log.Printf("[optiagent] L2 cache hit for stateful tool call is stale or disabled, treating as miss")
								}
							}
						}
					}
				}
			}
		}

	// Ensure l2Prefix is used (suppress unused warning) â€” used by main.go for cache population
	_ = l2Prefix

	// 3. L3 Cache (Compression / Payload Optimization)
	//
	// The cache-preserving variant splits the payload into a
	// static prefix and a dynamic tail, compresses only the
	// tail, and reassembles the result. The prefix bytes are
	// byte-for-byte identical to the input, so provider
	// prompt caches (Anthropic, OpenAI, MiniMax) keep hitting
	// across requests. The tail gets the full L3 treatment
	// (CoT pruning, tool output truncation, repeated-tool
	// compaction, reasoning_content stripping).
	//
	// We use the cache-preserving variant by default; the
	// legacy CompressPayload is still available behind
	// `!useCachePreservingL3` for debugging and A/B tests.
	var compressedPayload []byte
	var err error
	if enableL3 {
		compressedPayload, err = CompressPayloadCachePreserving(payload)
		if err != nil || compressedPayload == nil {
			compressedPayload = payload
		}
	} else {
		compressedPayload = payload
	}
	optTokens := countTokens(string(compressedPayload))
	if optTokens == 0 {
		optTokens = 1
	}

	cacheHitLevel := "NONE"
	// Only mark as L3 and use the compressed payload if it is actually
	// smaller â€” both in bytes AND in tokens. The JSON re-encoding done
	// by CompressPayload can grow the payload (re-indented maps, key
	// reordering, escapes) which would otherwise hurt the upstream
	// provider and cancel out any tool-output pruning benefit.
	if enableL3 && len(compressedPayload) < len(payload) && optTokens < origTokens {
		cacheHitLevel = "L3"
		if limitExceeded {
			compressedPayload = payload // Keep original payload if simulated
		}
	} else {
		// Compression made it worse â€” keep the original payload and
		// reset token counts so the dashboard's "saved" column does
		// not go negative.
		compressedPayload = payload
		optTokens = origTokens
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
