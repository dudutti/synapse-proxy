package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
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

var argsPathRe = regexp.MustCompile(`"(?:path|file|file_path|filepath|filename|uri)"\s*:\s*"([^"]+)"`)

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
	key := "optitoken:tools:" + virtualKey + ":" + hex.EncodeToString(h[:])

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
	key := "optitoken:tools:" + virtualKey + ":" + hex.EncodeToString(h[:])
	_ = rdb.Set(ctx, key, body, ttl).Err()
}
