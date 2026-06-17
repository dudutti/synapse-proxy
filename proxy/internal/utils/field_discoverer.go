package utils

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

// UsageMapping is the result of auto-discovering which JSON fields carry
// the prompt / completion / reasoning token counts for a given provider.
type UsageMapping struct {
	PromptField     string  `json:"prompt_field"`
	CompletionField string  `json:"completion_field"`
	ReasoningField  string  `json:"reasoning_field,omitempty"`
	ConfidenceScore float64 `json:"confidence_score"`
	SampleCount     int     `json:"sample_count"`
}

var (
	discoverMu    sync.RWMutex
	discoverCache = map[string]UsageMapping{} // modelID -> mapping
)

// Pattern sets used to score candidate field names. Order is meaningful
// for tie-breaking (more specific patterns win).
var (
	promptPatterns = []string{
		"prompt_tokens", "promptTokenCount", "prompt_token_count",
		"input_tokens", "inputTokenCount", "input_token_count",
		"prompt", "tokens_prompt", "request_tokens", "tokens_input",
	}
	completionPatterns = []string{
		"completion_tokens", "candidatesTokenCount", "completion_token_count",
		"output_tokens", "outputTokenCount", "output_token_count",
		"completion", "tokens_completion", "response_tokens", "tokens_output",
	}
	reasoningPatterns = []string{
		"reasoning_tokens", "reasoning_token_count", "thoughtsTokenCount",
		"thought_tokens", "thinking_tokens", "reasoning", "thoughts",
	}
)

// DiscoverUsageFields walks a list of JSON sample payloads and returns
// the most likely field paths for prompt / completion / reasoning tokens.
//
// The algorithm is intentionally simple and works on raw JSON without
// any provider-specific knowledge:
//  1. Recursively traverse each sample, recording every integer-valued
//     leaf with its dotted path.
//  2. Aggregate hits across all samples, prefer fields that appear in
//     >50% of samples, then break ties using the curated pattern list.
func DiscoverUsageFields(samples [][]byte) (UsageMapping, error) {
	if len(samples) == 0 {
		return UsageMapping{}, nil
	}

	threshold := len(samples) / 2
	if threshold == 0 {
		threshold = 1
	}

	// Collect candidates per role.
	promptHits := map[string]int{}
	completionHits := map[string]int{}
	reasoningHits := map[string]int{}
	intLeaves := map[string]int{} // path -> # samples that had an int there

	for _, raw := range samples {
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		walk("", v, func(path string, val interface{}) {
			if _, ok := val.(float64); !ok {
				return
			}
			intLeaves[path]++
		})
	}

	// Score each known pattern across paths.
	for path, hits := range intLeaves {
		if hits < threshold {
			continue
		}
		leaf := leafName(path)
		if matchAny(leaf, promptPatterns) {
			if h := promptHits[path]; hits > h {
				promptHits[path] = hits
			}
		}
		if matchAny(leaf, completionPatterns) {
			if h := completionHits[path]; hits > h {
				completionHits[path] = hits
			}
		}
		if matchAny(leaf, reasoningPatterns) {
			if h := reasoningHits[path]; hits > h {
				reasoningHits[path] = hits
			}
		}
	}

	mapping := UsageMapping{
		PromptField:     bestCandidate(promptHits, promptPatterns),
		CompletionField: bestCandidate(completionHits, completionPatterns),
		ReasoningField:  bestCandidate(reasoningHits, reasoningPatterns),
		SampleCount:     len(samples),
	}
	if mapping.PromptField != "" && mapping.CompletionField != "" {
		mapping.ConfidenceScore = float64(mapping.SampleCount) / float64(mapping.SampleCount+1)
	}
	return mapping, nil
}

func walk(path string, v interface{}, visit func(string, interface{})) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, child := range t {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			walk(childPath, child, visit)
		}
	case []interface{}:
		for i, child := range t {
			childPath := path + "[" + itoa(i) + "]"
			walk(childPath, child, visit)
		}
	default:
		visit(path, t)
	}
}

