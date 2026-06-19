package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
)

// AgentSignature is the result of trying to identify the agent that
// emitted a request flowing through the proxy. The proxy has no a-priori
// knowledge of "Hermes" or "OpenClaw" â€” we infer the origin from
// request headers and the request body.
type AgentSignature struct {
	ID         string  // stable id used for grouping, e.g. "hermes", "openclaw", "chat-direct"
	Label      string  // human-readable label with emoji prefix
	Confidence float64 // 0..1
	Source     string  // "ua" | "origin" | "prompt" | "tools" | "fallback"
}

// AgentDetectionInput carries everything we can sniff on the incoming
// request without copying the full body.
type AgentDetectionInput struct {
	Headers    http.Header
	Body       []byte         // already-read body bytes
	BodyParsed map[string]any // optional pre-parsed body to avoid double json.Unmarshal
}

// DetectAgent tries a list of increasingly specific rules and returns
// the first match. Falls back to "chat-direct" when nothing matches.
func DetectAgent(in AgentDetectionInput) AgentSignature {
	rules := []struct {
		confidence float64
		source     string
		match      func(in AgentDetectionInput) bool
		result     AgentSignature
	}{
		// --- User-Agent (most reliable when present) -------------------
		{1.0, "ua", uaMatches(`(?i)hermes[-_ ]?agent`), sig("hermes", "🤖 Hermes Agent")},
		{1.0, "ua", uaMatches(`(?i)openclaw`), sig("openclaw", "🦅 OpenClaw")},
		{1.0, "ua", uaMatches(`(?i)claude[-_ ]?code`), sig("claude-code", "🛠️ Claude Code")},
		{1.0, "ua", uaMatches(`(?i)langchain`), sig("langchain", "🔗 LangChain")},
		{1.0, "ua", uaMatches(`(?i)llamaindex`), sig("llamaindex", "🦙 LlamaIndex")},
		{0.95, "ua", uaMatches(`(?i)autogen|crewai|swarm`), sig("multi-agent", "🐝 Multi-Agent Framework")},
		{0.9, "ua", uaMatches(`(?i)curl/`), sig("curl", "📡 curl")},
		{0.9, "ua", uaMatches(`(?i)python-requests|aiohttp|httpx`), sig("python-sdk", "🐍 Python SDK")},

		// --- Origin / Referer (web apps) -------------------------------
		{0.7, "origin", headerMatches("Origin", `(?i)hermes\.`), sig("hermes", "🤖 Hermes Agent")},
		{0.7, "origin", headerMatches("Referer", `(?i)hermes\.`), sig("hermes", "🤖 Hermes Agent")},
		{0.7, "origin", headerMatches("Origin", `(?i)openclaw`), sig("openclaw", "🦅 OpenClaw")},
		{0.7, "origin", headerMatches("Referer", `(?i)openclaw`), sig("openclaw", "🦅 OpenClaw")},

		// --- System prompt signature (very reliable for known agents) --
		{0.85, "prompt", firstSystemMessageMatches(`(?i)you are hermes|hermes agent system prompt`), sig("hermes", "🤖 Hermes Agent")},
		{0.85, "prompt", firstSystemMessageMatches(`(?i)openclaw (os|assistant)|claude[-_ ]?openclaw`), sig("openclaw", "🦅 OpenClaw")},
		{0.8, "prompt", firstSystemMessageMatches(`(?i)you are claude code`), sig("claude-code", "🛠️ Claude Code")},
		{0.75, "prompt", firstSystemMessageMatches(`(?i)you are a helpful assistant`), sig("generic-assistant", "💬 Generic Assistant")},

		// --- Tool / function name prefix (best for code agents) --------
		{0.95, "tools", toolNameMatches(`(?i)^hermes[_.]`), sig("hermes", "🤖 Hermes Agent")},
		{0.95, "tools", toolNameMatches(`(?i)openclaw|claw[_.]`), sig("openclaw", "🦅 OpenClaw")},
		{0.9, "tools", toolNameMatches(`(?i)^(search_|browse_|shell_)`), sig("tool-using-agent", "🔧 Tool-Using Agent")},

		// --- Custom internal headers -----------------------------------
		{0.99, "ua", headerMatches("X-SynapseProxy-Client", `(?i)benchmark`), sig("benchmark", "🧪 Benchmark")},
		{0.99, "ua", headerMatches("X-SynapseProxy-Client", `(?i)playground`), sig("playground", "🎮 Playground")},
	}

	for _, r := range rules {
		if r.match(in) {
			r.result.Confidence = r.confidence
			r.result.Source = r.source
			return r.result
		}
	}
	return AgentSignature{
		ID:         "chat-direct",
		Label:      "ðŸ’¬ Chat direct",
		Confidence: 0.1,
		Source:     "fallback",
	}
}

