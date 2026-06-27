package optiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSmartCrusher_LosslessCSV(t *testing.T) {
	// Create raw homogeneous JSON array that is long enough to trigger (approx > 800 chars)
	// We'll repeat items to clear the min_tokens_to_crush (200 tokens ≈ 800 chars) and min_items (5) thresholds.
	var items []map[string]interface{}
	for i := 0; i < 15; i++ {
		items = append(items, map[string]interface{}{
			"id":        float64(i),
			"name":      "element_number_" + strings.Repeat("x", 50),
			"status":    "active",
			"score_val": 99.9,
		})
	}
	arrayBytes, _ := json.Marshal(items)
	arrayStr := string(arrayBytes)

	jsonArrayBytes, _ := json.Marshal(string(arrayBytes))
	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(`{"messages":[{"role":"user","content":` + string(jsonArrayBytes) + `}]}`),
	}

	hook := &SmartCrusherHook{}
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

	// Verify that the output is CSV compacted (contains commas, header row, and newline)
	if !strings.Contains(contentStr, "id,name,score_val,status") {
		t.Errorf("Expected CSV format header in content, got: %s", contentStr)
	}

	originalLen := len(arrayStr)
	csvLen := len(contentStr)
	if csvLen >= originalLen {
		t.Errorf("Expected CSV compaction to reduce length. Original: %d, CSV: %d", originalLen, csvLen)
	}
}

func TestSmartCrusher_LossyRowDrop(t *testing.T) {
	// In memory store for testing
	SetGlobalCompressionStore(newInMemoryCompressionStore())

	// Create very long homogeneous array that does NOT save enough via CSV to trigger lossless,
	// or we force it by making keys small but repeating items.
	// Actually, if we make values very short but keys long:
	// If keys are extremely long but values are short, CSV compaction saves a lot.
	// But if values are extremely long and keys are small, CSV compaction doesn't save as much as 15%.
	// Wait, let's create a scenario where CSV is NOT 15% savings or we just want to test row drop.
	// Actually, in our code:
	// originalLen - csvLen
	// Let's make an array with 20 objects, where each object has key "k" and value is a 100-char string.
	// original JSON item: `{"k":"..."}` (approx 110 chars). Array: 20 * 110 = 2200 chars.
	// CSV header: `k\n`. CSV values: 20 * 102 chars = 2040 chars.
	// originalLen - csvLen = 2200 - 2042 = 158 chars.
	// ratio: 158 / 2200 = 7.1% (which is < 15%).
	// So lossless CSV will NOT trigger, and it will fall through to lossy row drop!
	var items []map[string]interface{}
	for i := 0; i < 20; i++ {
		items = append(items, map[string]interface{}{
			"k": "val_" + strings.Repeat("a", 100),
		})
	}
	arrayBytes, _ := json.Marshal(items)
	arrayStr := string(arrayBytes)

	jsonArrayBytes, _ := json.Marshal(string(arrayBytes))
	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(`{"messages":[{"role":"user","content":` + string(jsonArrayBytes) + `}]}`),
	}

	hook := &SmartCrusherHook{}
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

	// Verify that it is row dropped and contains the sentinel marker "_ccr_dropped"
	if !strings.Contains(contentStr, "_ccr_dropped") {
		t.Errorf("Expected content to contain _ccr_dropped sentinel, got: %s", contentStr)
	}

	// Verify it contains a <<ccr:HASH...>> marker
	if !strings.Contains(contentStr, "ccr:") {
		t.Errorf("Expected content to contain ccr: marker, got: %s", contentStr)
	}

	// Verify that the original payload is in the CompressionStore
	store := GetGlobalCompressionStore()
	if store.Count() == 0 {
		t.Errorf("Expected at least one item saved in CCR CompressionStore")
	}

	// Retrieve by looking up the hash key.
	// Let's parse out the hash from the contentStr: "ccr:HASH "
	parts := strings.Split(contentStr, "ccr:")
	if len(parts) < 2 {
		t.Fatalf("Could not parse HASH from output: %s", contentStr)
	}
	hash := strings.Split(parts[1], " ")[0]
	// If the string contains escaped characters (like \u003e or >) we strip them
	hash = strings.TrimRight(hash, "\\\">")

	original, err := store.Lookup(hash)
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if string(original) != arrayStr {
		t.Errorf("Expected retrieved value to match original. Got: %s, Expected: %s", string(original), arrayStr)
	}
}
