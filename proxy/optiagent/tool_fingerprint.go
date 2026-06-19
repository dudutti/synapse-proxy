// Package optiagent — tool-call fingerprinting.
//
// The fingerprint is an OBSERVER, not a blocker. It counts
// identical (tool, args) retries in Redis and surfaces the
// count via response headers; the actual 429 is emitted by
// the proxy AFTER the cache check, only when the cache missed
// AND the tool is not read-only. See docs/agent_firewall.md
// for the full design.
package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// FingerprintResult is what CheckToolFingerprint returns.
// See docs/agent_firewall.md for the strategy.
type FingerprintResult struct {
	IsLoop        bool
	ToolName      string
	LoopCount     int
	WindowSecs    int
	RetryAfterSecs int
}

const (
	FingerprintThreshold     = 4
	FingerprintWindowSecs    = 30
	FingerprintRetryAfterSecs = 60
	CacheReplayMaxAgeSecs    = 60
)

var readOnlyToolNames = map[string]bool{
	"todo": true, "todos": true,
	"todo_write": true, "todo_read": true, "todo_update": true,
	"read_todos": true, "write_todo": true, "update_todo": true,
	"plan": true, "plans": true,
	"think": true, "reflect": true, "reason": true, "step_back": true,
	"task": true, "tasks": true,
	"notebook_read": true, "notebook_write": true,
	"read_file": true, "list_files": true, "list_dir": true,
	"get_status": true, "get_config": true, "get_state": true,
	"search_web": true, "web_search": true,
}

var readOnlyToolPrefixes = []string{
	"read_", "list_", "get_", "find_", "search_", "fetch_", "lookup_", "query_", "check_", "inspect_",
}

func isReadOnlyTool(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if readOnlyToolNames[lower] {
		return true
	}
	for _, p := range readOnlyToolPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func isTodoLikeTool(name string) bool {
	return isReadOnlyTool(name)
}

// openAIFingerprintRe matches every OpenAI-style tool call
// regardless of function name (unlike the file-read-only regex
// in tool_dedup.go). We capture:
//   - m[1]: function name (e.g. "search_web")
//   - m[2]: arguments JSON-encoded string (e.g. "{\"q\":\"...\"}")
//
// The arguments are JSON-encoded inside the JSON request body,
// so we have to JSON-decode them before hashing. We do that in
// the caller (CheckToolFingerprint), not here, because regex
// capture is just for *finding* the calls, not *parsing* them.
//
// IMPORTANT: this regex matches `"name":"X","arguments":"Y"` but
// in production payloads the JSON optimiser often reorders
// fields (Go's encoding/json sorts map keys alphabetically), so
// we get `"arguments":"Y","name":"X"` instead. The regex below
// accepts both orderings via alternation. We do NOT try to be
// clever about whitespace either — JSON has none between tokens.
var openAIFingerprintRe = regexp.MustCompile(
	`"name"\s*:\s*"([^"]+)"\s*,\s*"arguments"\s*:\s*"((?:[^"\\]|\\.)*)"` +
		`|` +
		`"arguments"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,\s*"name"\s*:\s*"([^"]+)"`,
)

// ExtractAllToolCalls returns ALL tool calls in the request
// body, not just file reads. This is what the fingerprint
// detector needs — the kill switch fires on any tool, so we
// can't be picky.
//
// The regex matches OpenAI-shaped tool calls: any function name
// with any arguments, in either field order (name-then-arguments
// or arguments-then-name). We don't try to be clever about
// non-OpenAI formats here; if the agent uses a custom protocol,
// the fingerprint detector silently returns nothing and the kill
// switch (which looks at the full body) is the only line of
// defence. That's an acceptable trade-off.
func ExtractAllToolCalls(payload []byte) []ToolCall {
	matches := openAIFingerprintRe.FindAllSubmatch(payload, -1)
	out := make([]ToolCall, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		// m[1] = name  (when name comes first)
		// m[2] = args  (when name comes first)
		// m[3] = args  (when arguments comes first)
		// m[4] = name  (when arguments comes first)
		var name, args string
		if len(m[1]) > 0 {
			name = string(m[1])
			args = string(m[2])
		} else {
			args = string(m[3])
			name = string(m[4])
		}
		// Dedupe on the (name, args) tuple inside a single
		// request — if the assistant tried to call the same
		// tool twice in one round-trip, we count it once.
		key := name + "|" + args
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ToolCall{ToolName: name, Command: args})
	}
	return out
}

