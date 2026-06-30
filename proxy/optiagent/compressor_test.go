package optiagent

// Tests for the deterministic JSON marshaller and the L3 compressor's
// idempotence property. Run with `go test ./optiagent/...`.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestMarshalDeterministic_Idempotent asserts that calling
// marshalDeterministic twice on the same value produces identical
// bytes. This is the property that allows provider prompt caching to
// work across multiple Synapse Proxy L3 calls.
func TestMarshalDeterministic_Idempotent(t *testing.T) {
	// Build a representative L3-compressed payload: a chat request
	// with a long system prompt, several tool messages, and a few
	// pruned-CoT assistant turns.
	original := map[string]interface{}{
		"model": "claude-sonnet-4-6",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": "You are a senior Go engineer. Be concise.",
			},
			map[string]interface{}{
				"role":    "user",
				"content": "Refactor the auth middleware",
			},
			map[string]interface{}{
				"role":    "assistant",
				"content": "[Pruned Thought Process] Refactoring now.",
			},
			map[string]interface{}{
				"role":    "tool",
				"content": "old output truncated by Synapse Proxy L3â€¦",
				"name":    "read_file",
			},
			map[string]interface{}{
				"role":    "assistant",
				"content": "Done. See patch below.",
			},
		},
		"temperature": 0.2,
		"max_tokens":  4096,
	}

	first, err := marshalDeterministic(original)
	if err != nil {
		t.Fatalf("first marshal failed: %v", err)
	}
	// Run the marshal 100 times; all must match.
	for i := 0; i < 100; i++ {
		again, err := marshalDeterministic(original)
		if err != nil {
			t.Fatalf("marshal %d failed: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("marshal %d differs from first\nfirst:  %s\nagain:  %s",
				i, first, again)
		}
	}
}

