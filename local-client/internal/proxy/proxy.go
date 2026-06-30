package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"synapse-local/internal/cache"
	"synapse-local/internal/compress"
	"synapse-local/internal/db"
	"synapse-local/internal/license"
)

var LogBroadcastChan = make(chan map[string]interface{}, 100)

type ChatCompletionRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
}

type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"model"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int `json:"index"`
		Message      map[string]interface{} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type OllamaTagResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type LMStudioModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// StartProxyServer starts the local proxy listener
func StartProxyServer(proxyPort string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", HandleChatCompletions)
	mux.HandleFunc("/v1/models", HandleV1Models)
	mux.HandleFunc("/api/models", HandleListModels)

	go func() {
		_ = http.ListenAndServe(":"+proxyPort, mux)
	}()
}

// HandleListModels queries Ollama/LM Studio model tags and lists them
func HandleListModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	provider := strings.ToLower(r.URL.Query().Get("provider"))
	url := r.URL.Query().Get("url")

	var list []string

	if provider == "ollama" {
		if url == "" {
			url = "http://localhost:11434"
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(url + "/api/tags")
		if err == nil && resp.StatusCode == http.StatusOK {
			var ollamaResp OllamaTagResponse
			if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err == nil {
				for _, m := range ollamaResp.Models {
					list = append(list, m.Name)
				}
			}
			resp.Body.Close()
		}
	} else if provider == "lmstudio" {
		if url == "" {
			url = "http://localhost:1234"
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(url + "/v1/models")
		if err == nil && resp.StatusCode == http.StatusOK {
			var lmResp LMStudioModelsResponse
			if err := json.NewDecoder(resp.Body).Decode(&lmResp); err == nil {
				for _, d := range lmResp.Data {
					list = append(list, d.ID)
				}
			}
			resp.Body.Close()
		}
	}

	if list == nil {
		list = []string{}
	}

	_ = json.NewEncoder(w).Encode(list)
}

// HandleV1Models handles the standard GET /v1/models endpoint for OpenAI clients
func HandleV1Models(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	authHeader := r.Header.Get("Authorization")
	// If it is a virtual key, forward to the SaaS endpoint
	if authHeader != "" && (strings.HasPrefix(authHeader, "Bearer sk-opti-") || strings.HasPrefix(authHeader, "Bearer sk-opt")) {
		client := &http.Client{Timeout: 6 * time.Second}
		req, err := http.NewRequest("GET", "https://synapse-proxy.com/v1/models", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", authHeader)

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "SaaS server unreachable: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	// Otherwise, return local models from Ollama and LM Studio
	type OpenAIModel struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	type OpenAIModelsResponse struct {
		Object string        `json:"object"`
		Data   []OpenAIModel `json:"data"`
	}

	var list []OpenAIModel
	now := time.Now().Unix()

	// 1. Try Ollama
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err == nil && resp.StatusCode == http.StatusOK {
		var ollamaResp OllamaTagResponse
		if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err == nil {
			for _, m := range ollamaResp.Models {
				list = append(list, OpenAIModel{
					ID:      m.Name,
					Object:  "model",
					Created: now,
					OwnedBy: "ollama",
				})
			}
		}
		resp.Body.Close()
	}

	// 2. Try LM Studio
	resp, err = client.Get("http://localhost:1234/v1/models")
	if err == nil && resp.StatusCode == http.StatusOK {
		var lmResp LMStudioModelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&lmResp); err == nil {
			for _, d := range lmResp.Data {
				list = append(list, OpenAIModel{
					ID:      d.ID,
					Object:  "model",
					Created: now,
					OwnedBy: "lmstudio",
				})
			}
		}
		resp.Body.Close()
	}

	if list == nil {
		list = []OpenAIModel{}
	}

	_ = json.NewEncoder(w).Encode(OpenAIModelsResponse{
		Object: "list",
		Data:   list,
	})
}

func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	provider := strings.ToLower(r.Header.Get("X-Synapse-Provider"))
	if provider == "" {
		provider = "openai" // Default fallback
	}

	// Determine the default model the upstream expects
	// (e.g. "MiniMax-M3" for Minimax's Anthropic endpoint).
	defaultModel := chatReqDefaultModel(bodyBytes)
	// Apply the full BeforeRequest pipeline: byte-preserving L3
	// compression + (optional) OpenAI→Anthropic translation.
	bodyBytes, _ = compress.RunBefore(bodyBytes, provider, defaultModel)

	var chatReq ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &chatReq); err != nil {
		http.Error(w, "invalid request json", http.StatusBadRequest)
		return
	}

	// Build a payload signature for cache lookup. We use the
	// post-L3 payload so the cache key matches what's actually
	// forwarded upstream.
	pc, pcErr := cache.MakePayloadContext(bodyBytes)
	if pcErr != nil {
		// Malformed payload — fall through to forward and let
		// the upstream reject it.
		pc = cache.PayloadContext{Hash: cache.ComputePayloadHash(bodyBytes)}
	}

	// 1. Check Caches
	cacheHit := false
	cacheLevel := "NONE"
	cachedResponse := ""

	if cached, ok := cache.GetL1(pc); ok {
		cacheHit = true
		cacheLevel = "L1"
		cachedResponse = cached
	} else if cached, ok := cache.GetL2(pc); ok {
		cacheHit = true
		cacheLevel = "L2"
		cachedResponse = cached
	} else if cached, ok := cache.GetL3(pc); ok {
		cacheHit = true
		cacheLevel = "L3"
		cachedResponse = cached
	}

	if cacheHit {
		w.Header().Set("X-Synapse-Cache", "HIT")
		w.Header().Set("X-Synapse-Level", cacheLevel)

		resp := ChatCompletionResponse{
			ID:      "chatcmpl-" + generateID(),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   chatReq.Model,
		}
		choice := struct {
			Index        int `json:"index"`
			Message      map[string]interface{} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			Index: 0,
			Message: map[string]interface{}{
				"role":    "assistant",
				"content": cachedResponse,
			},
			FinishReason: "stop",
		}
		resp.Choices = append(resp.Choices, choice)

		pTokens := pc.NumMessages * 1000 // rough estimate
		cTokens := countTokens(cachedResponse)
		resp.Usage.PromptTokens = pTokens
		resp.Usage.CompletionTokens = cTokens
		resp.Usage.TotalTokens = pTokens + cTokens

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)

		costSaved := float64(pTokens) * 0.000015
		recordLog(cacheLevel, chatReq.Model, provider, pTokens, cTokens, pTokens, cTokens, 10, costSaved)
		return
	}

	// 2. Cache Miss — forward to upstream. The pipeline may have
	// already translated the payload to Anthropic shape, so we
	// pick the right URL and headers.
	upstreamURL := ""
	upstreamHeaders := http.Header{}
	upstreamHeaders.Set("Content-Type", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		upstreamHeaders.Set("Authorization", auth)
	}

	anthropicCompat := compress.ProviderUsesAnthropicShape(provider)

	switch provider {
	case "ollama":
		upstreamURL = "http://localhost:11434/v1/chat/completions"
	case "lmstudio":
		upstreamURL = "http://localhost:1234/v1/chat/completions"
	case "minimax", "minimax-anthropic":
		// Minimax exposes both endpoints. The Anthropic-shape
		// path is the only one with provider-side prompt cache.
		if anthropicCompat {
			upstreamURL = "https://api.minimax.io/anthropic/v1/messages"
			upstreamHeaders.Del("Authorization")
			if auth := r.Header.Get("Authorization"); auth != "" {
				upstreamHeaders.Set("x-api-key", strings.TrimPrefix(auth, "Bearer "))
				upstreamHeaders.Set("anthropic-version", "2023-06-01")
			}
		} else {
			upstreamURL = "https://api.minimax.io/v1/chat/completions"
		}
	case "deepseek":
		if anthropicCompat {
			upstreamURL = "https://api.deepseek.com/anthropic/v1/messages"
			upstreamHeaders.Del("Authorization")
			if auth := r.Header.Get("Authorization"); auth != "" {
				upstreamHeaders.Set("x-api-key", strings.TrimPrefix(auth, "Bearer "))
				upstreamHeaders.Set("anthropic-version", "2023-06-01")
			}
		} else {
			upstreamURL = "https://api.deepseek.com/chat/completions"
		}
	case "anthropic", "claude":
		upstreamURL = "https://api.anthropic.com/v1/messages"
		upstreamHeaders.Del("Authorization")
		if auth := r.Header.Get("Authorization"); auth != "" {
			upstreamHeaders.Set("x-api-key", strings.TrimPrefix(auth, "Bearer "))
			upstreamHeaders.Set("anthropic-version", "2023-06-01")
		}
	default:
		upstreamURL = "https://api.openai.com/v1/chat/completions"
	}

	originalOptTokens := len(bodyBytes) / 4 // rough tokens estimate (4 bytes/token)

	t0 := time.Now()
	req, _ := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(bodyBytes))
	for k, vv := range upstreamHeaders {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 600 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "upstream connection error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("X-Synapse-Cache", "MISS")
	w.Header().Set("X-Synapse-Level", "NONE")
	w.WriteHeader(resp.StatusCode)

	respBodyBytes, _ := io.ReadAll(resp.Body)
	// Translate Anthropic response back to OpenAI shape so the
	// caller doesn't have to know.
	respBodyBytes = compress.RunAfter(respBodyBytes, provider, defaultModel)
	_, _ = w.Write(respBodyBytes)

	duration := time.Since(t0).Milliseconds()

	// Parse upstream usage
	var chatResp ChatCompletionResponse
	_ = json.Unmarshal(respBodyBytes, &chatResp)

	compText := ""
	if len(chatResp.Choices) > 0 {
		if content, ok := chatResp.Choices[0].Message["content"].(string); ok {
			compText = content
		}
	}

	if resp.StatusCode == http.StatusOK && compText != "" {
		cache.SetL1(pc, compText)
	}

	costSaved := float64(originalOptTokens) * 0.000015 * 0.5
	recordLog("NONE", chatReq.Model, provider,
		originalOptTokens, chatResp.Usage.CompletionTokens,
		originalOptTokens, chatResp.Usage.CompletionTokens,
		duration, costSaved)
}

