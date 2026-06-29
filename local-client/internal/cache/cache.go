package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"synapse-local/internal/db"
)

var cacheMu sync.RWMutex

// Helper to compute prompt SHA-256 hash
func ComputeHash(prompt string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(prompt)))
	return hex.EncodeToString(h.Sum(nil))
}

// GetL1 returns the exact matched cached response by prompt hash
func GetL1(prompt string) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	hash := ComputeHash(prompt)
	var responseText string
	err := db.DB.QueryRow("SELECT response_text FROM cache_entries WHERE hash = ?", hash).Scan(&responseText)
	if err == nil {
		return responseText, true
	}
	return "", false
}

// SetL1 inserts or updates a cache entry
func SetL1(prompt, response string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	hash := ComputeHash(prompt)
	id := hash // Use hash as ID
	_, _ = db.DB.Exec(`
		INSERT OR REPLACE INTO cache_entries (id, hash, cache_level, prompt_text, response_text, created_at)
		VALUES (?, ?, 'L1', ?, ?, ?)
	`, id, hash, prompt, response, time.Now())
}

// GetL2 performs word-level Jaccard semantic similarity over stored prompts
func GetL2(prompt string) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	// Get all cached entries
	rows, err := db.DB.Query("SELECT prompt_text, response_text FROM cache_entries LIMIT 1000")
	if err != nil {
		return "", false
	}
	defer rows.Close()

	words1 := setOfWords(prompt)
	if len(words1) == 0 {
		return "", false
	}

	bestScore := 0.0
	var bestResponse string

	for rows.Next() {
		var pText, rText string
		if err := rows.Scan(&pText, &rText); err != nil {
			continue
		}

		words2 := setOfWords(pText)
		score := jaccardSimilarity(words1, words2)
		if score > bestScore {
			bestScore = score
			bestResponse = rText
		}
	}

	// Jaccard threshold at 85% similarity
	if bestScore >= 0.85 {
		return bestResponse, true
	}

	return "", false
}

// GetL3 implements a sentence/chunk-based similarity matching
func GetL3(prompt string) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	// Simple chunk registry check: if a prompt is at least 80% composed of sentences 
	// that we have seen before, we can construct the output.
	// For local mode, we fallback to a relaxed Jaccard score (e.g. 70%) to emulate CCR hit.
	rows, err := db.DB.Query("SELECT prompt_text, response_text FROM cache_entries LIMIT 1000")
	if err != nil {
		return "", false
	}
	defer rows.Close()

	words1 := setOfWords(prompt)
	if len(words1) == 0 {
		return "", false
	}

	bestScore := 0.0
	var bestResponse string

	for rows.Next() {
		var pText, rText string
		if err := rows.Scan(&pText, &rText); err != nil {
			continue
		}

		words2 := setOfWords(pText)
		score := jaccardSimilarity(words1, words2)
		if score > bestScore {
			bestScore = score
			bestResponse = rText
		}
	}

	// L3 threshold at 70%
	if bestScore >= 0.70 {
		return bestResponse, true
	}

	return "", false
}

// Tokenize text into set of alphanumeric lowercased words
func setOfWords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	cleaned := strings.ToLower(text)
	// Simple word extraction
	f := func(c rune) bool {
		return !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '\'')
	}
	fields := strings.FieldsFunc(cleaned, f)
	for _, w := range fields {
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
