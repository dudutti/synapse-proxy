package db

import (
	"log"
	"strings"
	"sync"
	"time"
)

// ModelPricing holds per-token-class pricing for one provider+model combination.
// CostCachedInput1M and CostCacheWrite1M are optional (nullable in DB): when zero,
// the caller should fall back to a sensible default (0.1x input for cache_read,
// 1.25x input for cache_write 5m on Anthropic, etc.).
type ModelPricing struct {
	Provider          string
	ModelName         string
	CostPrompt1M      float64
	CostCompletion1M  float64
	CostCachedInput1M float64 // 0.1x input for Anthropic/OpenAI/Google, 0.02x for DeepSeek
	CostCacheWrite1M  float64 // 1.25x input for Anthropic 5m, 2x for 1h, =prompt for OpenAI/DeepSeek
}

var (
	pricingCache map[string]ModelPricing
	pricingMutex sync.RWMutex
)

// InitPricingSyncer starts a background worker that fetches prices from Postgres every hour
func InitPricingSyncer() {
	pricingCache = make(map[string]ModelPricing)

	// Initial fetch
	syncPricing()

	// Start hourly ticker
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			syncPricing()
		}
	}()
}

func syncPricing() {
	if dbClient == nil {
		return
	}

	rows, err := dbClient.Query(`
		SELECT provider, "modelName",
			COALESCE("costPromptPer1M", 1.0),
			COALESCE("costCompletionPer1M", 1.0),
			COALESCE("costCachedInputPer1M", 0),
			COALESCE("costCacheWritePer1M", 0)
		FROM "ProviderModel"`)
	if err != nil {
		log.Printf("PricingSyncer: Error querying ProviderModel: %v", err)
		return
	}
	defer rows.Close()

	newCache := make(map[string]ModelPricing)
	for rows.Next() {
		var p ModelPricing
		if err := rows.Scan(
			&p.Provider,
			&p.ModelName,
			&p.CostPrompt1M,
			&p.CostCompletion1M,
			&p.CostCachedInput1M,
			&p.CostCacheWrite1M,
		); err == nil {
			key := strings.ToLower(p.Provider + ":" + p.ModelName)
			newCache[key] = p
		}
	}

	pricingMutex.Lock()
	pricingCache = newCache
	pricingMutex.Unlock()

	log.Printf("PricingSyncer: Successfully synced %d models from database.", len(newCache))
}

// GetModelPricing returns the pricing for a given provider and model.
// If not found, it returns generic defaults (1.00 per 1M for input/output).
func GetModelPricing(provider, model string) ModelPricing {
	pricingMutex.RLock()
	defer pricingMutex.RUnlock()

	key := strings.ToLower(provider + ":" + model)
	if p, exists := pricingCache[key]; exists {
		return p
	}

	// Fallback generic pricing
	return ModelPricing{
		Provider:          provider,
		ModelName:         model,
		CostPrompt1M:      1.0,
		CostCompletion1M:  1.0,
		CostCachedInput1M: 0,
		CostCacheWrite1M:  0,
	}
}
