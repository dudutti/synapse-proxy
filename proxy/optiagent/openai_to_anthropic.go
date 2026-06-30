// Package optiagent — OpenAI → Anthropic payload translator.
//
// Same content as the SaaS proxy (proxy/optiagent/openai_to_anthropic.go).
// Used by the local-client and any future minideployments that
// route to an Anthropic-compatible provider (Minimax, DeepSeek,
// Bedrock, Vertex).
package optiagent

import (
	"encoding/json"
	"fmt"
)

type chatCompletionReq struct {
	Model       string                   `json:"model"`
	Messages    []map[string]interface{} `json:"messages"`
	Stream      bool                     `json:"stream,omitempty"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
	Temperature *float64                 `json:"temperature,omitempty"`
	TopP        *float64                 `json:"top_p,omitempty"`
	Stop        interface{}              `json:"stop,omitempty"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
}

type anthropicReq struct {
	Model         string                   `json:"model"`
	System        string                   `json:"system,omitempty"`
	Messages      []map[string]interface{} `json:"messages"`
	MaxTokens     int                      `json:"max_tokens"`
	Temperature   *float64                 `json:"temperature,omitempty"`
	TopP          *float64                 `json:"top_p,omitempty"`
	StopSequences []string                 `json:"stop_sequences,omitempty"`
	Tools         []map[string]interface{} `json:"tools,omitempty"`
}

func OpenAIToAnthropic(payload []byte, modelRemap string) ([]byte, error) {
	var src chatCompletionReq
	if err := json.Unmarshal(payload, &src); err != nil {
		return nil, fmt.Errorf("OpenAIToAnthropic: not valid JSON: %w", err)
	}
	if len(src.Messages) == 0 {
		return nil, fmt.Errorf("OpenAIToAnthropic: no messages")
	}
	out := anthropicReq{
		Messages:    []map[string]interface{}{},
		MaxTokens:   src.MaxTokens,
		Temperature: src.Temperature,
		TopP:        src.TopP,
	}
	if modelRemap != "" {
		out.Model = modelRemap
	} else {
		out.Model = src.Model
	}
	switch v := src.Stop.(type) {
	case string:
		if v != "" {
			out.StopSequences = []string{v}
		}
	case []interface{}:
		for _, s := range v {
			if str, ok := s.(string); ok {
				out.StopSequences = append(out.StopSequences, str)
			}
		}
	}
	if len(src.Tools) > 0 {
		out.Tools = anthropicTools(src.Tools)
	}
	var systemBuf []byte
	for _, msg := range src.Messages {
		role, _ := msg["role"].(string)
		content := msg["content"]
		switch role {
		case "system":
			switch c := content.(type) {
			case string:
				if c != "" {
					if len(systemBuf) > 0 {
						systemBuf = append(systemBuf, '\n', '\n')
					}
					systemBuf = append(systemBuf, c...)
				}
			case []interface{}:
				if len(c) > 0 {
					if b, ok := contentBlocksToAnthropic(c); ok {
						if len(systemBuf) > 0 {
							systemBuf = append(systemBuf, '\n', '\n')
						}
						systemBuf = append(systemBuf, b...)
					}
				}
			}
		case "user", "assistant":
			translated, ok := translateAnthropicMessage(role, content, msg)
			if !ok {
				continue
			}
			out.Messages = append(out.Messages, translated)
		case "tool", "function":
			translated, ok := translateAnthropicToolMessage(msg)
			if !ok {
				continue
			}
			out.Messages = append(out.Messages, translated)
		}
	}
	if len(systemBuf) > 0 {
		out.System = string(systemBuf)
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = 4096
	}
	return json.Marshal(out)
}

func translateAnthropicMessage(role string, content interface{}, msg map[string]interface{}) (map[string]interface{}, bool) {
	out := map[string]interface{}{"role": role}
	switch c := content.(type) {
	case string:
		if c == "" && role == "assistant" {
			out["content"] = []map[string]interface{}{{"type": "text", "text": " "}}
		} else {
			out["content"] = []map[string]interface{}{{"type": "text", "text": c}}
		}
	case []interface{}:
		if len(c) == 0 {
			return nil, false
		}
		blocks := []map[string]interface{}{}
		for _, blockIntf := range c {
			block, ok := blockIntf.(map[string]interface{})
			if !ok {
				continue
			}
			translated, ok := translateContentBlock(block)
			if ok {
				blocks = append(blocks, translated)
			}
		}
		if len(blocks) == 0 {
			if role == "assistant" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": " "})
			} else {
				return nil, false
			}
		}
		out["content"] = blocks
	case nil:
		out["content"] = []map[string]interface{}{{"type": "text", "text": " "}}
	default:
		return nil, false
	}
	if role == "assistant" {
		if tc, ok := msg["tool_calls"].([]interface{}); ok && len(tc) > 0 {
			blocks, _ := out["content"].([]map[string]interface{})
			for _, callIntf := range tc {
				call, ok := callIntf.(map[string]interface{})
				if !ok {
					continue
				}
				fn, _ := call["function"].(map[string]interface{})
				argsStr, _ := fn["arguments"].(string)
				idStr, _ := call["id"].(string)
				nameStr, _ := fn["name"].(string)
				blocks = append(blocks, map[string]interface{}{
					"type":  "tool_use",
					"id":    idStr,
					"name":  nameStr,
					"input": json.RawMessage(argsStr),
				})
			}
			if len(blocks) > 0 {
				out["content"] = blocks
			}
		}
	}
	return out, true
}

func translateAnthropicToolMessage(msg map[string]interface{}) (map[string]interface{}, bool) {
	id, _ := msg["tool_call_id"].(string)
	if id == "" {
		return nil, false
	}
	content := msg["content"]
	var data string
	switch c := content.(type) {
	case string:
		data = c
	case []interface{}:
		for _, b := range c {
			if bm, ok := b.(map[string]interface{}); ok {
				if bm["type"] == "text" {
					if t, ok := bm["text"].(string); ok {
						data += t
					}
				}
			}
		}
	default:
		b, _ := json.Marshal(c)
		data = string(b)
	}
	return map[string]interface{}{
		"role": "user",
		"content": []map[string]interface{}{{
			"type":        "tool_result",
			"tool_use_id": id,
			"content":     data,
		}},
	}, true
}

func translateContentBlock(b map[string]interface{}) (map[string]interface{}, bool) {
	t, _ := b["type"].(string)
	switch t {
	case "", "text":
		text, _ := b["text"].(string)
		return map[string]interface{}{"type": "text", "text": text}, true
	case "image_url":
		u, _ := b["image_url"].(map[string]interface{})
		url, _ := u["url"].(string)
		if url == "" {
			return nil, false
		}
		return map[string]interface{}{
			"type":   "image",
			"source": map[string]interface{}{"type": "url", "url": url},
		}, true
	default:
		return nil, false
	}
}

func contentBlocksToAnthropic(blocks []interface{}) ([]byte, bool) {
	out := []map[string]interface{}{}
	for _, blockIntf := range blocks {
		block, ok := blockIntf.(map[string]interface{})
		if !ok {
			continue
		}
		translated, ok := translateContentBlock(block)
		if ok {
			out = append(out, translated)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

func anthropicTools(src []map[string]interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, t := range src {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]interface{})
		out = append(out, map[string]interface{}{
			"name":         name,
			"description":  desc,
			"input_schema": params,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}