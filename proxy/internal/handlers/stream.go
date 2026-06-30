package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"synapse-proxy/internal/db"
	"synapse-proxy/internal/utils"
	"synapse-proxy/internal/workers"
	"synapse-proxy/optiagent"
)

func streamResponse(
	w http.ResponseWriter,
	resp *http.Response,
	vk, realKey, provider, model, defaultModel, payloadHash, clientModel string,
	vector []float32,
	promptOrig, promptOpt int,
	cacheLvl string,
	isBenchmark bool,
	rawPayload, optPayload []byte,
	startTime time.Time,
	wantStream bool,
	cacheTtl int,
	isNewModel bool,
	agentID, agentLabel, sessionID string,
	zeroLog bool,
	l0Capture *[]byte,
	toolCallsStr string,
	limitExceeded bool,
	turnCount int,
	convSignature string,
	hctx *optiagent.HookContext,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	upstreamCT := resp.Header.Get("Content-Type")
	if upstreamCT != "" {
		w.Header().Set("Content-Type", upstreamCT)
	} else if wantStream {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Per-request observability headers so the dashboard / Playground can
	// display stats without re-parsing the response body. Safe to expose:
	// they only contain aggregate token counts, cache level, and cost
	// deltas — never the prompt content.
	if cacheLvl != "" {
		w.Header().Set("X-SynapseProxy-Cache", cacheLvl)
		w.Header().Set("X-SynapseProxy-Tokens-In", strconv.Itoa(promptOrig))
		w.Header().Set("X-SynapseProxy-Tokens-Out", strconv.Itoa(promptOpt))
	}
	// Quick cost estimate using the same single-class helper as the
	// legacy dashboard headline. The full 4-class breakdown is computed
	// post-stream by the telemetry worker.
	if promptOrig > promptOpt {
		w.Header().Set("X-SynapseProxy-Cost-Saved", fmt.Sprintf("%.6f", utils.CalculateSavings(provider, model, promptOrig-promptOpt, 0)))
	}

	// Model re-stamping: if the client asked for a model that we aliased
	// upstream (e.g. "google/gemma-..." on a MiniMax-backed key), the
	// upstream will echo its own model name in every chunk. Re-stamp each
	// `data:` line so the client sees the model it asked for. We only do
	// the rewrite when clientModel != model (upstream model).
	needsRestamp := clientModel != "" && clientModel != model

	reader := bufio.NewReader(resp.Body)
	var fullResponse []byte
	var firstChunkLogged bool

	// Buffer for the first SSE event (data: {...}) to inspect upstream
	// application errors (e.g. MiniMax status_code != 0). When detected we
	// return a real HTTP 402/4xx to the client instead of forwarding a 200
	// with a poison body (which makes the agent hang waiting for chunks).
	var firstDataBuf []byte
	const maxFirstEventBytes = 64 * 1024

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if !firstChunkLogged {
				log.Printf("[streamResponse] Upstream sent first chunk: %s", string(line))
				firstChunkLogged = true
			}

			// Re-stamp "model" in `data:` payloads so the client sees the
			// model it asked for when we have aliased upstream.
			if needsRestamp && bytes.HasPrefix(line, []byte("data: ")) {
				line = utils.RestampModel(line, clientModel)
			}

			// Inspect the first data: line for an application error.
			if len(firstDataBuf) < maxFirstEventBytes && bytes.HasPrefix(line, []byte("data: ")) {
				firstDataBuf = append(firstDataBuf, line...)
				if err := detectUpstreamAppError(firstDataBuf); err != nil {
					log.Printf("[streamResponse] Upstream application error detected: %v", err)
					// Reject the request with a real HTTP error and stop streaming.
					w.Header().Set("Content-Type", "application/json")
					statusCode := http.StatusBadGateway
					if err.quota {
						statusCode = http.StatusPaymentRequired
					}
					w.WriteHeader(statusCode)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error": map[string]interface{}{
							"message": err.message,
							"type":    "upstream_application_error",
							"code":    err.statusCode,
						},
					})
					flusher.Flush()
					return
				}
			}

			if wantStream {
				w.Write(line)
				flusher.Flush()
			}
			fullResponse = append(fullResponse, line...)
		}

		if err != nil {
			if err != io.EOF {
				log.Printf("[streamResponse] Read error: %v", err)
			}
			break
		}
	}

	// Discover the real model name the upstream used. For SSE we have to
	// reconstruct the full body first; for non-streaming it's already
	// complete.
	var cacheableResponse []byte
	if wantStream {
		cacheableResponse = reconstructFromSSE(fullResponse, model)
	} else {
		cacheableResponse = fullResponse
	}

	// Anthropic-shape response -> OpenAI-shape response. When the
	// proxy forwarded via /anthropic/v1/messages the upstream replies
	// in Anthropic message format; we translate it back to the
	// OpenAI chat-completion shape that callers expect.
	if provider == "minimax-anthropic" {
		now := time.Now().Unix()
		translated, tErr := optiagent.AnthropicToOpenAI(cacheableResponse, now, clientModel)
		if tErr != nil {
			log.Printf("[streamResponse] Anthropic->OpenAI translation failed on vk=%s: %v (forwarding raw response)", vk, tErr)
		} else {
			cacheableResponse = translated
		}
	}

	// L0 capture: hand the upstream response back to ProxyHandler so it
	// can publish it for in-flight coalescing followers. Only valid JSON
	// (not upstream errors) is propagated.
	if l0Capture != nil && !wantStream && len(cacheableResponse) > 0 {
		var jsonMap map[string]interface{}
		if err := json.Unmarshal(cacheableResponse, &jsonMap); err == nil {
			if _, hasError := jsonMap["error"]; !hasError {
				*l0Capture = cacheableResponse
			}
		}
	}

	realModel := extractModelFromResponse(cacheableResponse, model)

	isValidResponse := false
	if resp.StatusCode == http.StatusOK && len(cacheableResponse) > 0 {
		isValidResponse = true
		var jsonMap map[string]interface{}
		if err := json.Unmarshal(cacheableResponse, &jsonMap); err == nil {
			if _, hasError := jsonMap["error"]; hasError {
				isValidResponse = false
			}
			if baseResp, hasBaseResp := jsonMap["base_resp"].(map[string]interface{}); hasBaseResp {
				if statusCode, ok := baseResp["status_code"].(float64); ok && statusCode != 0 {
					isValidResponse = false
				}
			}
		}
	}

	if isValidResponse {
		ctx := context.Background()
		hctx.UpstreamStatus = resp.StatusCode
		hctx.UpstreamResponse = cacheableResponse
		cacheableResponse = optiagent.RunAfterHooks(ctx, hctx)
	}

	if !wantStream {
		log.Printf("[streamResponse] Writing non-streaming response to client: %d bytes: %s", len(cacheableResponse), string(cacheableResponse))
		w.Header().Set("Content-Length", strconv.Itoa(len(cacheableResponse)))
		w.WriteHeader(resp.StatusCode)
		w.Write(cacheableResponse)
		flusher.Flush()
	}

	if payloadHash != "" && isValidResponse {
		ctx := context.Background()
		rdb := db.GetRedis()
		l1Key := "synapse:l1cache:" + vk + ":" + payloadHash
		ttl := time.Duration(cacheTtl) * time.Second

		// Zero-Log Mode: we still token-count and measure latency
		// (metadata is fine) but we do NOT store the response body in
		// L1/L2 cache, and we do NOT store it as a loop response. The
		// upstream provider still has the response (we just don't
		// keep it on our side).
		if zeroLog {
			hashPrefix := payloadHash
			if len(hashPrefix) > 12 {
				hashPrefix = hashPrefix[:12]
			}
			log.Printf("[streamResponse] Zero-Log Mode: skipping L1/L2/loop cache for vk=%s hash=%s", vk, hashPrefix)
		} else {
			rdb.Set(ctx, l1Key, cacheableResponse, ttl)

			// Feature 1 (continuation): remember this response as the
			// "first" of a potential loop. The 3rd+ identical call in the
			// next 60s will pull this from the loop cache instead of
			// re-hitting upstream.
			//
			// Safety net: don't cache a poisoned response (e.g. a MiniMax
			// quota error returned as an empty `content:""`). Same check
			// as the L1 cache.
			if !utils.IsCachedResponseAnError(cacheableResponse) {
				optiagent.StoreLoopFirstResponse(ctx, rdb, vk, payloadHash, cacheableResponse)
			}

			if len(vector) > 0 {
				buf := new(bytes.Buffer)
				if binary.Write(buf, binary.LittleEndian, vector) == nil {
					l2Key := "synapse:l2cache:" + vk + ":" + payloadHash
					rdb.HSet(ctx, l2Key, "vk", vk, "vector", buf.Bytes(), "response", cacheableResponse)
					rdb.Expire(ctx, l2Key, ttl)
				}
			}
		}
	}

	usage := utils.ExtractUsage(cacheableResponse)
	truePromptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens
	reasoningTokens := usage.ReasoningTokens

	// Model Radar: two complementary actions.
	// 1. If a previously-unknown model returned a parseable usage block,
	//    promote it to "known" so we stop flagging it.
	// 2. If we still couldn't parse usage from a flagged new model, store
	//    the raw response so we can later discover its fields.
	//
	// Under Zero-Log Mode we skip step 2 entirely (the raw response
	// contains user content and must never be persisted). Step 1 is
	// safe because it only stores metadata (the model name), no
	// content.
	if isNewModel && !zeroLog {
		if usage.Source != "estimated" && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
			go workers.PromoteKnown(context.Background(), db.GetRedis(), provider, realModel)
		} else if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
			// CollectSample is non-blocking; we add a follow-up discovery
			// attempt that runs the FieldDiscoverer on the accumulated
			// samples once we have enough of them. The goroutine is
			// safe to fire on every miss because TryDiscoverForModel
			// is idempotent and the sample list is bounded.
			go workers.CollectSample(context.Background(), db.GetRedis(), realModel, cacheableResponse)
			go workers.TryDiscoverForModel(context.Background(), db.GetRedis(), realModel)
		}
	}

	if truePromptTokens > 0 {
		// Calculate ratio of actual billed tokens vs our tiktoken estimation
		// To safely adjust promptOrig for L3 compression without apples-to-oranges comparison.
		if cacheLvl == "L3" && promptOpt > 0 {
			ratio := float64(truePromptTokens) / float64(promptOpt)
			promptOrig = int(float64(promptOrig) * ratio)
		}

		promptOpt = truePromptTokens

		// If no optimization was applied (Standard Routing), the original tokens
		// should match exactly what the provider billed, to avoid false "savings" anomalies.
		if cacheLvl == "NONE" {
			promptOrig = truePromptTokens
		}
	}

	completionOrig := completionTokens
	completionOpt := completionTokens

	perHookSavings := workers.BuildPerHookSavingsJSON(hctx)
	go workers.PushTelemetry(vk, provider, realModel, promptOrig, completionOrig, promptOpt, completionOpt, reasoningTokens, cacheLvl, time.Since(startTime), string(rawPayload), string(optPayload), string(cacheableResponse), usage.CacheCreationTokens, usage.CacheReadTokens, usage.CacheHitTokens, usage.CacheMissTokens, agentID, agentLabel, sessionID, zeroLog, toolCallsStr, limitExceeded, false, turnCount, convSignature, perHookSavings)

	if isBenchmark {
		go runBenchmarkEvaluation(vk, realKey, provider, realModel, defaultModel, rawPayload, optPayload, cacheableResponse, time.Since(startTime), promptOpt, completionOpt)
	}
}

