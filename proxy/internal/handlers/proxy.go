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
	"os"
	"regexp"
	"strings"
	"time"

	"optitoken/internal/db"
	"optitoken/internal/services"
	"optitoken/internal/utils"
	"optitoken/internal/workers"
	"optitoken/optiagent"
	"github.com/redis/go-redis/v9"
)

// ProxyHandler is the main HTTP handler intercepting LLM requests
func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	virtualKey := r.Header.Get("Authorization")
	virtualKey = strings.TrimPrefix(virtualKey, "Bearer ")
	
	// Fallback to default key for local apps (like LMStudio) that don't send auth
	if virtualKey == "" || virtualKey == "lm-studio" {
		virtualKey = os.Getenv("DEFAULT_VIRTUAL_KEY")
	}
	
	if virtualKey == "" {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	authHeader := "Bearer " + virtualKey
	_, realKey, provider, fallbackKey, fallbackProvider, isBenchmark, semanticTolerance, cacheTtl, defaultModel, isolateCache, err := services.ValidateVirtualKey(ctx, authHeader)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	reqModel := "unknown"
	wantStream := false
	var payloadMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payloadMap); err == nil {
		if m, ok := payloadMap["model"].(string); ok && m != "" {
			reqModel = m
		}
		if s, ok := payloadMap["stream"].(bool); ok {
			wantStream = s
		}
	}

	isBypassStr := r.Header.Get("X-Bypass-Cache")
	isBypass := isBypassStr == "true"
	log.Printf("[ProxyHandler] Received X-Bypass-Cache: %q -> isBypass: %v", isBypassStr, isBypass)
	
	var optResult optiagent.OptimizationResult
	rdb := db.GetRedis()

	if !isBypass {
		optResult, err = optiagent.ProcessRequest(ctx, rdb, bodyBytes, semanticTolerance, virtualKey, isolateCache)
		if err != nil {
			http.Error(w, "Optimization engine failure", http.StatusInternalServerError)
			return
		}

		if optResult.CacheHitLevel == "L1" || optResult.CacheHitLevel == "L2" {
			_, completionTokens := utils.ExtractUsage(optResult.HitResponse)

			if wantStream {
				streamCachedResponse(w, optResult.HitResponse, reqModel)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Write(optResult.HitResponse)
			}
			
			go workers.PushTelemetry(virtualKey, provider, reqModel, optResult.PromptTokensOrig, completionTokens, optResult.PromptTokensOpt, 0, optResult.CacheHitLevel, time.Since(startTime))
			
			if isBenchmark {
				go runBenchmarkEvaluation(virtualKey, realKey, provider, reqModel, defaultModel, bodyBytes, optResult.Payload, optResult.HitResponse, time.Since(startTime), 0, 0)
			}
			return
		}
	} else {
		optResult = optiagent.OptimizationResult{
			Payload: bodyBytes,
			PromptTokensOrig: utils.CountTokens(string(bodyBytes)), 
			PromptTokensOpt: utils.CountTokens(string(bodyBytes)),
			CacheHitLevel: "BYPASS",
		}
	}

	executeRequest := func(currentProvider, currentRealKey string) (*http.Response, error) {
		var targetURL string
		switch currentProvider {
		case "anthropic":
			targetURL = "https://api.anthropic.com/v1/messages"
		case "google":
			targetURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
		case "minimax":
			targetURL = "https://api.minimax.io/v1/text/chatcompletion_v2"
		case "deepseek":
			targetURL = "https://api.deepseek.com/chat/completions"
		case "mistral":
			targetURL = "https://api.mistral.ai/v1/chat/completions"
		case "openrouter":
			targetURL = "https://openrouter.ai/api/v1/chat/completions"
		case "groq":
			targetURL = "https://api.groq.com/openai/v1/chat/completions"
		case "together":
			targetURL = "https://api.together.xyz/v1/chat/completions"
		case "perplexity":
			targetURL = "https://api.perplexity.ai/chat/completions"
		default:
			targetURL = "https://api.openai.com/v1/chat/completions"
		}

		upstreamPayload := optResult.Payload
		var pMap map[string]interface{}
		if err := json.Unmarshal(upstreamPayload, &pMap); err == nil {
			modified := false
			if wantStream {
				pMap["stream"] = true
				modified = true
			}
			
			forceModel := defaultModel
			if forceModel == "" {
				forceModel = os.Getenv("FORCE_MODEL")
			}
			if forceModel != "" {
				pMap["model"] = forceModel
				modified = true
			}
			
			if modified {
				if rewritten, err := json.Marshal(pMap); err == nil {
					upstreamPayload = rewritten
				}
			}
		}

		req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(upstreamPayload))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		if currentProvider == "anthropic" {
			req.Header.Set("x-api-key", currentRealKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+currentRealKey)
		}

		client := &http.Client{Timeout: 90 * time.Second}
		return client.Do(req)
	}

	maxRetries := 3
	var resp *http.Response
	var reqErr error
	usedProvider := provider

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoffDur := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("Upstream provider %s failed. Retrying in %v (attempt %d/%d)...", usedProvider, backoffDur, attempt, maxRetries)
			time.Sleep(backoffDur)
		}

		resp, reqErr = executeRequest(usedProvider, realKey)
		
		if reqErr == nil && resp.StatusCode < 429 && resp.StatusCode != 408 {
			break
		}
		
		if resp != nil {
			resp.Body.Close()
		}
	}

	if (reqErr != nil || (resp != nil && (resp.StatusCode >= 429 || resp.StatusCode == 408))) && fallbackProvider != "" && fallbackKey != "" {
		log.Printf("Primary provider %s exhausted. Failing over to fallback provider: %s", provider, fallbackProvider)
		usedProvider = fallbackProvider
		resp, reqErr = executeRequest(fallbackProvider, fallbackKey)
	}

	if reqErr != nil || (resp != nil && resp.StatusCode >= 400) {
		status := http.StatusBadGateway
		if resp != nil {
			status = resp.StatusCode
		}
		log.Printf("All upstream providers failed. Last error: %v, Status: %d", reqErr, status)
		http.Error(w, "Failed to reach upstream provider", status)
		return
	}
	defer resp.Body.Close()

	streamResponse(w, resp, virtualKey, realKey, usedProvider, reqModel, defaultModel, optResult.PayloadHash, optResult.Vector, optResult.PromptTokensOrig, optResult.PromptTokensOpt, optResult.CacheHitLevel, isBenchmark, bodyBytes, optResult.Payload, startTime, wantStream, cacheTtl)
}

