package optiagent

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// todoListSignatures is the set of substrings that, when present in
// a tool result body, mark it as a todo / task list the agent
// iterates on. We must NOT truncate these (Hermes-style agents lose
// their plan visibility past turn 3 and start hallucinating tasks).
//
// Format-agnostic: the agent SDK can serialise a Todo as
//   [{"id":"1","content":"…","status":"in_progress"},{"id":"2",…}]
// or as a Markdown checkbox list, or as plain text with leading
// "- [ ]" / "- [x]". We match on a few canonical anchors that
// cover all three.
var todoListSignatures = []string{
	`"status":"pending"`,
	`"status":"in_progress"`,
	`"status":"completed"`,
	`"status":"todo"`,
	`"status":"done"`,
	`"todos":`,
	`"tasks":`,
	`"checklist":`,
	`"todoList":`,
	`- [ ]`,
	`- [x]`,
	`* [ ]`,
	`* [x]`,
}

// looksLikeTodoList returns true when the body looks like an
// agent's todo / task list that must be preserved verbatim. We
// intentionally err on the safe side: false positives are harmless
// (we just skip the truncation pass), false negatives corrupt the
// agent's plan and are catastrophic.
func looksLikeTodoList(content string) bool {
	if content == "" {
		return false
	}
	// Trim only the very edge whitespace; the signatures we look
	// for are usually in the first ~200 chars.
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	for _, sig := range todoListSignatures {
		if strings.Contains(head, sig) {
			return true
		}
	}
	return false
}

type ChatMessage struct {
	Role    string      `json:"role"`
	Content string      `json:"content"`
	Name    string      `json:"name,omitempty"`
}

type ChatPayload struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

var (
	// Hermes / Claude / General chain-of-thought blocks
	thoughtRegex = regexp.MustCompile(`(?s)<thought>.*?</thought>|<thinking>.*?</thinking>|<scratchpad>.*?</scratchpad>`)
	
	// OpenClaw / Agentic framework massive tool outputs (base64, huge JSONs)
	// We'll use a length threshold for older tool outputs
)

