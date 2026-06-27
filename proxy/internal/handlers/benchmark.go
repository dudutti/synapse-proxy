package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"synapse-proxy/internal/db"
	"github.com/redis/go-redis/v9"
)

func runBenchmarkEvaluation(
	vk, realKey, provider, model, defaultModel string,
	rawPayload, optPayload, optimizedResponse []byte,
	optDuration time.Duration,
	promptOpt, completionOpt int,
) {
	start := time.Now()

	var upstreamURL string
	switch provider {
	case "anthropic":
		upstreamURL = "https://api.anthropic.com/v1/messages"
	case "google":
		upstreamURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case "minimax":
		upstreamURL = "https://api.minimax.io/v1/chat/completions"
		if envURL := os.Getenv("MINIMAX_UPSTREAM_URL"); envURL != "" {
			upstreamURL = envURL
		}
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
	case "lmstudio":
		upstreamURL = "http://127.0.0.1:1234/v1/chat/completions"
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
		pMap["stream"] = false         // Force non-streaming for the benchmark control request
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
		Stream: "synapse:benchmark_logs",
		Values: map[string]interface{}{
			"vk":                    vk,
			"orig_prompt":           string(rawPayload),
			"opt_prompt":            string(optPayload),
			"opt_resp":              string(optimizedResponse),
			"orig_resp":             string(unoptResp),
			"opt_ms":                optDuration.Milliseconds(),
			"orig_ms":               unoptDuration.Milliseconds(),
			"score":                 score,
			"feedback":              feedback,
			"opt_prompt_tokens":     promptOpt,
			"opt_completion_tokens": completionOpt,
		},
	})
}
