package optiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffCompressor(t *testing.T) {
	// Seed store
	SetGlobalCompressionStore(newInMemoryCompressionStore())

	// Create a long diff (>= 50 lines) to trigger CCR storage
	var lines []string
	lines = append(lines, "diff --git a/src/main.go b/src/main.go")
	lines = append(lines, "index 8374289..2837498 100644")
	lines = append(lines, "--- a/src/main.go")
	lines = append(lines, "+++ b/src/main.go")
	lines = append(lines, "@@ -1,60 +1,62 @@")
	for i := 1; i <= 25; i++ {
		lines = append(lines, " line context before change")
	}
	lines = append(lines, "-old line to delete")
	lines = append(lines, "+new line to add")
	for i := 1; i <= 25; i++ {
		lines = append(lines, " line context after change")
	}

	originalDiff := strings.Join(lines, "\n")
	payloadStr := `{"messages":[{"role":"user","content":"` + strings.ReplaceAll(originalDiff, "\n", "\\n") + `"}]}`

	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(payloadStr),
	}

	hook := &DiffCompressorHook{}
	resPayload, err := hook.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(resPayload, &body); err != nil {
		t.Fatalf("Failed to unmarshal result payload: %v", err)
	}

	contentStr, ok := body.Messages[0].Content.(string)
	if !ok {
		t.Fatalf("Result content is not string: %T", body.Messages[0].Content)
	}

	// Verify that the output is compressed (contains "...")
	if !strings.Contains(contentStr, "...") {
		t.Errorf("Expected compressed diff to contain elisions '...', got: %s", contentStr)
	}

	// Verify it contains a <<ccr:HASH>> marker
	if !strings.Contains(contentStr, "<<ccr:") {
		t.Errorf("Expected compressed diff to contain CCR marker, got: %s", contentStr)
	}

	// Verify that the original diff is in the CompressionStore
	store := GetGlobalCompressionStore()
	if store.Count() == 0 {
		t.Errorf("Expected at least one item saved in CCR CompressionStore")
	}
}
