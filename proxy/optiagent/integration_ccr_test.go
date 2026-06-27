// Integration test: does the ccr_cache_key feature
// flow from LogCompressor to CCRToolInjection?
//
// If it doesn't, P1.1's tool injection is dead code in
// the prod pipeline. We need to know.

package optiagent

import (
	"context"
	"strings"
	"testing"
)

func TestIntegration_CCRCacheKeyFlow(t *testing.T) {
	// Build a payload that LogCompressor should compress
	// (structured log with repeated WARN lines).
	logContent := strings.Repeat("WARN: timeout fetching /api/test 1\n", 30)
	payload := []byte(`{"messages":[{"role":"tool","content":"` + logContent + `"}]}`)

	hctx := &HookContext{
		VK:               "vk-int",
		RawPayload:       payload,
		OptimizedPayload: payload,
		Features:         map[string]interface{}{},
	}

	// Run LogCompressor first.
	lc := &LogCompressorHook{}
	out, err := lc.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("LogCompressor error: %v", err)
	}
	if out == nil {
		t.Fatal("LogCompressor returned nil")
	}
	t.Logf("LogCompressor returned %d bytes (orig %d)", len(out), len(payload))
	t.Logf("LogCompressor output: %s", string(out))

	// Check if ccr_cache_key was set.
	cacheKey, _ := hctx.GetFeature("ccr_cache_key").(string)
	if cacheKey == "" {
		t.Fatal("LogCompressor did NOT set ccr_cache_key (this is the bug)")
	}
	t.Logf("ccr_cache_key = %s", cacheKey)

	// Now run CCRToolInjection. It should see the cache_key
	// and inject the tool.
	hctx.OptimizedPayload = out
	inj := &CCRToolInjectionHook{}
	out2, err := inj.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("CCRToolInjection error: %v", err)
	}
	if out2 == nil || string(out2) == string(out) {
		t.Fatal("CCRToolInjection did NOT modify payload (no tool injected)")
	}
	t.Logf("CCRToolInjection output: %s", string(out2)[:200])
	if !strings.Contains(string(out2), SynapseRetrieveToolName) {
		t.Fatalf("synapse_retrieve not found in output")
	}
}