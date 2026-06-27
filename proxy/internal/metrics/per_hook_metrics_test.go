// Tests for the byte counters (network savings).
// P1.5 DASHBOARD FIRST: bytes saved is a secondary
// metric (the primary is tokens) but it's still useful
// for showing network bandwidth savings on the
// dashboard. This test verifies that:
//   - LogCompressor records tokens saved
//   - CCR cache hits record bytes saved (the size of
//     the cached response that was avoided)

package metrics

import (
	"bytes"
	"strings"
	"testing"
)

// TestMetrics_LogCompressorTokensCounterIsExposed: when
// RecordLogCompressorTokens is called with N tokens,
// the /metrics endpoint must include
// "synapse_log_compressor_tokens_saved_total N".
func TestMetrics_LogCompressorTokensCounterIsExposed(t *testing.T) {
	ResetForTest()
	RecordLogCompressorTokens(500)
	var buf bytes.Buffer
	WritePrometheus(&buf)
	out := buf.String()
	if !strings.Contains(out, "synapse_log_compressor_tokens_saved_total 500") {
		t.Fatalf("counter not exposed\n  output: %s", out)
	}
}

// TestMetrics_CCRCacheHitBytesCounterIsExposed: when
// RecordCCRCacheHitBytes is called with N bytes saved
// (the size of the cached response that was served
// from cache instead of upstream), the /metrics
// endpoint must include the bytes saved with a kind
// label.
func TestMetrics_CCRCacheHitBytesCounterIsExposed(t *testing.T) {
	ResetForTest()
	RecordCCRCacheHitBytes("retrieve", 8192)
	var buf bytes.Buffer
	WritePrometheus(&buf)
	out := buf.String()
	if !strings.Contains(out, `synapse_ccr_cache_bytes_saved_total{kind="retrieve"} 8192`) {
		t.Fatalf("counter not exposed\n  output: %s", out)
	}
}