// ExtractSessionID walks a chain of well-known header / body fields to
// find a session/thread/conversation identifier. Returns "" if none.
//
// Order of precedence:
//  1. X-Session-Id / X-Conversation-Id / X-Thread-Id / X-Request-Id headers
//  2. body.metadata.session_id / thread_id / conversation_id
//  3. body.user (string) â€” a stable user id is a decent proxy
//  4. body.system (Anthropic) â€” hashed so sessions without an explicit id
//     are still grouped by their system context
func ExtractSessionID(headers http.Header, body map[string]any) string {
	hdrs := []string{"X-Session-Id", "X-Conversation-Id", "X-Thread-Id", "X-Request-Id"}
	for _, h := range hdrs {
		if v := strings.TrimSpace(headers.Get(h)); v != "" {
			return sanitizeSessionID(v)
		}
	}
	if body != nil {
		// 1) nested metadata
		if md, ok := body["metadata"].(map[string]any); ok {
			for _, k := range []string{"session_id", "sessionId", "thread_id", "threadId", "conversation_id", "conversationId"} {
				if v, ok := md[k].(string); ok && v != "" {
					return sanitizeSessionID(v)
				}
			}
		}
		// 2) flat fields (OpenAI `user` is the most stable session proxy in
		//    practice for client integrations; checked first so the resulting
		//    groupId is consistent across pages of the same conversation).
		for _, k := range []string{"session_id", "sessionId", "thread_id", "threadId", "conversation_id", "conversationId", "user"} {
			if v, ok := body[k].(string); ok && v != "" {
				// Prefix `user:` for OpenAI `user` so it cannot collide with an
				// explicit session id that happens to equal the same string.
				if k == "user" {
					return "user:" + sanitizeSessionID(v)
				}
				return sanitizeSessionID(v)
			}
		}
		// 3) Anthropic-style top-level
		if v, ok := body["system"].(string); ok && len(v) > 0 {
			sum := sha1.Sum([]byte(v))
			return "sys-" + hex.EncodeToString(sum[:6])
		}
	}
	return ""
}

// --- helpers ---------------------------------------------------------

func sig(id, label string) AgentSignature {
	return AgentSignature{ID: id, Label: label}
}

func uaMatches(pattern string) func(AgentDetectionInput) bool {
	re := regexp.MustCompile(pattern)
	return func(in AgentDetectionInput) bool {
		return re.MatchString(in.Headers.Get("User-Agent"))
	}
}

func headerMatches(header, pattern string) func(AgentDetectionInput) bool {
	re := regexp.MustCompile(pattern)
	return func(in AgentDetectionInput) bool {
		return re.MatchString(in.Headers.Get(header))
	}
}

func firstSystemMessageMatches(pattern string) func(AgentDetectionInput) bool {
	re := regexp.MustCompile(pattern)
	return func(in AgentDetectionInput) bool {
		messages, _ := in.BodyParsed["messages"].([]any)
		if len(messages) == 0 {
			// Anthropic format: top-level "system" string
			if sys, ok := in.BodyParsed["system"].(string); ok {
				return re.MatchString(sys)
			}
			// last-resort: regex over the raw body (cheap, no full parse)
			return re.Match(in.Body)
		}
		first, _ := messages[0].(map[string]any)
		if first == nil {
			return false
		}
		if first["role"] != "system" {
			return false
		}
		// content can be string or array of {type:text,text:...}
		switch c := first["content"].(type) {
		case string:
			return re.MatchString(c)
		case []any:
			for _, part := range c {
				pm, _ := part.(map[string]any)
				if pm == nil {
					continue
				}
				if t, _ := pm["type"].(string); t == "text" {
					if txt, _ := pm["text"].(string); re.MatchString(txt) {
						return true
					}
				}
			}
		}
		return false
	}
}

func toolNameMatches(pattern string) func(AgentDetectionInput) bool {
	re := regexp.MustCompile(pattern)
	return func(in AgentDetectionInput) bool {
		tools, _ := in.BodyParsed["tools"].([]any)
		for _, t := range tools {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			// OpenAI shape: { type:"function", function:{ name, ... } }
			if fn, ok := tm["function"].(map[string]any); ok {
				if name, _ := fn["name"].(string); re.MatchString(name) {
					return true
				}
			}
			// flat shape: { name: "..." }
			if name, _ := tm["name"].(string); re.MatchString(name) {
				return true
			}
		}
		return false
	}
}

// sanitizeSessionID truncates and strips dangerous chars so it is safe
// to use in URLs and HTML attributes.
func sanitizeSessionID(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 64 {
		s = s[:64]
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			out = append(out, r)
		case r == '-' || r == '_' || r == ':':
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return string(out)
}
