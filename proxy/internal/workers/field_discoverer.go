package workers

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// UsageMapping describes the JSON paths to extract token usage from a
// provider's response. The FieldDiscoverer fills this in automatically
// from the samples accumulated by CollectSample.
type UsageMapping struct {
	PromptField     string  `json:"prompt_field"`     // e.g. "usage.prompt_tokens"
	CompletionField string  `json:"completion_field"` // e.g. "usage.completion_tokens"
	ConfidenceScore float64 `json:"confidence_score"` // 0..1
	SampleCount     int     `json:"sample_count"`
	DiscoveredAt    string  `json:"discovered_at"`
}

// usageMapKey is the Redis hash field where the mapping is stored.
// We piggy-back on the same hash structure as radar entries so the
// dashboard can read them in one pass.
const usageMapKey = "optitoken:radar:usage_mappings"

// FieldDiscoverer is a stateless analyzer: feed it raw response bodies
// (the same bytes CollectSample stored) and it returns the most likely
// (prompt_field, completion_field) pair.
//
// The algorithm is intentionally dumb-but-effective: walk the JSON
// recursively, look for integer fields whose name contains one of the
// candidate keywords, and pick the one with the highest agreement
// across samples. This handles every provider we ship (OpenAI,
// Anthropic, Google) and gracefully degrades on unknown shapes.
type FieldDiscoverer struct {
	mu sync.Mutex
}

var globalFieldDiscoverer = &FieldDiscoverer{}

// GlobalFieldDiscoverer is the process-wide instance.
func GlobalFieldDiscoverer() *FieldDiscoverer { return globalFieldDiscoverer }

// candidateKeywords is the set of substring patterns that suggest a
// field is a prompt-token or completion-token count. Order is not
// important; we score by frequency across samples.
var promptKeywords = []string{
	"prompt_token", "input_token", "promptToken", "inputToken",
	"prompt_count", "input_count",
}

var completionKeywords = []string{
	"completion_token", "output_token", "completionToken", "outputToken",
	"candidatesToken", "candidateToken", "response_token", "generated_token",
}

// fallbackKeywords are total-token fields used when no dedicated
// prompt/completion fields can be found (rare, only for exotic
// providers that bundle everything under `total_tokens`).
var fallbackKeywords = []string{"total_token", "totalToken"}

// fieldHit is a per-field tally used by walkForUsage and pickBest.
// It is declared at package scope because walkForUsage is a
// package-level function and cannot see local types.
type fieldHit struct {
	path   string
	hits   int
	values []int64
}

// DiscoverUsageFields analyses a batch of raw response bodies and
// returns the best-guess usage mapping. Returns (zero, false) if no
// confident mapping can be derived from the samples.
func (fd *FieldDiscoverer) DiscoverUsageFields(samples [][]byte) (UsageMapping, bool) {
	if len(samples) == 0 {
		return UsageMapping{}, false
	}

	promptHits := make(map[string]*fieldHit)
	completionHits := make(map[string]*fieldHit)
	fallbackHits := make(map[string]*fieldHit)

	for _, body := range samples {
		var doc interface{}
		if err := json.Unmarshal(body, &doc); err != nil {
			continue
		}
		walkForUsage(doc, "", promptKeywords, promptHits)
		walkForUsage(doc, "", completionKeywords, completionHits)
		walkForUsage(doc, "", fallbackKeywords, fallbackHits)
	}

	prompt, promptConf := pickBest(promptHits, len(samples))
	completion, compConf := pickBest(completionHits, len(samples))
	if prompt == "" && completion == "" {
		// Try the fallback path: a single `total_tokens` field.
		if fb, fbConf := pickBest(fallbackHits, len(samples)); fb != "" {
			return UsageMapping{
				PromptField:     fb,
				CompletionField: "",
				ConfidenceScore: fbConf,
				SampleCount:     len(samples),
				DiscoveredAt:    time.Now().UTC().Format(time.RFC3339),
			}, true
		}
		return UsageMapping{}, false
	}

	// If only one side is found, still return it (with lower confidence).
	if prompt == "" {
		prompt, promptConf = completion, compConf
	}
	if completion == "" {
		completion, compConf = prompt, promptConf
	}

	confidence := (promptConf + compConf) / 2
	// Penalize mappings that share a parent path (probably the same field
	// picked for both sides — a sign of bad discovery).
	if pathPrefixMatch(prompt, completion) {
		confidence *= 0.5
	}

	return UsageMapping{
		PromptField:     prompt,
		CompletionField: completion,
		ConfidenceScore: confidence,
		SampleCount:     len(samples),
		DiscoveredAt:    time.Now().UTC().Format(time.RFC3339),
	}, confidence >= 0.5
}