func reconstructFromSSE(sseData []byte, model string) []byte {
	lines := strings.Split(string(sseData), "\n")
	var contentParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}
	discoveredModel := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			// Capture the upstream model name from the first chunk that
			// carries it. SSE chunks from OpenAI-compatible providers
			// include `"model":"..."` in every chunk.
			if discoveredModel == "" {
				if m, ok := chunk["model"].(string); ok && m != "" {
					discoveredModel = m
				}
			}
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok {
							contentParts = append(contentParts, content)
						}
						if reasoning, ok := delta["reasoning_content"].(string); ok {
							reasoningParts = append(reasoningParts, reasoning)
						}
						if tcs, ok := delta["tool_calls"].([]interface{}); ok {
							// Merge tool calls by index
							for _, tcIntf := range tcs {
								tc, ok := tcIntf.(map[string]interface{})
								if !ok {
									continue
								}
								index := -1
								if idxFloat, ok := tc["index"].(float64); ok {
									index = int(idxFloat)
								}

								// Expand toolCalls slice if needed
								for len(toolCalls) <= index {
									toolCalls = append(toolCalls, map[string]interface{}{})
								}

								if index >= 0 {
									merged := toolCalls[index]
									if id, ok := tc["id"].(string); ok {
										merged["id"] = id
									}
									if typ, ok := tc["type"].(string); ok {
										merged["type"] = typ
									}
									if fn, ok := tc["function"].(map[string]interface{}); ok {
										if merged["function"] == nil {
											merged["function"] = map[string]interface{}{"name": "", "arguments": ""}
										}
										mfn := merged["function"].(map[string]interface{})
										if name, ok := fn["name"].(string); ok {
											mfn["name"] = mfn["name"].(string) + name
										}
										if args, ok := fn["arguments"].(string); ok {
											mfn["arguments"] = mfn["arguments"].(string) + args
										}
									}
									toolCalls[index] = merged
								}
							}
						}
					}
				}
			}
		}
	}

	fullContent := strings.Join(contentParts, "")
	fullReasoning := strings.Join(reasoningParts, "")

	message := map[string]interface{}{
		"role":    "assistant",
		"content": fullContent,
	}
	if fullReasoning != "" {
		message["reasoning_content"] = fullReasoning
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": message, "finish_reason": "stop", "index": 0},
		},
		"model": pickModel(discoveredModel, model),
	}
	out, _ := json.Marshal(resp)
	return out
}

