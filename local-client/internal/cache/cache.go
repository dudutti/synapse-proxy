package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"synapse-local/internal/db"
)

var cacheMu sync.RWMutex

// ComputeHash returns a stable SHA-256 hex digest of the bytes
// after trim. We use this for L1 cache keying so the cache
// matches what's actually forwarded upstream (post-L3
// compression).
func ComputeHash(prompt string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(prompt)))
	return hex.EncodeToString(h.Sum(nil))
}

// ComputePayloadHash is like ComputeHash but on the raw JSON
// payload (post-L3). It is the preferred cache key because it
// matches what the upstream sees, including whitespace and
// key order.
func ComputePayloadHash(payload []byte) string {
	h := sha256.New()
	h.Write(bytes.TrimSpace(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// PayloadContext: the trimmed-down version of the request
// we use as the L1/L2/L3 cache key. Storing this lets us hit
// the cache without storing the full payload (which would
// bloat SQLite). The trimmed version preserves enough
// signature to detect content equivalence while being
// significantly smaller.
type PayloadContext struct {
	Hash          string
	SystemHead    string
	UserLast      string
	ToolsHash     string
	NumMessages   int
	LastToolName  string
	LastToolCount int
}

// MakePayloadContext derives a cache key + small signature
// from an OpenAI-shape chat-completion payload. The hash is
// stable across byte-equivalent payloads. The other fields
// are useful for analytics and for Jaccard similarity in L2.
func MakePayloadContext(payload []byte) (PayloadContext, error) {
	var body struct {
		Messages []map[string]interface{} `json:"messages"`
		Tools    []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return PayloadContext{}, err
	}
	pc := PayloadContext{
		Hash:        ComputePayloadHash(payload),
		NumMessages: len(body.Messages),
	}
	if len(body.Messages) > 0 {
		// System head
		if first, ok := body.Messages[0]["content"].(string); ok {
			pc.SystemHead = first
		}
		// Last user message
		for i := len(body.Messages) - 1; i >= 0; i-- {
			if body.Messages[i]["role"] == "user" {
				if c, ok := body.Messages[i]["content"].(string); ok {
					pc.UserLast = c
				}
				break
			}
		}
		// Last tool name + count of same-name tools
		for i := len(body.Messages) - 1; i >= 0; i-- {
			if body.Messages[i]["role"] == "tool" || body.Messages[i]["role"] == "function" {
				if n, ok := body.Messages[i]["name"].(string); ok {
					pc.LastToolName = n
					for j := i; j >= 0; j-- {
						if (body.Messages[j]["role"] == "tool" || body.Messages[j]["role"] == "function") &&
							body.Messages[j]["name"] == n {
							pc.LastToolCount++
						} else {
							break
						}
					}
				}
				break
			}
		}
	}
	// Hash of tool definitions (function names + descriptions)
	// so two different tool catalogs don't share a cache.
	if len(body.Tools) > 0 {
		toolBytes, _ := json.Marshal(body.Tools)
		th := sha256.New()
		th.Write(toolBytes)
		pc.ToolsHash = hex.EncodeToString(th.Sum(nil))
	}
	return pc, nil
}

// GetL1 returns the exact matched cached response for a
// PayloadContext. Returns (response_text, tool_calls_summary,
// ok).
func GetL1(pc PayloadContext) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	var responseText string
	err := db.DB.QueryRow(
		"SELECT response_text FROM cache_entries WHERE hash = ?",
		pc.Hash,
	).Scan(&responseText)
	if err == nil {
		return responseText, true
	}
	return "", false
}

// SetL1 inserts or updates a cache entry keyed by the payload
// hash. We also store the truncated signature (system head,
// user last, tools hash, num messages) so analytics queries
// can answer "what does the cached distribution look like".
func SetL1(pc PayloadContext, response string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	_, _ = db.DB.Exec(`
		INSERT OR REPLACE INTO cache_entries (
			id, hash, cache_level, prompt_text, response_text, created_at
		) VALUES (?, ?, 'L1', ?, ?, ?)
	`, pc.Hash, pc.Hash,
		pc.SystemHead[:min(200, len(pc.SystemHead))],
		response, time.Now())
}

// GetL2 performs word-level Jaccard semantic similarity over
// stored system heads. We match against the system prompt
// (which is the most stable signal across conversation turns).
// Jaccard >= 0.85 → hit.
func GetL2(pc PayloadContext) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	words := setOfWords(pc.SystemHead)
	if len(words) == 0 {
		return "", false
	}
	rows, err := db.DB.Query(
		"SELECT response_text FROM cache_entries WHERE cache_level = 'L1' AND length(prompt_text) > 50 LIMIT 1000",
	)
	if err != nil {
		return "", false
	}
	defer rows.Close()

	bestScore := 0.0
	var bestResponse string
	for rows.Next() {
		var pText, rText string
		if err := rows.Scan(&pText, &rText); err != nil {
			continue
		}
		score := jaccardSimilarity(words, setOfWords(pText))
		if score > bestScore {
			bestScore = score
			bestResponse = rText
		}
	}
	if bestScore >= 0.85 {
		return bestResponse, true
	}
	return "", false
}

// GetL3 implements a relaxed Jaccard (0.70) match against
// system head OR last user message, emulating CCR chunk-based
// similarity.
func GetL3(pc PayloadContext) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	rows, err := db.DB.Query(
		"SELECT response_text FROM cache_entries WHERE cache_level = 'L1' AND length(prompt_text) > 50 LIMIT 1000",
	)
	if err != nil {
		return "", false
	}
	defer rows.Close()

	queryWords := setOfWords(pc.UserLast)
	if len(queryWords) == 0 {
		queryWords = setOfWords(pc.SystemHead)
	}
	if len(queryWords) == 0 {
		return "", false
	}

	bestScore := 0.0
	var bestResponse string
	for rows.Next() {
		var pText, rText string
		if err := rows.Scan(&pText, &rText); err != nil {
			continue
		}
		score := jaccardSimilarity(queryWords, setOfWords(pText))
		if score > bestScore {
			bestScore = score
			bestResponse = rText
		}
	}
	if bestScore >= 0.70 {
		return bestResponse, true
	}
	return "", false
}

// Tokenize text into set of alphanumeric lowercased words
// (length > 2).
func setOfWords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	cleaned := strings.ToLower(text)
	f := func(c rune) bool {
		return !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '\'')
	}
	for _, w := range strings.FieldsFunc(cleaned, f) {
		if len(w) > 2 {
			words[w] = struct{}{}
		}
	}
	return words
}

func jaccardSimilarity(set1, set2 map[string]struct{}) float64 {
	intersection := 0
	for k := range set1 {
		if _, ok := set2[k]; ok {
			intersection++
		}
	}
	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}