// CompressPayload applies agent-specific compression for frameworks like OpenClaw
func CompressPayload(payload []byte) ([]byte, error) {
	// Unmarshal into generic map to preserve all fields (tools, temperature, etc.)
	var genericPayload map[string]interface{}
	if err := json.Unmarshal(payload, &genericPayload); err != nil {
		return nil, err
	}

	messagesRaw, ok := genericPayload["messages"].([]interface{})
	if !ok {
		return payload, nil
	}

	var lastToolName string
	var consecutiveToolCount int
	msgCount := len(messagesRaw)

	for i, msgIntf := range messagesRaw {
		msg, ok := msgIntf.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		// Extract content string, handling both string and array-of-blocks formats
		var contentStr string
		var hasContent bool
		var isContentArray bool
		
		if c, ok := msg["content"].(string); ok {
			contentStr = c
			hasContent = true
			isContentArray = false
		} else if cArr, ok := msg["content"].([]interface{}); ok {
			isContentArray = true
			// Concatenate all text blocks to check length or regex
			for _, blockIntf := range cArr {
				if block, ok := blockIntf.(map[string]interface{}); ok {
					if t, ok := block["type"].(string); ok && t == "text" {
						if text, ok := block["text"].(string); ok {
							contentStr += text
							hasContent = true
						}
					}
				}
			}
		}

		name, _ := msg["name"].(string)
		// isRecentMessage: only the LAST assistant message is
		// preserved verbatim so the agent can see its own most
		// recent reasoning. Earlier thinking blocks and tool
		// outputs are stripped.
		isRecentMessage := i >= msgCount-1

		// 1. Hermes / Claude Chain-of-Thought Pruning
		if role == "assistant" && !isRecentMessage && hasContent {
			if isContentArray {
				cArr := msg["content"].([]interface{})
				for j, blockIntf := range cArr {
					if block, ok := blockIntf.(map[string]interface{}); ok {
						if t, ok := block["type"].(string); ok && t == "text" {
							if text, ok := block["text"].(string); ok {
								block["text"] = thoughtRegex.ReplaceAllString(text, "[Pruned Thought Process]")
								cArr[j] = block
							}
						}
					}
				}
				msg["content"] = cArr
			} else {
				msg["content"] = thoughtRegex.ReplaceAllString(contentStr, "[Pruned Thought Process]")
			}
		}

		// 1b. Strip reasoning_content from previous assistant turns.
		// Thinking models (DeepSeek-R1, MiniMax M3, Qwen QwQ, Gemma) return
		// reasoning as a sibling field of content. Clients re-send the whole
		// assistant turn as input context, which is pure waste â€” observed
		// at 89% of input tokens on agentic workloads. Only prune turns that
		// are not the most recent assistant message.
		if role == "assistant" && !isRecentMessage {
			if _, hasReasoning := msg["reasoning_content"]; hasReasoning {
				delete(msg, "reasoning_content")
			}
			if _, hasReasoning := msg["reasoning"]; hasReasoning {
				delete(msg, "reasoning")
			}
		}

		// 2. OpenClaw / Stale Tool Compaction
		// CRITICAL: do NOT replace tool output with a synthetic stub
		// message. The agent's safety filter reads the tool result and
		// treats anything that looks like a system message ("_opti_pruned",
		// "compacted_repeated_tool", "Respond as helpfully as possible",
		// "JAILBREAK", etc.) as a prompt-injection attempt, then refuses
		// to use the result. We must keep the *shape* of the original
		// content and only shrink it.
		if role == "tool" || role == "function" {
			// TODO-LIST / TASK-LIST carve-out. Hermes and similar
			// multi-turn agents store their plan in the result of
			// read_todos / write_todo / TodoWrite tool calls and re-read
			// it on every turn. Truncating those breaks plan visibility
			// past turn 3 and makes the agent hallucinate already-done
			// steps. We detect todo-list payloads by signature and skip
			// both the length truncation and the "drop repeated results"
			// pass below.
			isTodo := looksLikeTodoList(contentStr)

			if !isTodo && !isRecentMessage && hasContent && len(contentStr) > 200 {
				// Keep the first N chars + a trailing ellipsis.
				const keep = 200
				if len(contentStr) > keep+50 {
					if isContentArray {
						cArr := msg["content"].([]interface{})
						// Just truncate the first text block and drop the rest to be safe
						for j, blockIntf := range cArr {
							if block, ok := blockIntf.(map[string]interface{}); ok {
								if t, ok := block["type"].(string); ok && t == "text" {
									if text, ok := block["text"].(string); ok && len(text) > keep {
										block["text"] = text[:keep] + "\n[â€¦truncated by Synapse Proxy L3â€¦]"
										cArr[j] = block
										// Remove subsequent blocks
										msg["content"] = cArr[:j+1]
										break
									}
								}
							}
						}
					} else {
						msg["content"] = contentStr[:keep] + "\n[â€¦truncated by Synapse Proxy L3â€¦]"
					}
				}
			}

			if name == lastToolName && name != "" {
				consecutiveToolCount++
				// Drop repeated tool results beyond the first 2 â€" just
				// replace with a minimal valid JSON that the agent can
				// parse without flagging as injection.
				// Skip if this run is a todo-list (see todo carve-out
				// above): the agent's plan lives here and dropping it
				// silently is a correctness bug, not a token-saving win.
				if !isTodo && consecutiveToolCount > 2 && !isRecentMessage {
					msg["content"] = ""
				}
			} else {
				lastToolName = name
				consecutiveToolCount = 1
			}
		} else {
			lastToolName = ""
			consecutiveToolCount = 0
		}
	}

	// Re-encode preserving all original fields.
	//
	// We use the deterministic encoder (not stdlib json.Marshal) so
	// that two calls to CompressPayload with the same input produce
	// byte-identical output. This is required for provider prompt
	// caching: Anthropic / OpenAI / MiniMax hash the request
	// bytes for cache lookup, and any whitespace / key-order drift
	// would invalidate the cache silently. See
	// marshal_deterministic.go for the rationale.
	return marshalDeterministic(genericPayload)
}

// ModelRouting rewrites payload to a budget-friendly model based on heuristics
func ModelRouting(req *ChatPayload) {
	// Simple predictive routing heuristic
	// If the task involves "format" or "sort" and is using a heavy model, downgrade it.
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(strings.ToLower(lastMsg), "format this as json") || 
		   strings.Contains(strings.ToLower(lastMsg), "sort this list") {
			if req.Model == "gpt-4o" {
				req.Model = "gpt-4o-mini"
				log.Println("Routed gpt-4o -> gpt-4o-mini to save cost")
			}
		}
	}
}