// TestMarshalDeterministic_KeyOrder asserts that keys are emitted in
// alphabetical order. This is the byte-level property provider caches
// rely on: as long as the same logical payload serializes to the same
// bytes, the cache hits.
func TestMarshalDeterministic_KeyOrder(t *testing.T) {
	// Build a map with insertion order "zebra, alpha, mango". Even
	// though Go's map iteration is random, the deterministic encoder
	// must sort keys alphabetically.
	v := map[string]interface{}{
		"zebra":  1,
		"alpha":  2,
		"mango":  3,
	}
	out, err := marshalDeterministic(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	want := `{"alpha":2,"mango":3,"zebra":1}`
	if string(out) != want {
		t.Fatalf("expected %s, got %s", want, out)
	}
}

// TestMarshalDeterministic_Compact asserts that no whitespace is
// emitted (no spaces around colons, no newlines). Provider caches
// include whitespace in the hash, so a non-compact encoder would
// produce a different prefix hash on every Go run if it accidentally
// indented differently.
func TestMarshalDeterministic_Compact(t *testing.T) {
	v := map[string]interface{}{
		"a": 1,
		"b": []interface{}{1, 2, 3},
		"c": "hello",
	}
	out, err := marshalDeterministic(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	for _, b := range out {
		if b == ' ' || b == '\n' || b == '\t' {
			t.Fatalf("expected compact output, got whitespace: %s", out)
		}
	}
}

// TestMarshalDeterministic_NoHTMLEscape asserts that non-ASCII
// characters are emitted as-is and not HTML-escaped. Provider caches
// hash the raw bytes, so we want "cafÃ©" -> "cafÃ©", not
// "caf\u00e9".
func TestMarshalDeterministic_NoHTMLEscape(t *testing.T) {
	v := map[string]interface{}{"msg": "cafÃ© rÃ©sumÃ©"}
	out, err := marshalDeterministic(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !bytes.Contains(out, []byte("cafÃ©")) {
		t.Fatalf("expected raw UTF-8 in output, got %s", out)
	}
	if bytes.Contains(out, []byte(`\u00`)) {
		t.Fatalf("expected no Unicode escapes, got %s", out)
	}
}

// TestCompressPayload_Idempotent is the end-to-end idempotence
// test: compress the same payload twice, the results must match
// byte-for-byte. This is the property that the rest of the cache
// pipeline relies on.
func TestCompressPayload_Idempotent(t *testing.T) {
	// A realistic Hermes multi-turn payload with a pruned CoT block.
	raw := []byte(`{
		"model": "MiniMax-M3",
		"messages": [
			{"role": "system", "content": "You are Hermes. <thinking>internal scratch</thinking>"},
			{"role": "user", "content": "add auth middleware"},
			{"role": "assistant", "content": "<thought>plan: 1) parse token 2) check redis 3) inject user</thought>I'll add the middleware."},
			{"role": "tool", "name": "read_file", "content": "` + longBase64ToolOutput() + `"},
			{"role": "tool", "name": "read_file", "content": "` + longBase64ToolOutput() + `"},
			{"role": "tool", "name": "read_file", "content": "` + longBase64ToolOutput() + `"},
			{"role": "assistant", "content": "Done."}
		],
		"temperature": 0.0
	}`)

	first, err := CompressPayload(raw)
	if err != nil {
		t.Fatalf("first compress: %v", err)
	}
	// Validate the result is still valid JSON.
	var check interface{}
	if err := json.Unmarshal(first, &check); err != nil {
		t.Fatalf("compressed output is not valid JSON: %v\nraw: %s", err, first)
	}

	for i := 0; i < 50; i++ {
		again, err := CompressPayload(raw)
		if err != nil {
			t.Fatalf("compress %d: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("compress %d differs\nfirst len=%d, again len=%d",
				i, len(first), len(again))
		}
	}
}

// TestCompressPayload_StableKeyOrder is the property that matters
// for cache hit: a second CompressPayload on a payload that
// LOGICALLY matches the first (same messages, same CoT pruned) must
// serialize the message map in the same order. We can't assert the
// message-array order changes since it follows input order, but we
// can assert that the object-key order inside each message is
// stable.
func TestCompressPayload_StableKeyOrder(t *testing.T) {
	raw := []byte(`{
		"messages": [
			{"role": "assistant", "content": "Hi"}
		]
	}`)
	first, _ := CompressPayload(raw)
	// After compression, the assistant message's content may have
	// been transformed; we only check the key order is stable.
	if !bytes.Contains(first, []byte(`"content":"Hi"`)) &&
		!bytes.Contains(first, []byte(`"content":"[Pruned Thought Process]Hi"`)) {
		t.Fatalf("unexpected first output: %s", first)
	}
	for i := 0; i < 20; i++ {
		again, _ := CompressPayload(raw)
		if !bytes.Equal(first, again) {
			t.Fatalf("compress %d differs: %s vs %s", i, first, again)
		}
	}
}

// longBase64ToolOutput returns a string of sufficient length to
// trigger the L3 "stale tool" truncation path (200+ chars).
func longBase64ToolOutput() string {
	const filler = "abcdef0123456789"
	out := make([]byte, 0, len(filler)*50)
	for i := 0; i < 50; i++ {
		out = append(out, filler...)
	}
	return string(out)
}

// TestCompressPayload_PreservesTodoList asserts that the L3 compressor
// does NOT truncate the result of a read_todos / write_todo tool call,
// because Hermes and similar agents store their plan there and need
// the full list visible on every turn.
func TestCompressPayload_PreservesTodoList(t *testing.T) {
	todoJSON := `{"todos":[{"id":"1","content":"Analyse compression L3","status":"in_progress"},{"id":"2","content":"Refactor CompressPayload","status":"pending"},{"id":"3","content":"Run e2e tests","status":"pending"}]}`
	// Wrap the todo list as a tool result with a long synthetic body
	// that would normally trigger the 200-char truncation. The todo
	// signature is in the first 512 chars so it must survive.
	body := todoJSON + strings.Repeat(" padding padding padding ", 30)

	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"messages": [
			{"role": "user", "content": "what's on my plate?"},
			{"role": "tool", "tool_call_id": "call_1", "name": "read_todos", "content": ` + strconvQuote(body) + `},
			{"role": "assistant", "content": "Let me check the todos."},
			{"role": "tool", "tool_call_id": "call_2", "name": "write_todo", "content": ` + strconvQuote(body) + `},
			{"role": "tool", "tool_call_id": "call_3", "name": "write_todo", "content": ` + strconvQuote(body) + `},
			{"role": "user", "content": "ok continue"}
		]
	}`)

	out, err := CompressPayload(raw)
	if err != nil {
		t.Fatalf("CompressPayload: %v", err)
	}

	if !bytes.Contains(out, []byte(`"status":"in_progress"`)) {
		t.Fatalf("todo list status lost after compression:\n%s", out)
	}
	if bytes.Contains(out, []byte("…truncated by Synapse Proxy L3")) {
		t.Fatalf("todo list was truncated despite todo signature:\n%s", out)
	}
	// 5 todos × ≥3 occurrences each must remain in the output.
	count := bytes.Count(out, []byte(`"id":"`))
	if count < 15 {
		t.Fatalf("expected ≥15 todo id entries after compression, got %d:\n%s", count, out)
	}
}

// TestCompressPayload_TruncatesNonTodoToolOutput is the negative case:
// non-todo tool outputs SHOULD still be truncated so we keep the L3
// savings for noisy tool calls.
func TestCompressPayload_TruncatesNonTodoToolOutput(t *testing.T) {
	body := strings.Repeat("blah blah blah ", 200)

	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"messages": [
			{"role": "user", "content": "look at file"},
			{"role": "tool", "tool_call_id": "call_1", "name": "read_file", "content": ` + strconvQuote(body) + `},
			{"role": "assistant", "content": "seen"}
		]
	}`)

	out, err := CompressPayload(raw)
	if err != nil {
		t.Fatalf("CompressPayload: %v", err)
	}
	if !bytes.Contains(out, []byte("…truncated by Synapse Proxy L3")) {
		t.Fatalf("non-todo tool output was NOT truncated, regression:\n%s", out)
	}
}

// strconvQuote is a tiny helper to JSON-encode a Go string without
// pulling in fmt at the top of the test file (keeps the test
// readable inline).
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