// walkForUsage walks a decoded JSON document recursively and records
// hits for any integer field whose name (case-insensitive substring
// match) appears in the keyword list. Result is stored as a dot-path
// (e.g. "usage.prompt_tokens") in the provided hit map.
func walkForUsage(doc interface{}, path string, keywords []string, hits map[string]*fieldHit) {
	switch v := doc.(type) {
	case map[string]interface{}:
		for k, val := range doc.(map[string]interface{}) {
			sub := k
			if path != "" {
				sub = path + "." + k
			}
			walkForUsage(val, sub, keywords, hits)
		}
	case []interface{}:
		for _, val := range v {
			walkForUsage(val, path, keywords, hits)
		}
	case float64:
		// json.Unmarshal puts every number in float64
		if v < 0 || v != float64(int64(v)) {
			return // not an integer
		}
		leafName := lastPathSegment(path)
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(leafName), strings.ToLower(kw)) {
				h, ok := hits[path]
				if !ok {
					h = &fieldHit{path: path}
					hits[path] = h
				}
				h.hits++
				h.values = append(h.values, int64(v))
				break
			}
		}
	}
}

func lastPathSegment(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// pickBest returns the field path with the highest hit ratio AND with
// non-zero values. Returns ("", 0) if no field meets the 50% threshold.
func pickBest(hits map[string]*fieldHit, sampleCount int) (string, float64) {
	if sampleCount == 0 {
		return "", 0
	}
	bestPath := ""
	bestScore := 0.0
	for path, h := range hits {
		if len(h.values) == 0 {
			continue
		}
		// Score = hit ratio * (1 if any value is > 0, else 0.3)
		hitRatio := float64(h.hits) / float64(sampleCount)
		hasNonZero := false
		for _, v := range h.values {
			if v > 0 {
				hasNonZero = true
				break
			}
		}
		zeroWeight := 0.3
		if hasNonZero {
			zeroWeight = 1.0
		}
		score := hitRatio * zeroWeight
		if score > bestScore {
			bestScore = score
			bestPath = path
		}
	}
	if bestScore < 0.5 {
		return "", 0
	}
	return bestPath, bestScore
}

// pathPrefixMatch returns true if two paths share the same parent
// (e.g. "usage.prompt_tokens" and "usage.completion_tokens") — this is
// usually fine, but if they're the SAME field picked twice for both
// sides, we want to penalize.
func pathPrefixMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	// Strip the last segment
	ap := a[:max(0, strings.LastIndex(a, "."))]
	bp := b[:max(0, strings.LastIndex(b, "."))]
	return ap != "" && ap == bp && a == b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TryDiscoverForModel runs the field discoverer on the samples
// accumulated for a given model and, if a confident mapping is found,
// persists it to Redis under the usage_mappings hash.
//
// Returns the discovered mapping and true if a new mapping was
// successfully written (or updated). False if not enough samples or
// no confident mapping could be derived.
func TryDiscoverForModel(ctx context.Context, rdb *redis.Client, modelID string) (UsageMapping, bool, error) {
	if rdb == nil || modelID == "" {
		return UsageMapping{}, false, nil
	}

	// Read the samples (we stored up to maxSamplesPerMod)
	rawSamples, err := rdb.LRange(ctx, radarSamplesKey+modelID, 0, int64(maxSamplesPerMod-1)).Result()
	if err != nil {
		return UsageMapping{}, false, err
	}
	if len(rawSamples) < 3 {
		// Need at least 3 samples to have any statistical confidence.
		return UsageMapping{}, false, nil
	}

	samples := make([][]byte, 0, len(rawSamples))
	for _, s := range rawSamples {
		samples = append(samples, []byte(s))
	}

	mapping, ok := GlobalFieldDiscoverer().DiscoverUsageFields(samples)
	if !ok {
		log.Printf("[FieldDiscoverer] %s: no confident mapping from %d samples", modelID, len(samples))
		return UsageMapping{}, false, nil
	}

	// Persist
	mappingJSON, _ := json.Marshal(mapping)
	if err := rdb.HSet(ctx, usageMapKey, modelID, string(mappingJSON)).Err(); err != nil {
		return UsageMapping{}, false, err
	}
	log.Printf("[FieldDiscoverer] %s -> prompt=%q completion=%q conf=%.2f (from %d samples)",
		modelID, mapping.PromptField, mapping.CompletionField, mapping.ConfidenceScore, mapping.SampleCount)

	// Update the radar entry to status=mapped
	entryKey := radarEntryPrefix + modelID
	raw, err := rdb.Get(ctx, entryKey).Bytes()
	if err == nil {
		var e RadarEntry
		if json.Unmarshal(raw, &e) == nil {
			e.Status = RadarStatusMapped
			e.UsageMap = mappingJSON
			e.SampleCnt = len(samples)
			updated, _ := json.Marshal(e)
			_ = rdb.Set(ctx, entryKey, updated, radarEntryTTL).Err()
		}
	}

	return mapping, true, nil
}

// GetUsageMapping returns the discovered mapping for a model, if any.
// Used by the dashboard API.
func GetUsageMapping(ctx context.Context, rdb *redis.Client, modelID string) (UsageMapping, bool, error) {
	raw, err := rdb.HGet(ctx, usageMapKey, modelID).Result()
	if err == redis.Nil {
		return UsageMapping{}, false, nil
	}
	if err != nil {
		return UsageMapping{}, false, err
	}
	var m UsageMapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return UsageMapping{}, false, err
	}
	return m, true, nil
}