func leafName(path string) string {
	if idx := strings.LastIndexAny(path, ".[]"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func matchAny(name string, patterns []string) bool {
	lname := strings.ToLower(name)
	for _, p := range patterns {
		if lname == strings.ToLower(p) {
			return true
		}
	}
	return false
}

func bestCandidate(hits map[string]int, patterns []string) string {
	if len(hits) == 0 {
		return ""
	}
	// Pick the candidate whose leaf name matches the most specific pattern.
	// If tie, prefer the one with the most hits.
	best := ""
	bestScore := -1
	for path := range hits {
		leaf := strings.ToLower(leafName(path))
		score := 0
		for i, p := range patterns {
			if leaf == strings.ToLower(p) {
				// Earlier patterns in the list are more specific.
				score = len(patterns) - i
				break
			}
		}
		if score < 0 {
			score = 0
		}
		// Weighted score: specificity * 10 + hits
		weighted := score*10 + hits[path]
		if weighted > bestScore {
			bestScore = weighted
			best = path
		}
	}
	return best
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// discoverUsage is a per-request best-effort lookup. It checks the
// in-memory cache (keyed by modelID) first, then falls back to scanning
// the JSON for usage-like blocks. On a successful discovery it stores
// the mapping in the cache for subsequent calls.
func discoverUsage(respBytes []byte) (UsageMapping, bool) {
	// 1. Cache lookup by modelID (the caller should set this via DiscoverUsageFields
	// after a successful offline analysis, OR via SetMapping for hot-known providers).
	modelID := extractModelID(respBytes)
	if modelID != "" {
		discoverMu.RLock()
		cached, ok := discoverCache[modelID]
		discoverMu.RUnlock()
		if ok {
			return cached, true
		}
	}

	// 2. If the payload has the OpenAI/Anthropic/Google keys, the earlier
	// branches in ExtractUsage would have caught it. This branch is the
	// safety net for proprietary providers like OpenRouter, Groq, etc.
	// that nest usage under custom names.
	var generic map[string]interface{}
	if err := json.Unmarshal(respBytes, &generic); err != nil {
		return UsageMapping{}, false
	}

	candidates := findUsageBlocks(generic, "")
	if len(candidates) == 0 {
		return UsageMapping{}, false
	}

	for _, c := range candidates {
		m := buildMapping(c.leaves)
		if m.PromptField != "" && m.CompletionField != "" {
			m.SampleCount = 1
			m.ConfidenceScore = 0.5
			if modelID != "" {
				discoverMu.Lock()
				discoverCache[modelID] = m
				discoverMu.Unlock()
			}
			return m, true
		}
	}
	return UsageMapping{}, false
}

// SetMapping lets callers (e.g. the Model Radar worker) seed the cache
// with a previously-discovered mapping for a given modelID.
func SetMapping(modelID string, m UsageMapping) {
	if modelID == "" {
		return
	}
	discoverMu.Lock()
	discoverCache[modelID] = m
	discoverMu.Unlock()
}

// extractModelID pulls the top-level "model" field out of a response.
// Returns "" if absent.
func extractModelID(respBytes []byte) string {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(respBytes, &body); err == nil {
		return body.Model
	}
	return ""
}

// buildMapping is the shared logic that turns a list of integer leaf
// names into a UsageMapping. Used by both DiscoverUsageFields (offline)
// and discoverUsage (per-request).
func buildMapping(leaves []string) UsageMapping {
	m := UsageMapping{}
	for _, lf := range leaves {
		leaf := strings.ToLower(lf)
		switch {
		case matchAny(leaf, promptPatterns):
			if m.PromptField == "" {
				m.PromptField = lf
			}
		case matchAny(leaf, completionPatterns):
			if m.CompletionField == "" {
				m.CompletionField = lf
			}
		case matchAny(leaf, reasoningPatterns):
			if m.ReasoningField == "" {
				m.ReasoningField = lf
			}
		}
	}
	return m
}

// usageBlock describes a node whose integer leaves look like usage counters.
type usageBlock struct {
	path   string
	leaves []string
}

func findUsageBlocks(v interface{}, path string) []usageBlock {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	var out []usageBlock
	intLeaves := []string{}
	for k, child := range m {
		if _, isNum := child.(float64); isNum {
			intLeaves = append(intLeaves, k)
		}
	}
	// A block is "usage-like" if it has at least 2 integer leaves whose
	// names match one of the known pattern sets.
	hits := 0
	for _, l := range intLeaves {
		ll := strings.ToLower(l)
		if matchAny(ll, promptPatterns) || matchAny(ll, completionPatterns) || matchAny(ll, reasoningPatterns) {
			hits++
		}
	}
	if hits >= 1 {
		out = append(out, usageBlock{path: path, leaves: intLeaves})
	}
	for k, child := range m {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		out = append(out, findUsageBlocks(child, childPath)...)
	}
	return out
}

// applyMapping reads values from a parsed JSON object using a discovered mapping.
func applyMapping(generic map[string]interface{}, m UsageMapping) (int, int, int) {
	p := readByPath(generic, m.PromptField)
	c := readByPath(generic, m.CompletionField)
	r := 0
	if m.ReasoningField != "" {
		r = readByPath(generic, m.ReasoningField)
	}
	return p, c, r
}

func readByPath(generic map[string]interface{}, path string) int {
	if path == "" {
		return 0
	}
	// Split on dot, walk map[string]interface{}.
	cur := interface{}(generic)
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return 0
		}
		cur, ok = m[seg]
		if !ok {
			return 0
		}
	}
	return asInt(cur)
}