// chatReqDefaultModel extracts the model field from an
// OpenAI-shape request body. Returns "" if the body is not
// valid JSON or has no model field.
func chatReqDefaultModel(body []byte) string {
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Model
}

func getCacheType(lvl string) string {
	switch lvl {
	case "L1":
		return "Cache Hit (L1)"
	case "L2":
		return "L2 Cache (semantic)"
	case "L3":
		return "Cache Hit (L3)"
	default:
		return "Cache Miss"
	}
}

func recordLog(level, model, provider string, tokensIn, tokensOut, tokensInOpt, tokensOutOpt int, duration int64, cost float64) {
	id := generateID()
	_, _ = db.DB.Exec(`
		INSERT INTO request_logs (id, cache_level, model, provider, tokens_in, tokens_out, tokens_in_opt, tokens_out_opt, duration_ms, cost_saved, agent_id, agent_label, session_id, api_key_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '', 'local-virtual-key')
	`, id, level, model, provider, tokensIn, tokensOut, tokensInOpt, tokensOutOpt, duration, cost)

	// Broadcast to active SSE listeners
	select {
	case LogBroadcastChan <- map[string]interface{}{
		"id":             id,
		"type":           getCacheType(level),
		"cacheLevel":     level,
		"model":          model,
		"provider":       provider,
		"tokensIn":       tokensIn,
		"tokensOut":      tokensOut,
		"tokensInOpt":    tokensInOpt,
		"tokensOutOpt":   tokensOutOpt,
		"durationMs":     duration,
		"costSaved":      cost,
		"costWithout":    float64(tokensIn)*0.000015 + float64(tokensOut)*0.000015,
		"costWith":       float64(tokensInOpt)*0.000015 + float64(tokensOutOpt)*0.000015,
		"createdAt":      time.Now().Format("2006-01-02 15:04:05"),
		"originalPrompt": "Prompt cached locally",
	}:
	default:
		// Drop if buffer is full / no readers
	}
}