func streamResponse(w http.ResponseWriter, resp *http.Response, vk, realKey, provider, model, defaultModel, payloadHash string, vector []float32, promptOrig, promptOpt int, cacheLvl string, isBenchmark bool, rawPayload, optPayload []byte, startTime time.Time, wantStream bool, cacheTtl int) {
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

	modelRegex := regexp.MustCompile(`"model":"[^"]+"`)
	replacement := []byte(`"model":"` + model + `"`)

	reader := bufio.NewReader(resp.Body)
	var fullResponse []byte
	var firstChunkLogged bool

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if wantStream && bytes.HasPrefix(line, []byte("data: {")) {
				line = modelRegex.ReplaceAll(line, replacement)
			}
			
			if !firstChunkLogged {
				log.Printf("[streamResponse] Upstream sent first chunk: %s", string(line))
				firstChunkLogged = true
			}
			
			w.Write(line)
			flusher.Flush()
			fullResponse = append(fullResponse, line...)
		}
		
		if err != nil {
			if err != io.EOF {
				log.Printf("[streamResponse] Read error: %v", err)
			}
			break
		}
	}

	var cacheableResponse []byte
	if wantStream {
		cacheableResponse = reconstructFromSSE(fullResponse, model)
	} else {
		cacheableResponse = fullResponse
	}

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

	if payloadHash != "" && isValidResponse {
		ctx := context.Background()
		rdb := db.GetRedis()
		l1Key := "optitoken:l1cache:" + vk + ":" + payloadHash
		ttl := time.Duration(cacheTtl) * time.Second
		rdb.Set(ctx, l1Key, cacheableResponse, ttl)

		if len(vector) > 0 {
			buf := new(bytes.Buffer)
			if binary.Write(buf, binary.LittleEndian, vector) == nil {
				l2Key := "optitoken:l2cache:" + vk + ":" + payloadHash
				rdb.HSet(ctx, l2Key, "vk", vk, "vector", buf.Bytes(), "response", cacheableResponse)
				rdb.Expire(ctx, l2Key, ttl)
			}
		}
	}

	truePromptTokens, completionTokens := utils.ExtractUsage(cacheableResponse)
	
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

	go workers.PushTelemetry(vk, provider, model, promptOrig, completionOrig, promptOpt, completionOpt, cacheLvl, time.Since(startTime))

	if isBenchmark {
		go runBenchmarkEvaluation(vk, realKey, provider, model, defaultModel, rawPayload, optPayload, cacheableResponse, time.Since(startTime), promptOpt, completionOpt)
	}
}