// CheckToolFingerprint inspects the request body for tool calls
// and returns a FingerprintResult. The proxy uses the result to
// decide whether to short-circuit with HTTP 429.
//
// One call per request. The function:
//  1. Extracts every (tool_name, args) tuple from the body
//  2. For each tuple, hashes (vk, tool_name, args) and checks
//     the Redis counter `synapse:fp:<vk>:<tool>:<hash>`
//  3. Increments the counter and sets a TTL
//  4. Returns the highest LoopCount across all tuples (the
//     first one to cross the threshold wins)
//
// We return the worst tuple because the proxy can only send
// one response — sending HTTP 429 for the first offender is
// enough to make the agent back off.
func CheckToolFingerprint(ctx context.Context, rdb *redis.Client, virtualKey string, payload []byte) FingerprintResult {
	calls := ExtractAllToolCalls(payload)
	if len(calls) == 0 {
		return FingerprintResult{}
	}

	worst := FingerprintResult{WindowSecs: FingerprintWindowSecs, RetryAfterSecs: FingerprintRetryAfterSecs}

	for _, c := range calls {
		// Skip todo-like tools entirely. These are tools
		// that agents legitimately re-invoke with the same
		// arguments as they iterate on their plan.
		// Fingerprinting them produces constant false
		// positives (Hermes-style agents trip the detector
		// on every todo round-trip). The carve-out is
		// hard-coded for now; operators can extend it per
		// key via Redis (future work).
		if isTodoLikeTool(c.ToolName) {
			continue
		}

		// Hash (tool_name + args). We use sha256 because the
		// key is short anyway and sha256 is collision-safe
		// for our threat model (we just want to count, not
		// defend against adversaries).
		h := sha256.Sum256([]byte(c.ToolName + "\x00" + c.Command))
		fpHex := hex.EncodeToString(h[:])
		key := "synapse:fp:" + virtualKey + ":" + c.ToolName + ":" + fpHex

		// INCR returns the new value. EXPIRE sets the TTL on
		// every call so the counter eventually forgets. We use
		// a pipeline to avoid a round-trip per call.
		pipe := rdb.Pipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, time.Duration(FingerprintWindowSecs+10)*time.Second)
		if _, err := pipe.Exec(ctx); err != nil {
			// non-fatal: treat as not a loop. The kill switch
			// is the safety net if Redis is degraded.
			continue
		}

		count, err := incr.Result()
		if err != nil {
			continue
		}
		countInt, _ := strconv.Atoi(strconv.FormatInt(count, 10))
		if int(countInt) > worst.LoopCount {
			worst.LoopCount = int(countInt)
			worst.ToolName = c.ToolName
		}
		if int(countInt) >= FingerprintThreshold {
			worst.IsLoop = true
			// We can return immediately — we only need the
			// first tuple to cross the threshold.
			return worst
		}
	}
	return worst
}

// ShouldReuseCache determines if a cache hit should be re-served.
// Option 3 strategy:
//   - If the request contains no tool calls, return true (always allow).
//   - If the request contains only read-only tool calls (e.g., todo, think, plan), return true (whitelist).
//   - If the request contains any stateful/mutation tool call (e.g., write_file, shell_exec),
//     return true only if the cache entry is fresh (age <= maxAgeSecs, default 60s).
func ShouldReuseCache(ctx context.Context, rdb *redis.Client, payload []byte, cacheKey string, cacheTtl int, maxAgeSecs int) bool {
	calls := ExtractAllToolCalls(payload)
	if len(calls) == 0 {
		return true
	}

	allReadOnly := true
	for _, c := range calls {
		if !isReadOnlyTool(c.ToolName) {
			allReadOnly = false
			break
		}
	}

	if allReadOnly {
		return true
	}

	// Stateful/mutation tool call present. Check age via Redis TTL.
	ttlRemaining, err := rdb.TTL(ctx, cacheKey).Result()
	if err != nil || ttlRemaining <= 0 {
		// If we can't get the TTL or it expired/has no TTL, do not reuse
		return false
	}

	totalTtl := time.Duration(cacheTtl) * time.Second
	age := totalTtl - ttlRemaining
	if age > time.Duration(maxAgeSecs)*time.Second {
		return false
	}

	return true
}