func streamCachedResponse(w http.ResponseWriter, cachedResp []byte, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedResp)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string                   `json:"content"`
				ToolCalls []map[string]interface{} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(cachedResp, &parsed); err != nil || len(parsed.Choices) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedResp)
		return
	}

	content := parsed.Choices[0].Message.Content
	toolCalls := parsed.Choices[0].Message.ToolCalls

	if len(toolCalls) > 0 {
		// Format tool calls for streaming by injecting the "index" property
		for i, tc := range toolCalls {
			tc["index"] = i
		}
		chunk := map[string]interface{}{
			"id":      "chatcmpl-cached",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{"delta": map[string]interface{}{
					"role":       "assistant",
					"content":    content,
					"tool_calls": toolCalls,
				}, "index": 0},
			},
			"model": model,
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	} else if content != "" {
		runes := []rune(content)
		chunkSize := 15
		for i := 0; i < len(runes); i += chunkSize {
			end := i + chunkSize
			if end > len(runes) {
				end = len(runes)
			}
			chunkText := string(runes[i:end])
			chunk := map[string]interface{}{
				"id":      "chatcmpl-cached",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"choices": []map[string]interface{}{
					{"delta": map[string]string{"content": chunkText}, "index": 0},
				},
				"model": model,
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	} else {
		// Empty content, no tools
		chunk := map[string]interface{}{
			"id":      "chatcmpl-cached",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": ""}, "index": 0},
			},
			"model": model,
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	// Send the final chunk with finish_reason
	finalChunk := map[string]interface{}{
		"id":      "chatcmpl-cached",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"choices": []map[string]interface{}{
			{"delta": map[string]string{}, "index": 0, "finish_reason": finishReason},
		},
		"model": model,
	}
	finalData, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", finalData)
	flusher.Flush()

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
