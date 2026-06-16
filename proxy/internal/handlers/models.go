package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"optitoken/internal/db"
	"optitoken/internal/workers"
)

type FetchModelsRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
}

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FetchModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// FetchModelsHandler retrieves available models from the specified provider
func FetchModelsHandler(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for dashboard access
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqData FetchModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if reqData.Provider == "" || reqData.APIKey == "" {
		http.Error(w, "Provider and api_key are required", http.StatusBadRequest)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var models []ModelInfo
	var fetchErr error

	switch reqData.Provider {
	case "openai":
		models, fetchErr = fetchOpenAIModels(client, reqData.APIKey)
	case "anthropic":
		models, fetchErr = fetchAnthropicModels(client, reqData.APIKey)
	case "deepseek":
		models, fetchErr = fetchDeepseekModels(client, reqData.APIKey)
	case "mistral":
		models, fetchErr = fetchMistralModels(client, reqData.APIKey)
	case "google":
		models, fetchErr = fetchGoogleModels(client, reqData.APIKey)
	case "minimax":
		models, fetchErr = fetchMinimaxModels(client, reqData.APIKey)
	case "openrouter":
		models, fetchErr = fetchOpenRouterModels(client, reqData.APIKey)
	case "moonshot":
		models, fetchErr = fetchMoonshotModels(client, reqData.APIKey)
	default:
		// Unknown provider or not yet implemented
		http.Error(w, fmt.Sprintf("Provider %s is not supported for dynamic model fetching yet", reqData.Provider), http.StatusBadRequest)
		return
	}

	if fetchErr != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch models: %v", fetchErr), http.StatusBadGateway)
		return
	}

	// Sort models alphabetically
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	// Seed the Model Radar with the models we just confirmed exist.
	// Fire-and-forget: the dashboard response is not blocked on this.
	if rdb := db.GetRedis(); rdb != nil {
		ids := make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
		go workers.RegisterKnownModels(context.Background(), rdb, reqData.Provider, ids)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(FetchModelsResponse{Models: models})
}

func fetchOpenAIModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}

func fetchAnthropicModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.anthropic.com/v1/models", nil)
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		models = append(models, ModelInfo{ID: m.ID, Name: name})
	}
	return models, nil
}

func fetchDeepseekModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.deepseek.com/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}

func fetchMistralModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.mistral.ai/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}

func fetchGoogleModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	// Gemini uses API key in URL query parameter
	url := "https://generativelanguage.googleapis.com/v1beta/models?key=" + apiKey
	req, _ := http.NewRequest("GET", url, nil)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Models {
		// Google models often come prefixed with "models/", we can strip it or keep it depending on usage.
		// Standard usage in Google API typically includes the prefix "models/gemini-1.5-pro-latest" or just "gemini-1.5-pro-latest"
		// We will provide the name as it might be used.
		id := m.Name
		name := m.DisplayName
		if name == "" {
			name = id
		}
		// Strip "models/" prefix if present for cleaner ID if needed, but it's often safer to pass exactly what's required
		// For the standard OpenAI compat endpoint they use, it might just need "gemini-1.5-pro"
		idClean := id
		if len(idClean) > 7 && idClean[:7] == "models/" {
			idClean = idClean[7:]
		}
		
		models = append(models, ModelInfo{ID: idClean, Name: name})
	}
	return models, nil
}

func fetchMinimaxModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.minimax.io/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}

func fetchOpenRouterModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, ModelInfo{ID: m.ID, Name: name})
	}
	return models, nil
}

func fetchMoonshotModels(client *http.Client, apiKey string) ([]ModelInfo, error) {
	req, _ := http.NewRequest("GET", "https://api.moonshot.cn/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}