func reconstructFromSSE(sseData []byte, model string) []byte {
	lines := strings.Split(string(sseData), "\n")
	var contentParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}

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
		"model": model,
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
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(cachedResp, &parsed); err != nil || len(parsed.Choices) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedResp)
		return
	}

	content := parsed.Choices[0].Message.Content
	runes := []rune(content)
	chunkSize := 15
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunkText := string(runes[i:end])
		chunk := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": chunkText}, "index": 0},
			},
			"model": model,
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	
	// Send the final chunk with finish_reason
	finalChunk := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]string{}, "index": 0, "finish_reason": "stop"},
		},
		"model": model,
	}
	finalData, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", finalData)
	flusher.Flush()

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func runBenchmarkEvaluation(vk, realKey, provider, model, defaultModel string, rawPayload, optPayload, optimizedResponse []byte, optDuration time.Duration, promptOpt, completionOpt int) {
	start := time.Now()
	
	var upstreamURL string
	switch provider {
	case "anthropic":
		upstreamURL = "https://api.anthropic.com/v1/messages"
	case "google":
		upstreamURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case "minimax":
		upstreamURL = "https://api.minimax.io/v1/text/chatcompletion_v2"
	case "deepseek":
		upstreamURL = "https://api.deepseek.com/chat/completions"
	case "mistral":
		upstreamURL = "https://api.mistral.ai/v1/chat/completions"
	case "openrouter":
		upstreamURL = "https://openrouter.ai/api/v1/chat/completions"
	case "groq":
		upstreamURL = "https://api.groq.com/openai/v1/chat/completions"
	case "together":
		upstreamURL = "https://api.together.xyz/v1/chat/completions"
	case "perplexity":
		upstreamURL = "https://api.perplexity.ai/chat/completions"
	default:
		upstreamURL = "https://api.openai.com/v1/chat/completions"
	}
	
	// Create context with timeout for background task to prevent goroutine leak
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Rewrite model for the control request if necessary
	upstreamPayload := rawPayload
	var pMap map[string]interface{}
	if err := json.Unmarshal(rawPayload, &pMap); err == nil {
		forceModel := defaultModel
		if forceModel == "" {
			forceModel = os.Getenv("FORCE_MODEL")
		}
		if forceModel != "" {
			pMap["model"] = forceModel
		}
		pMap["stream"] = false // Force non-streaming for the benchmark control request
		delete(pMap, "stream_options") // stream_options is forbidden when stream=false
		if rewritten, err := json.Marshal(pMap); err == nil {
			upstreamPayload = rewritten
		}
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewBuffer(upstreamPayload))
	req.Header.Set("Content-Type", "application/json")
	if provider == "anthropic" {
		req.Header.Set("x-api-key", realKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+realKey)
	}
	
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Benchmark error: %v", err)
		return
	}
	defer resp.Body.Close()
	
	unoptResp, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Benchmark control request failed: %s - %s", resp.Status, string(unoptResp))
	}
	
	unoptDuration := time.Since(start)

	extractContent := func(payload []byte) string {
		var body struct {
			Choices []struct {
				Message struct {
					Content   string      `json:"content"`
					ToolCalls interface{} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(payload, &body); err == nil && len(body.Choices) > 0 {
			msg := body.Choices[0].Message
			if msg.Content != "" {
				return msg.Content
			}
			if msg.ToolCalls != nil {
				tcBytes, _ := json.Marshal(msg.ToolCalls)
				return string(tcBytes)
			}
		}
		return ""
	}

	score := 95
	feedback := "Fallback mocked score"

	origContent := extractContent(unoptResp)
	optContent := extractContent(optimizedResponse)
	
	if origContent == "" {
		log.Printf("Benchmark extractContent(unoptResp) failed. Body: %s", string(unoptResp))
	}
	if optContent == "" {
		log.Printf("Benchmark extractContent(optimizedResponse) failed.")
	}

	if origContent != "" && optContent != "" {
		evalPrompt := fmt.Sprintf(`Compare Response A and Response B. Rate how semantically similar they are from 0 to 100. Return ONLY a valid JSON object with {"score": <integer>, "feedback": "<1 sentence explanation>"}.

Response A:
%s

Response B:
%s`, origContent, optContent)

		evalModel := model
		forceModel := defaultModel
		if forceModel == "" {
			forceModel = os.Getenv("FORCE_MODEL")
		}
		if forceModel != "" {
			evalModel = forceModel
		}

		evalReqBody := map[string]interface{}{
			"model": evalModel,
			"messages": []map[string]string{
				{"role": "user", "content": evalPrompt},
			},
		}
		evalBodyBytes, _ := json.Marshal(evalReqBody)

		evalReq, _ := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewBuffer(evalBodyBytes))
		evalReq.Header.Set("Content-Type", "application/json")
		if provider == "anthropic" {
			evalReq.Header.Set("x-api-key", realKey)
			evalReq.Header.Set("anthropic-version", "2023-06-01")
		} else {
			evalReq.Header.Set("Authorization", "Bearer "+realKey)
		}
		
		evalResp, evalErr := client.Do(evalReq)
		if evalErr == nil {
			defer evalResp.Body.Close()
			evalRespBytes, _ := io.ReadAll(evalResp.Body)
			evalText := extractContent(evalRespBytes)
			
			var evalData struct {
				Score    int    `json:"score"`
				Feedback string `json:"feedback"`
			}
			evalText = strings.TrimSpace(evalText)
			evalText = strings.TrimPrefix(evalText, "```json\n")
			evalText = strings.TrimSuffix(evalText, "\n```")
			evalText = strings.TrimSuffix(evalText, "```")
			
			if err := json.Unmarshal([]byte(evalText), &evalData); err == nil {
				score = evalData.Score
				feedback = evalData.Feedback
			}
		}
	}

	rdb := db.GetRedis()
	rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: "optitoken:benchmark_logs",
		Values: map[string]interface{}{
			"vk": vk,
			"orig_prompt": string(rawPayload),
			"opt_prompt": string(optPayload),
			"opt_resp": string(optimizedResponse),
			"orig_resp": string(unoptResp),
			"opt_ms": optDuration.Milliseconds(),
			"orig_ms": unoptDuration.Milliseconds(),
			"score": score,
			"feedback": feedback,
			"opt_prompt_tokens": promptOpt,
			"opt_completion_tokens": completionOpt,
		},
	})
}
