// Package optiagent — Smart JSON array crusher hook.
//
// Implements Phase 2: smart crushing and lossless CSV compaction
// of JSON arrays in the chat completion prompt messages.
package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

// SmartCrusherHook is a BeforeRequest hook that detects and
// compresses homogeneous JSON arrays in message contents.
type SmartCrusherHook struct{}

// Name returns the hook name.
func (h *SmartCrusherHook) Name() string { return "smart_crusher" }

// Priority is 720: after CacheAligner (700) and before CCRCompress (740).
func (h *SmartCrusherHook) Priority() int { return 720 }

// IsEnabled always returns true.
func (h *SmartCrusherHook) IsEnabled(vk string) bool { return true }

// BeforeRequest parses the payload and applies SmartCrusher logic.
func (h *SmartCrusherHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	payload := hctx.OptimizedPayload
	if payload == nil {
		payload = hctx.RawPayload
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || len(body.Messages) == 0 {
		return nil, nil
	}

	modified := false
	for i, msg := range body.Messages {
		if contentStr, ok := msg.Content.(string); ok && len(contentStr) > 0 {
			crushed, ok, strategy := tryCrushJSONArray(contentStr, hctx.VK)
			if ok {
				body.Messages[i].Content = crushed
				modified = true
				log.Printf("[SmartCrusher] applied strategy=%s on message %d", strategy, i)
			}
		}
	}

	if modified {
		newPayload, err := json.Marshal(body)
		if err == nil {
			return newPayload, nil
		}
	}

	return payload, nil
}

// AfterResponse is a no-op.
func (h *SmartCrusherHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	return nil, nil
}

func tryCrushJSONArray(contentStr string, vk string) (string, bool, string) {
	trimmed := strings.TrimSpace(contentStr)
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return "", false, "passthrough"
	}

	var rawArray []interface{}
	if err := json.Unmarshal([]byte(trimmed), &rawArray); err != nil {
		return "", false, "passthrough"
	}

	// 1. Minimum items gate
	if len(rawArray) < 5 { // min_items_to_analyze
		return "", false, "passthrough"
	}

	// 2. Minimum tokens/chars gate (approx 4 chars per token)
	if len(trimmed)/4 < 200 { // min_tokens_to_crush
		return "", false, "passthrough"
	}

	// 3. Extract and check homogeneous objects
	allObjects := true
	var objects []map[string]interface{}
	for _, item := range rawArray {
		m, ok := item.(map[string]interface{})
		if !ok {
			allObjects = false
			break
		}
		objects = append(objects, m)
	}

	if !allObjects {
		return "", false, "passthrough"
	}

	// Calculate keys similarity
	allKeys := make(map[string]bool)
	for _, obj := range objects {
		for k := range obj {
			allKeys[k] = true
		}
	}

	if len(allKeys) == 0 {
		return "", false, "passthrough"
	}

	var totalSimilarity float64
	for _, obj := range objects {
		var matchedKeys float64
		for k := range obj {
			if allKeys[k] {
				matchedKeys++
			}
		}
		totalSimilarity += matchedKeys / float64(len(allKeys))
	}
	avgSimilarity := totalSimilarity / float64(len(objects))

	if avgSimilarity < 0.8 { // similarity_threshold
		return "", false, "passthrough"
	}

	// 4. Try Lossless CSV Compaction
	csvString, csvOk := convertToCSV(objects)
	if csvOk {
		originalLen := len(trimmed)
		csvLen := len(csvString)
		savingsRatio := float64(originalLen-csvLen) / float64(originalLen)
		if savingsRatio >= 0.15 { // lossless_min_savings_ratio
			return csvString, true, "csv_compaction"
		}
	}

	// 5. Try Lossy Row Drop with CCR marker
	n := len(objects)
	firstCount := int(float64(n) * 0.3)
	lastCount := int(float64(n) * 0.15)

	if firstCount < 1 {
		firstCount = 1
	}
	if lastCount < 1 {
		lastCount = 1
	}
	if firstCount+lastCount >= n {
		return "", false, "passthrough"
	}

	droppedCount := n - (firstCount + lastCount)
	droppedItems := rawArray[firstCount : firstCount+droppedCount]

	// Compute stable 12-char SHA-256 hash of dropped content
	droppedBytes, _ := json.Marshal(droppedItems)
	hSum := sha256.Sum256(droppedBytes)
	droppedHash := hex.EncodeToString(hSum[:])[:12]

	// CCR dropped sentinel
	sentinel := map[string]interface{}{
		"_ccr_dropped": fmt.Sprintf("<<ccr:%s %d_rows_offloaded>>", droppedHash, droppedCount),
	}

	var crushedArray []interface{}
	crushedArray = append(crushedArray, rawArray[:firstCount]...)
	crushedArray = append(crushedArray, sentinel)
	crushedArray = append(crushedArray, rawArray[firstCount+droppedCount:]...)

	crushedBytes, err := json.Marshal(crushedArray)
	if err != nil {
		return "", false, "passthrough"
	}

	// Store original full payload in CompressionStore
	store := GetGlobalCompressionStore()
	if store != nil {
		_, err = store.Save(droppedHash, []byte(trimmed))
		if err != nil {
			log.Printf("[SmartCrusher] failed to save dropped array: %v", err)
		} else {
			log.Printf("[SmartCrusher] saved original array (len %d bytes) under ccr key %s", len(trimmed), droppedHash)
		}
	}

	return string(crushedBytes), true, "row_drop"
}

func convertToCSV(objects []map[string]interface{}) (string, bool) {
	headerMap := make(map[string]bool)
	for _, obj := range objects {
		for k := range obj {
			headerMap[k] = true
		}
	}
	var headers []string
	for k := range headerMap {
		headers = append(headers, k)
	}
	sort.Strings(headers)

	var sb strings.Builder
	for i, h := range headers {
		sb.WriteString(escapeCSVField(h))
		if i < len(headers)-1 {
			sb.WriteString(",")
		}
	}
	sb.WriteString("\n")

	for _, obj := range objects {
		for i, h := range headers {
			val := obj[h]
			var valStr string
			if val == nil {
				valStr = ""
			} else {
				switch v := val.(type) {
				case string:
					valStr = v
				case bool:
					valStr = fmt.Sprintf("%t", v)
				case float64:
					valStr = fmt.Sprintf("%g", v)
				default:
					b, err := json.Marshal(v)
					if err == nil {
						valStr = string(b)
					}
				}
			}
			sb.WriteString(escapeCSVField(valStr))
			if i < len(headers)-1 {
				sb.WriteString(",")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String(), true
}

func escapeCSVField(f string) string {
	if strings.ContainsAny(f, ",\"\n\r") {
		return `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	return f
}

func init() {
	RegisterHook(&SmartCrusherHook{})
}
