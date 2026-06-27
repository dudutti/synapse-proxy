package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// appError describes an upstream application-level error that the proxy
// surfaces to the client as a real HTTP status (instead of forwarding a
// 200 OK with a poison body that causes the agent to hang).
type appError struct {
	statusCode int    // upstream's reported code (e.g. 2056 for MiniMax quota)
	message    string // human-readable message
	quota      bool   // true for quota/credit/payment-required errors
}

// detectUpstreamAppError parses an upstream response body and returns a
// non-nil *appError if the upstream returned an application-level error
// (despite the HTTP 200 status). Supports:
//   - MiniMax: { "base_resp": { "status_code": N, "status_msg": "..." } }
//   - OpenAI-style: { "error": { "message": "...", "type": "...", "code": ... } }
//
// nil means "no error detected, keep streaming".
func detectUpstreamAppError(body []byte) *appError {
	if len(body) == 0 {
		return nil
	}
	// Extract the JSON part from an SSE "data: {...}" line.
	jsonBody := body
	if bytes.HasPrefix(body, []byte("data: ")) {
		jsonBody = body[len("data: "):]
		// SSE may include a trailing "\n\n" after the JSON.
		if idx := bytes.IndexByte(jsonBody, '\n'); idx > 0 {
			jsonBody = jsonBody[:idx]
		}
	}
	// Skip the SSE "data: [DONE]" sentinel.
	if bytes.HasPrefix(jsonBody, []byte("[DONE]")) {
		return nil
	}
	if !bytes.HasPrefix(jsonBody, []byte("{")) {
		return nil
	}

	// First, try a generic structure that can hold either base_resp or error.
	var generic struct {
		BaseResp *struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		// Some upstreams (e.g. Anthropic) put code at the top level.
		TopCode any `json:"code"`
	}
	if err := json.Unmarshal(jsonBody, &generic); err != nil {
		return nil
	}

	// MiniMax-style: base_resp.status_code != 0 means error.
	if generic.BaseResp != nil && generic.BaseResp.StatusCode != 0 {
		msg := generic.BaseResp.StatusMsg
		if msg == "" {
			msg = fmt.Sprintf("upstream returned status_code %d", generic.BaseResp.StatusCode)
		}
		return &appError{
			statusCode: generic.BaseResp.StatusCode,
			message:    msg,
			quota:      isQuotaError(generic.BaseResp.StatusCode, msg),
		}
	}

	// OpenAI-style: { "error": { ... } }
	if generic.Error != nil && generic.Error.Message != "" {
		msg := generic.Error.Message
		return &appError{
			statusCode: 0,
			message:    msg,
			quota:      isQuotaError(0, msg),
		}
	}

	return nil
}

// isQuotaError returns true if the upstream error looks like a quota/credit
// problem (so the proxy can return HTTP 402 Payment Required to the client).
func isQuotaError(code int, msg string) bool {
	m := strings.ToLower(msg)
	keywords := []string{
		"quota", "credit", "usage limit", "rate limit", "billing", "plan",
		"insufficient", "payment", "exhausted",
	}
	for _, k := range keywords {
		if strings.Contains(m, k) {
			return true
		}
	}
	// MiniMax returns code 2056 for quota and 1002/1003/1004 for billing.
	if code == 2056 || code == 1002 || code == 1003 || code == 1004 {
		return true
	}
	return false
}

// maskVirtualKey returns a short, non-secret prefix of the virtual key
// for safe inclusion in panic / error logs. Format: first 8 chars + "…"
// (e.g. "sk-opti…"). Returns "<empty>" for empty input.
func maskVirtualKey(authHeader string) string {
	vk := strings.TrimPrefix(authHeader, "Bearer ")
	vk = strings.TrimSpace(vk)
	if vk == "" {
		return "<empty>"
	}
	if len(vk) <= 8 {
		return vk[:min(len(vk), 4)] + "…"
	}
	return vk[:8] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func extractModelFromResponse(respBytes []byte, fallback string) string {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(respBytes, &body); err == nil && body.Model != "" {
		return body.Model
	}
	return fallback
}

func pickModel(discovered, fallback string) string {
	if discovered != "" {
		return discovered
	}
	return fallback
}

// makeSelfCorrectionResponse constructs a mock Chat Completions response containing the self-correction hint.
func makeSelfCorrectionResponse(toolName string, model string) []byte {
	var msgContent string
	if toolName != "" {
		msgContent = "Attention : Vous venez de répéter l'outil " + toolName + " avec les mêmes arguments. Veuillez vérifier vos actions précédentes ou changer de stratégie pour éviter une boucle infinie."
	} else {
		msgContent = "Attention : Une boucle répétitive a été détectée dans vos requêtes. Veuillez vérifier vos actions précédentes ou changer de stratégie pour éviter une boucle infinie."
	}

	respObj := map[string]interface{}{
		"id":      "chatcmpl-selfcorrect",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": msgContent,
				},
				"finish_reason": "stop",
			},
		},
	}
	respBytes, _ := json.Marshal(respObj)
	return respBytes
}

// injectSystemWarning appends a system warning to the last message in the payload
// to nudge the LLM out of a tool loop without stopping the agent framework.
func injectSystemWarning(payload []byte, toolName string) []byte {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}
	messagesRaw, ok := body["messages"].([]interface{})
	if !ok || len(messagesRaw) == 0 {
		return payload
	}

	lastMsgRaw := messagesRaw[len(messagesRaw)-1]
	lastMsg, ok := lastMsgRaw.(map[string]interface{})
	if !ok {
		return payload
	}

	warningText := fmt.Sprintf("\n\n[SYSTEM WARNING: The proxy intercepted your request because you are caught in a loop. You have repeated the tool '%s' with identical arguments too many times. You MUST change your strategy immediately. Do not repeat the same action.]", toolName)

	// Try to append to string content
	if contentStr, ok := lastMsg["content"].(string); ok {
		lastMsg["content"] = contentStr + warningText
	} else if contentArr, ok := lastMsg["content"].([]interface{}); ok {
		// It's an array of content blocks (OpenAI vision or Anthropic style)
		contentArr = append(contentArr, map[string]interface{}{
			"type": "text",
			"text": warningText,
		})
		lastMsg["content"] = contentArr
	} else {
		// Fallback: append a user message
		warningMsg := map[string]interface{}{
			"role": "user",
			"content": warningText,
		}
		body["messages"] = append(messagesRaw, warningMsg)
	}

	newPayload, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return newPayload
}
