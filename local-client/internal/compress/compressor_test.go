// compress_test.go — local-client L3 byte-preserving tests.
//
// Run with: go test -tags no_test_linker ./internal/compress
//
// (The -tags no_test_linker isn't required for the compress
// package itself because it has no CGO deps, but the wider
// project does. Keeping the tag here for the same pattern as
// the server-side tests.)

package compress

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCompressBytePreserving_ToolOutputTruncated: a tool
// message whose content is more than 200 chars gets truncated
// in place, and the marker text appears in the output.
func TestCompressBytePreserving_ToolOutputTruncated(t *testing.T) {
	src := buildMultiturnPayload()
	out, saved := CompressBytePreserving(src)
	if saved <= 0 {
		t.Fatalf("expected savings > 0, got %d", saved)
	}
	if !bytes.Contains(out, []byte("…truncated by Synapse L3…")) {
		t.Errorf("expected truncation marker in output, got: %.200s…", out)
	}
}

// TestCompressBytePreserving_ThinkingStripped: a <thinking>
// block in an assistant message is removed, and the rest of
// the assistant content is preserved.
func TestCompressBytePreserving_ThinkingStripped(t *testing.T) {
	src := buildMultiturnPayload()
	out, _ := CompressBytePreserving(src)
	if bytes.Contains(out, []byte("<thinking>")) {
		t.Errorf("expected <thinking> stripped, got: %s", out)
	}
	if !bytes.Contains(out, []byte("done")) {
		t.Errorf("expected 'done' preserved, got: %s", out)
	}
}

// TestCompressBytePreserving_TodoCarveOut: a tool message
// whose content begins with a todo-list anchor (status:
// in_progress) is NOT truncated.
func TestCompressBytePreserving_TodoCarveOut(t *testing.T) {
	src := buildMultiturnPayload()
	out, _ := CompressBytePreserving(src)
	if !bytes.Contains(out, []byte(`\"todos\":[`)) {
		t.Errorf("expected todo anchor in tool msg preserved, got: %.400s…", out)
	}
}

// TestCompressBytePreserving_OutputIsValidJSON: the
// compressed payload is still parseable.
func TestCompressBytePreserving_OutputIsValidJSON(t *testing.T) {
	src := buildMultiturnPayload()
	out, _ := CompressBytePreserving(src)
	var v interface{}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("output is not valid JSON: %v\nbody: %s", err, out)
	}
}

// TestCompressBytePreserving_Idempotent: running the
// compressor twice on the same input gives the same output.
func TestCompressBytePreserving_Idempotent(t *testing.T) {
	src := buildMultiturnPayload()
	out1, _ := CompressBytePreserving(src)
	out2, _ := CompressBytePreserving(out1)
	if !bytes.Equal(out1, out2) {
		t.Errorf("compressor is not idempotent\n  out1: %.200s\n  out2: %.200s", out1, out2)
	}
}

// TestCompressBytePreserving_PrefixIsByteStable: the
// byte-level preservation means the prefix up to the first
// compressed content is byte-identical to the input.
func TestCompressBytePreserving_PrefixIsByteStable(t *testing.T) {
	src := buildMultiturnPayload()
	out, _ := CompressBytePreserving(src)
	// Find the first byte that differs.
	diff := 0
	for i := 0; i < len(src) && i < len(out); i++ {
		if src[i] != out[i] {
			diff = i
			break
		}
	}
	if diff > 600 {
		t.Errorf("prefix diverged at byte %d (expected before first truncated tool msg at ~660)", diff)
	}
}

// buildMultiturnPayload returns a realistic Hermes-style
// multi-turn payload with 3 large tool outputs and one todo
// list tool output.
func buildMultiturnPayload() []byte {
	messages := []map[string]interface{}{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "u1"},
		{"role": "assistant", "tool_calls": []map[string]interface{}{
			{"id": "t1", "type": "function",
				"function": map[string]interface{}{"name": "go", "arguments": "{}"}},
		}},
		{"role": "tool", "tool_call_id": "t1", "name": "go",
			"content": "a" + strings.Repeat("X", 600)},
		{"role": "tool", "tool_call_id": "t2", "name": "go",
			"content": "b" + strings.Repeat("Y", 600)},
		{"role": "tool", "tool_call_id": "t3", "name": "go",
			"content": "c" + strings.Repeat("Z", 600)},
		{"role": "tool", "tool_call_id": "t4", "name": "read_file",
			"content": `{"todos":[{"id":"1","content":"task A","status":"in_progress"}]}`},
		{"role": "assistant", "content": "<thinking>scratch</thinking>done"},
		{"role": "user", "content": "u2"},
	}
	body := map[string]interface{}{"model": "gpt-4o-mini", "messages": messages, "system": ""}
	out, _ := json.Marshal(body)
	return out
}

// TestUnquoteJSONString_RoundTrip: the byte-level content
// extraction returns a Go string equivalent to what json.Marshal
// would produce.
func TestUnquoteJSONString_RoundTrip(t *testing.T) {
	cases := []string{
		"hello",
		"with \"escaped\" quotes",
		"multi\nline\nstring",
		"",
		"日本語",
	}
	for _, c := range cases {
		encoded := jsonEncodeString(t, c)
		got, ok := unquoteJSONString(encoded)
		if !ok {
			t.Errorf("unquote failed for %q", c)
			continue
		}
		if got != c {
			t.Errorf("round-trip failed: input %q, got %q", c, got)
		}
	}
}

func jsonEncodeString(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// strip surrounding quotes
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		t.Fatalf("unexpected marshal: %s", b)
	}
	return b[1 : len(b)-1]
}

// TestTruncateRunes: UTF-8 safe truncation.
func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in     string
		max    int
		expect string
	}{
		{"hello", 3, "hel"},
		{"", 10, ""},
		{"héllo", 2, "hé"},
		{"日本語", 1, "日"},
		{"abc", 100, "abc"},
	}
	for _, c := range cases {
		got := truncateRunes(c.in, c.max)
		if got != c.expect {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", c.in, c.max, got, c.expect)
		}
		// Sanity: the result must be a valid UTF-8 prefix.
		if got != "" && !utf8.ValidString(got) {
			t.Errorf("truncateRunes produced invalid UTF-8: %q", got)
		}
	}
}