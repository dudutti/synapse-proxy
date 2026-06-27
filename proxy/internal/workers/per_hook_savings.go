
package workers

import (
	"encoding/json"
	"log"

	"synapse-proxy/optiagent"
)

// buildPerHookSavingsJSON extracts the per-hook savings
// from hctx.Features and returns them as a JSON string
// suitable for the perHookSavings DB column.
//
// The JSON shape is:
// {
//   "logCompressor":     {"bytesSaved": N, "tokensSaved": N},
//   "outputReducer":     {"bytesSaved": N, "tokensSaved": N},
//   "ccrCache":          {"hits": N, "bytesSaved": N},
//   "tagProtector":      {"zones": N},
//   "synapseRetrieve":   {"toolsInjected": N}
// }
//
// This is the single source of truth for the per-hook
// metrics. The dashboard parses this JSON.
func BuildPerHookSavingsJSON(hctx *optiagent.HookContext) string {
	if hctx == nil {
		return "{}"
	}
	savings := map[string]interface{}{}
	// logCompressor
	lcBytes, _ := hctx.GetFeature("log_compressor_savings").(int)
	savings["logCompressor"] = map[string]interface{}{
		"bytesSaved": lcBytes,
		// tokensSaved is set by the metrics package, not
		// in hctx.Features. The dashboard reads tokens
		// from /metrics. We can add a hctx feature
		// for tokens if needed.
	}
	// ccrCache
	if hit, ok := hctx.GetFeature("ccr_cache_hit").(string); ok && hit != "" {
		ccr := map[string]interface{}{"hits": 1}
		savings["ccrCache"] = ccr
	}
	// synapseRetrieve
	if key, ok := hctx.GetFeature("ccr_cache_key").(string); ok && key != "" {
		savings["synapseRetrieve"] = map[string]interface{}{"toolsInjected": 1}
	}
	// tagProtector
	if zones, ok := hctx.GetFeature("tag_protector_zones").([]optiagent.TagZone); ok {
		savings["tagProtector"] = map[string]interface{}{"zones": len(zones)}
	}
	b, err := json.Marshal(savings)
	if err != nil {
		log.Printf("buildPerHookSavingsJSON: marshal failed: %v", err)
		return "{}"
	}
	return string(b)
}
