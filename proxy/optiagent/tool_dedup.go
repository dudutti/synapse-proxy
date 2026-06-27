package optiagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
	"synapse-proxy/cache"
	"time"

	"github.com/redis/go-redis/v9"
)

// ToolCall describes a single file-read intent extracted from the
// request body. Two requests that read the same file via different tool
// names ("read_file" vs "cat", "readFile" vs "Read") end up with the
// same FilePath, which is what we dedup on.
type ToolCall struct {
	ToolName string
	FilePath string
	Command  string // for shell-based reads: the full cat/head/tail command
}

// ExtractToolCalls walks the request body and pulls out every tool call
// that looks like a file read.
func ExtractToolCalls(payload []byte) []ToolCall {
	var out []ToolCall
	if tcList := extractOpenAIToolCalls(payload); len(tcList) > 0 {
		out = append(out, tcList...)
	}
	if fcList := extractFromText(payload); len(fcList) > 0 {
		out = append(out, fcList...)
	}
	return out
}

// fileReadTools lists tool names that read a file by path.
var fileReadTools = map[string]bool{
	"read_file":     true,
	"readFile":      true,
	"read":          true,
	"file_read":     true,
	"fileRead":      true,
	"fs.read":       true,
	"fs.readFile":   true,
	"Get-Content":   true,
	"get_content":   true,
	"cat":           true,
	"load_file":     true,
	"open_file":     true,
	"view":          true,
}

func extractOpenAIToolCalls(payload []byte) []ToolCall {
	matches := openAIToolCallRe.FindAllSubmatch(payload, -1)
	var out []ToolCall
	for _, m := range matches {
		name := string(m[1])
		args := string(m[2])
		if !fileReadTools[strings.ToLower(name)] {
			continue
		}
		path := extractPathFromArgs(args)
		if path == "" {
			continue
		}
		out = append(out, ToolCall{ToolName: name, FilePath: path})
	}
	return out
}