// compactPrompt kept for backwards compat with the older
// collapse-only path used by the cached-response fallback when
// the L3 compressor refused to parse the payload.
func compactPrompt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func countTokens(text string) int {
	words := len(strings.Fields(text))
	if words == 0 {
		return 0
	}
	return int(math.Ceil(float64(words) * 1.3))
}

func generateID() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// HandleKeysRoute manages keys creation, deletion, and listing locally
func HandleKeysRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodGet {
		rows, err := db.DB.Query("SELECT id, virtual_key, provider, real_key, default_model FROM virtual_keys ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []map[string]interface{}
		for rows.Next() {
			var id, vk, prov, realKey, defModel string
			if err := rows.Scan(&id, &vk, &prov, &realKey, &defModel); err == nil {
				list = append(list, map[string]interface{}{
					"id":                 id,
					"virtualKey":         vk,
					"provider":           prov,
					"realKey":            realKey,
					"defaultModel":       defModel,
					"benchmarkMode":      false,
					"semanticTolerance":  0.15,
					"cacheTtl":           86400,
					"isolateCacheByUser": false,
				})
			}
		}
		if list == nil {
			list = []map[string]interface{}{}
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Provider     string `json:"provider"`
			RealKey      string `json:"realKey"`
			DefaultModel string `json:"defaultModel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		id := generateID()
		vk := "sk-opti-" + generateID()[:12]
		_, err := db.DB.Exec("INSERT INTO virtual_keys (id, virtual_key, provider, real_key, default_model) VALUES (?, ?, ?, ?, ?)", id, vk, req.Provider, req.RealKey, req.DefaultModel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         id,
			"virtualKey": vk,
		})
		return
	}

	if r.Method == http.MethodDelete {
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-1]
		if id == "" || id == "keys" {
			http.Error(w, "missing key id", http.StatusBadRequest)
			return
		}

		_, err := db.DB.Exec("DELETE FROM virtual_keys WHERE id = ?", id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		return
	}
}

// HandleUserRoute handles local user profile query
func HandleUserRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                 "local-user",
		"email":              "developer@synapse.local",
		"tier":               license.ActiveTier,
		"currentMonthTokens": license.QuotaUsed,
	})
}

// HandlePlansRoute returns empty list of plans
func HandlePlansRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode([]interface{}{})
}

// HandleSessionRoute returns a fake NextAuth session so client logic runs
func HandleSessionRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user": map[string]interface{}{
			"name":  "Local Developer",
			"email": "developer@synapse.local",
		},
		"expires": time.Now().AddDate(1, 0, 0).Format(time.RFC3339),
	})
}

// HandleAnalyticsRoute aggregates stats from SQLite request_logs
func HandleAnalyticsRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Query sums from sqlite request_logs
	var totalIn, totalOut, totalInOpt, totalOutOpt int64
	var costSaved float64
	var l1Hits, l2Hits, l3Hits, misses int64

	_ = db.DB.QueryRow(`
		SELECT
			COALESCE(SUM(tokens_in), 0),
			COALESCE(SUM(tokens_out), 0),
			COALESCE(SUM(tokens_in_opt), 0),
			COALESCE(SUM(tokens_out_opt), 0),
			COALESCE(SUM(cost_saved), 0)
		FROM request_logs
	`).Scan(&totalIn, &totalOut, &totalInOpt, &totalOutOpt, &costSaved)

	// Fetch cache levels count
	rows, err := db.DB.Query("SELECT cache_level, COUNT(*) FROM request_logs GROUP BY cache_level")
	if err == nil {
		for rows.Next() {
			var lvl string
			var count int64
			if err := rows.Scan(&lvl, &count); err == nil {
				switch lvl {
				case "L1":
					l1Hits = count
				case "L2":
					l2Hits = count
				case "L3":
					l3Hits = count
				default:
					misses = count
				}
			}
		}
		rows.Close()
	}

	// Fetch recent 100 logs
	logRows, err := db.DB.Query(`
		SELECT id, cache_level, model, provider, tokens_in, tokens_out, tokens_in_opt, tokens_out_opt, duration_ms, cost_saved, created_at
		FROM request_logs
		ORDER BY created_at DESC
		LIMIT 100
	`)
	var logs []map[string]interface{}
	if err == nil {
		defer logRows.Close()
		for logRows.Next() {
			var id, cacheLvl, model, prov, createdAt string
			var tIn, tOut, tInOpt, tOutOpt, duration int64
			var cSaved float64
			if err := logRows.Scan(&id, &cacheLvl, &model, &prov, &tIn, &tOut, &tInOpt, &tOutOpt, &duration, &cSaved, &createdAt); err == nil {
				logs = append(logs, map[string]interface{}{
					"id":             id,
					"type":           getCacheType(cacheLvl),
					"cacheLevel":     cacheLvl,
					"model":          model,
					"provider":       prov,
					"tokensIn":       tIn,
					"tokensOut":      tOut,
					"tokensInOpt":    tInOpt,
					"tokensOutOpt":   tOutOpt,
					"durationMs":     duration,
					"costSaved":      cSaved,
					"costWithout":    float64(tIn)*0.000015 + float64(tOut)*0.000015,
					"costWith":       float64(tInOpt)*0.000015 + float64(tOutOpt)*0.000015,
					"createdAt":      createdAt,
					"originalPrompt": "Prompt cached locally",
				})
			}
		}
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}

	res := AnalyticsResponse{
		TotalTokensSent: map[string]int64{
			"total":  totalIn + totalOut,
			"input":  totalIn,
			"output": totalOut,
		},
		TotalTokensOptimized: map[string]int64{
			"total":  totalInOpt + totalOutOpt,
			"input":  totalInOpt,
			"output": totalOutOpt,
		},
		CacheHitDistribution: map[string]int64{
			"MISS": misses,
			"L1":   l1Hits,
			"L2":   l2Hits,
			"L3":   l3Hits,
		},
		CacheByProvider:          map[string]interface{}{},
		CacheHitRateByProvider:   map[string]float64{},
		MeasuredSavings: map[string]int64{
			"l1L2Hits":        l1Hits + l2Hits,
			"l3Compressions": l3Hits,
		},
		OpportunitySavings: map[string]interface{}{
			"highCacheReadProviders": []string{},
		},
		TotalSavingsByClass: map[string]float64{
			"inputFresh":    0,
			"cacheRead":     costSaved,
			"cacheCreation": 0,
			"output":        0,
		},
		SavingsByClassByProvider: map[string]interface{}{},
		TotalSavingsReal:         costSaved,
		ChartData:                []interface{}{},
		Logs:                     logs,
	}

	_ = json.NewEncoder(w).Encode(res)
}

type AnalyticsResponse struct {
	TotalTokensSent          map[string]int64         `json:"totalTokensSent"`
	TotalTokensOptimized     map[string]int64         `json:"totalTokensOptimized"`
	CacheHitDistribution     map[string]int64         `json:"cacheHitDistribution"`
	CacheByProvider          map[string]interface{}    `json:"cacheByProvider"`
	CacheHitRateByProvider   map[string]float64       `json:"cacheHitRateByProvider"`
	MeasuredSavings          map[string]int64         `json:"measuredSavings"`
	OpportunitySavings       map[string]interface{}    `json:"opportunitySavings"`
	TotalSavingsByClass      map[string]float64       `json:"totalSavingsByClass"`
	SavingsByClassByProvider map[string]interface{}    `json:"savingsByClassByProvider"`
	TotalSavingsReal         float64                  `json:"totalSavingsReal"`
	ChartData                []interface{}            `json:"chartData"`
	Logs                     []map[string]interface{} `json:"logs"`
}

// HandleActivateLicenseRoute validates a license key with cloud and saves locally
func HandleActivateLicenseRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ok, err := license.ValidateLicense(req.Key)
	if err != nil || !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Invalid or expired license key"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"tier":    license.ActiveTier,
	})
}

// HandleAnalyticsStreamRoute serves real-time telemetry events via Server-Sent Events (SSE)
func HandleAnalyticsStreamRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "data: {\"connected\": true}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case logEvent := <-LogBroadcastChan:
			data, err := json.Marshal(logEvent)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", string(data))
				flusher.Flush()
			}
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}