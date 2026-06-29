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
	Created int64  `json:"created"`
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

	var chatReq ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &chatReq); err != nil {
		http.Error(w, "invalid request json", http.StatusBadRequest)
		return
	}

	// Extract prompt text
	promptText := ""
	for _, msg := range chatReq.Messages {
		if content, ok := msg["content"].(string); ok {
			promptText += content + "\n"
		}
	}

	provider := strings.ToLower(r.Header.Get("X-Synapse-Provider"))
	if provider == "" {
		provider = "openai" // Default fallback
	}

	// 1. Check Caches
	cacheHit := false
	cacheLevel := "NONE"
	cachedResponse := ""

	if cached, ok := cache.GetL1(promptText); ok {
		cacheHit = true
		cacheLevel = "L1"
		cachedResponse = cached
	} else if cached, ok := cache.GetL2(promptText); ok {
		cacheHit = true
		cacheLevel = "L2"
		cachedResponse = cached
	} else if cached, ok := cache.GetL3(promptText); ok {
		cacheHit = true
		cacheLevel = "L3"
		cachedResponse = cached
	}

	if cacheHit {
		w.Header().Set("X-Synapse-Cache", "HIT")
		w.Header().Set("X-Synapse-Level", cacheLevel)
		
		// Build standard OpenAI chat response
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
		
		// Estimate tokens
		pTokens := countTokens(promptText)
		cTokens := countTokens(cachedResponse)
		resp.Usage.PromptTokens = pTokens
		resp.Usage.CompletionTokens = cTokens
		resp.Usage.TotalTokens = pTokens + cTokens

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)

		// Record the saved log
		costSaved := float64(pTokens) * 0.000015 // approximated
		recordLog(cacheLevel, chatReq.Model, provider, pTokens, cTokens, 0, cTokens, 10, costSaved)
		return
	}

	// 2. Cache Miss - Forward to Target Upstream LLM Server
	upstreamURL := ""
	if provider == "ollama" {
		upstreamURL = "http://localhost:11434/v1/chat/completions"
	} else if provider == "lmstudio" {
		upstreamURL = "http://localhost:1234/v1/chat/completions"
	} else {
		// Public Cloud LLM target URL fallbacks
		upstreamURL = "https://api.openai.com/v1/chat/completions"
		if provider == "anthropic" {
			upstreamURL = "https://api.anthropic.com/v1/messages"
		}
	}

	// Dynamic token compression simulation (if license allows)
	compressedText := promptText
	origTokens := countTokens(promptText)
	optTokens := origTokens

	if license.CheckQuota() {
		// Compact spaces/tabs, remove comments (simulation of compressors)
		compressedText = compactPrompt(promptText)
		optTokens = countTokens(compressedText)
	}

	// Rebuild request if compression changed prompt length
	if optTokens < origTokens {
		// Update request messages content
		for _, msg := range chatReq.Messages {
			if _, ok := msg["content"].(string); ok {
				msg["content"] = compactPrompt(msg["content"].(string))
			}
		}
		bodyBytes, _ = json.Marshal(chatReq)
	}

	t0 := time.Now()
	req, _ := http.NewRequest("POST", upstreamURL, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Forward authorization header if it exists
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	client := &http.Client{}
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

	// Save L1 Cache for next hits
	if resp.StatusCode == http.StatusOK && compText != "" {
		cache.SetL1(promptText, compText)
	}

	// Record token count and logs
	savedTokens := int64(origTokens - optTokens)
	if savedTokens > 0 {
		license.IncrementQuota(savedTokens)
	}

	costSaved := float64(savedTokens) * 0.000015
	recordLog("NONE", chatReq.Model, provider, origTokens, chatResp.Usage.CompletionTokens, optTokens, chatResp.Usage.CompletionTokens, duration, costSaved)
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

func compactPrompt(text string) string {
	// Simple local compressor (collapse multiple whitespaces, strip HTML-like tags)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func countTokens(text string) int {
	words := len(strings.Fields(text))
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
	TotalTokensSent          map[string]int64  `json:"totalTokensSent"`
	TotalTokensOptimized     map[string]int64  `json:"totalTokensOptimized"`
	CacheHitDistribution    map[string]int64  `json:"cacheHitDistribution"`
	CacheByProvider          map[string]interface{} `json:"cacheByProvider"`
	CacheHitRateByProvider   map[string]float64 `json:"cacheHitRateByProvider"`
	MeasuredSavings          map[string]int64  `json:"measuredSavings"`
	OpportunitySavings       map[string]interface{} `json:"opportunitySavings"`
	TotalSavingsByClass      map[string]float64 `json:"totalSavingsByClass"`
	SavingsByClassByProvider map[string]interface{} `json:"savingsByClassByProvider"`
	TotalSavingsReal         float64           `json:"totalSavingsReal"`
	ChartData                []interface{}     `json:"chartData"`
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

	// Establish connection
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