var openAIToolCallRe = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"\s*,\s*"arguments"\s*:\s*"((?:[^"\\]|\\.)*)"`)

var argsPathRe = regexp.MustCompile(`\\?"(?:path|file|file_path|filepath|filename|uri)\\?"\s*:\s*\\?"([^"\\]+)\\?"`)

func extractPathFromArgs(argsBlob string) string {
	if m := argsPathRe.FindStringSubmatch(argsBlob); len(m) > 1 {
		return unescapeJSON(m[1])
	}
	return ""
}

func unescapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\\`, "\x00")
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, "\x00", `\\`)
	return s
}

func extractFromText(payload []byte) []ToolCall {
	last := extractLastUserText(payload)
	if last == "" {
		return nil
	}
	var out []ToolCall
	for _, m := range catRe.FindAllStringSubmatch(last, -1) {
		out = append(out, ToolCall{ToolName: "cat", FilePath: m[1], Command: m[0]})
	}
	for _, m := range readRe.FindAllStringSubmatch(last, -1) {
		out = append(out, ToolCall{ToolName: m[1], FilePath: m[2]})
	}
	return out
}

var (
	catRe  = regexp.MustCompile(`\bcat\s+([^\s"';|&<>]+)`)
	readRe = regexp.MustCompile(`\b(read_file|readFile|Read|get_content|load_file|view)\s*\(\s*["']([^"']+)["']\s*\)`)
)

func extractLastUserText(payload []byte) string {
	idx := strings.LastIndex(string(payload), `"role":"user"`)
	if idx < 0 {
		return ""
	}
	tail := string(payload)[idx:]
	cIdx := strings.Index(tail, `"content":"`)
	if cIdx < 0 {
		return ""
	}
	start := cIdx + len(`"content":"`)
	end := start
	for end < len(tail) {
		if tail[end] == '\\' {
			end += 2
			continue
		}
		if tail[end] == '"' {
			break
		}
		end++
	}
	if end >= len(tail) {
		return ""
	}
	return unescapeJSON(tail[start:end])
}

// ToolDedupResult is what CheckToolDedup returns.
type ToolDedupResult struct {
	HasDup    bool
	FilePath  string
	ToolName  string
	ReuseBody []byte
	FirstSeen time.Time
	HitCount  int
}

// CheckToolDedup looks at the current request's tool calls and returns a
// dedup hint if the same file is being read again.
func CheckToolDedup(ctx context.Context, rdb *redis.Client, virtualKey string, calls []ToolCall, currentBody []byte, ttl time.Duration) ToolDedupResult {
	if len(calls) == 0 {
		return ToolDedupResult{}
	}
	var path string
	for _, c := range calls {
		if c.FilePath == "" {
			return ToolDedupResult{}
		}
		if path == "" {
			path = c.FilePath
		} else if path != c.FilePath {
			return ToolDedupResult{}
		}
	}
	if path == "" {
		return ToolDedupResult{}
	}

	h := sha256.Sum256([]byte(path))
	key := "synapse:tools:" + virtualKey + ":" + hex.EncodeToString(h[:])

	existing, err := rdb.Get(ctx, key).Bytes()
	if err == nil && len(existing) > 0 {
		_ = rdb.Incr(ctx, key+":hits").Err()
		return ToolDedupResult{
			HasDup:    true,
			FilePath:  path,
			ToolName:  calls[0].ToolName,
			ReuseBody: existing,
		}
	}

	placeholder := []byte(`{"path":"` + path + `","toolName":"` + calls[0].ToolName + `"}`)
	_ = rdb.Set(ctx, key, placeholder, ttl).Err()
	return ToolDedupResult{}
}

// StoreToolDedupBody caches the tool's response body so a future
// identical read can be served from this cache.
func StoreToolDedupBody(ctx context.Context, rdb *redis.Client, virtualKey, filePath string, body []byte, ttl time.Duration) {
	if len(body) == 0 {
		return
	}
	h := sha256.Sum256([]byte(filePath))
	key := "synapse:tools:" + virtualKey + ":" + hex.EncodeToString(h[:])
	_ = rdb.Set(ctx, key, body, ttl).Err()
}

// Message is a representation of an OpenAI-style chat completion message.
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls,omitempty"`
}

// StoreCompletedToolCalls parses the request payload and stores any completed tool responses
// in the Redis tool cache index.
func StoreCompletedToolCalls(ctx context.Context, rdb *redis.Client, virtualKey string, payload []byte) {
	if rdb == nil || len(payload) == 0 {
		return
	}

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || len(body.Messages) == 0 {
		return
	}

	// We iterate through messages. For each message with role: tool (or function),
	// we search backward for the assistant message that requested it.
	for i, msg := range body.Messages {
		if msg.Role == "tool" || msg.Role == "function" {
			var outputStr string
			if str, ok := msg.Content.(string); ok {
				outputStr = str
			} else if bytes, ok := msg.Content.([]byte); ok {
				outputStr = string(bytes)
			} else {
				// Try marshalling non-string content (e.g. JSON array/object)
				if b, err := json.Marshal(msg.Content); err == nil {
					outputStr = string(b)
				}
			}

			if outputStr == "" {
				continue
			}

			// Find matching tool call in previous messages
			var matchedName, matchedArgs string
			matched := false

			// Match by ID if tool_call_id is present
			if msg.ToolCallID != "" {
				for j := i - 1; j >= 0; j-- {
					prev := body.Messages[j]
					if prev.Role == "assistant" && len(prev.ToolCalls) > 0 {
						for _, tc := range prev.ToolCalls {
							if tc.ID == msg.ToolCallID {
								matchedName = tc.Function.Name
								matchedArgs = tc.Function.Arguments
								matched = true
								break
							}
						}
					}
					if matched {
						break
					}
				}
			}

			// Fallback: match by Name if tool_call_id didn't match or is missing
			if !matched && msg.Name != "" {
				for j := i - 1; j >= 0; j-- {
					prev := body.Messages[j]
					if prev.Role == "assistant" && len(prev.ToolCalls) > 0 {
						for _, tc := range prev.ToolCalls {
							if tc.Function.Name == msg.Name {
								matchedName = tc.Function.Name
								matchedArgs = tc.Function.Arguments
								matched = true
								break
							}
						}
					}
					if matched {
						break
					}
				}
			}

			if matched && matchedName != "" && matchedArgs != "" {
				StoreToolCallCache(ctx, rdb, virtualKey, matchedName, matchedArgs, outputStr)
			}
		}
	}
}

// StoreToolCallCache calculates embeddings for the tool arguments and saves the tool output to Redis.
func StoreToolCallCache(ctx context.Context, rdb *redis.Client, virtualKey string, toolName string, arguments string, output string) {
	if output == "" || rdb == nil {
		return
	}

	h := sha256.Sum256([]byte(arguments))
	exactKey := "synapse:toolcache:exact:" + virtualKey + ":" + toolName + ":" + hex.EncodeToString(h[:])
	_ = rdb.Set(ctx, exactKey, []byte(output), 24*time.Hour).Err()

	if cache.GlobalEmbedder == nil {
		return
	}

	// Retrieve embedding vector of arguments
	vector, err := cache.GlobalEmbedder.GenerateEmbedding(arguments)
	if err != nil || len(vector) == 0 {
		return
	}

	key := "synapse:toolcache:" + virtualKey + ":" + toolName + ":" + hex.EncodeToString(h[:])

	// Convert float32 vector to byte array for Redis
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, vector); err != nil {
		return
	}
	vectorBytes := buf.Bytes()

	redisData := map[string]interface{}{
		"vk":        virtualKey,
		"tool":      toolName,
		"vector":    vectorBytes,
		"arguments": arguments,
		"response":  output,
	}

	_ = rdb.HMSet(ctx, key, redisData).Err()
	_ = rdb.Expire(ctx, key, 24*time.Hour).Err()
}

