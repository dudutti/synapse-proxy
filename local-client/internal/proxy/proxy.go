package proxy

import (
	"bytes"
	"encoding/json"
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