// QueryToolCallCache checks exact match and semantic VSS match for a tool's arguments.
func QueryToolCallCache(ctx context.Context, rdb *redis.Client, virtualKey string, toolName string, arguments string, semanticTolerance float64) (string, bool) {
	if rdb == nil {
		return "", false
	}

	// 1. Exact match first
	h := sha256.Sum256([]byte(arguments))
	exactKey := "synapse:toolcache:exact:" + virtualKey + ":" + toolName + ":" + hex.EncodeToString(h[:])
	exactVal, err := rdb.Get(ctx, exactKey).Result()
	if err == nil && exactVal != "" {
		return exactVal, true
	}

	// Check exact match on the VSS key format as fallback
	vssExactKey := "synapse:toolcache:" + virtualKey + ":" + toolName + ":" + hex.EncodeToString(h[:])
	if vssVal, err := rdb.HGet(ctx, vssExactKey, "response").Result(); err == nil && vssVal != "" {
		return vssVal, true
	}

	// 2. Semantic VSS match
	if cache.GlobalEmbedder == nil {
		return "", false
	}

	vector, err := cache.GlobalEmbedder.GenerateEmbedding(arguments)
	if err != nil || len(vector) == 0 {
		return "", false
	}

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, vector); err != nil {
		return "", false
	}
	vectorBytes := buf.Bytes()

	escapedVK := EscapeRedisTag(virtualKey)
	escapedTool := EscapeRedisTag(toolName)
	query := "(@vk:{" + escapedVK + "} @tool:{" + escapedTool + "})=>[KNN 1 @vector $query_vec AS score]"
	
	res, err := rdb.Do(ctx, "FT.SEARCH", "idx:toolcache", query, "PARAMS", "2", "query_vec", vectorBytes, "RETURN", "2", "score", "response", "DIALECT", "2").Result()
	if err != nil {
		return "", false
	}

	resArr, ok := res.([]interface{})
	if !ok || len(resArr) <= 2 {
		return "", false
	}

	fields, ok := resArr[2].([]interface{})
	if !ok {
		return "", false
	}

	var score float64
	var hitResponse string
	for i := 0; i < len(fields); i += 2 {
		k := fields[i].(string)
		if k == "score" {
			score, _ = strconv.ParseFloat(fields[i+1].(string), 64)
		} else if k == "response" {
			hitResponse = fields[i+1].(string)
		}
	}

	if score < semanticTolerance && hitResponse != "" {
		log.Printf("[SemanticToolDedup] Semantic hit for tool %s: input %s matched cached query with cosine distance %f", toolName, arguments, score)
		return hitResponse, true
	}

	return "", false
}
