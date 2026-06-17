package utils

import "optitoken/internal/db"

// SavingsBreakdown decomposes the cost saved per token class.
// Total = InputFreshSaved + CacheReadSaved + CacheCreationSaved + OutputSaved.
//
// Use Total() for a single number, or expose the breakdown in the dashboard
// so users see exactly where the savings come from.
type SavingsBreakdown struct {
	InputFreshSaved    float64
	CacheReadSaved     float64
	CacheCreationSaved float64
	OutputSaved        float64
}

func (s SavingsBreakdown) Total() float64 {
	return s.InputFreshSaved + s.CacheReadSaved + s.CacheCreationSaved + s.OutputSaved
}

// CalculateSavings returns the estimated cost saved based on distinct prompt and
// completion tokens saved. It is the legacy single-number version, kept for
// backward compatibility (dashboard headline, cost-saved chart).
// Use CalculateSavingsByClass for the per-class breakdown.
func CalculateSavings(provider, model string, promptTokensSaved, completionTokensSaved int) float64 {
	if promptTokensSaved <= 0 && completionTokensSaved <= 0 {
		return 0
	}

	pricing := db.GetModelPricing(provider, model)

	var savings float64 = 0
	if promptTokensSaved > 0 {
		savings += float64(promptTokensSaved) * (pricing.CostPrompt1M / 1000000.0)
	}
	if completionTokensSaved > 0 {
		savings += float64(completionTokensSaved) * (pricing.CostCompletion1M / 1000000.0)
	}

	return savings
}

// CalculateSavingsByClass returns a 4-class breakdown of cost saved.
//
// Inputs:
//   - promptTokensSaved: total input tokens saved (promptOrig - promptOpt)
//   - completionTokensSaved: total output tokens saved
//   - cacheReadTokensSaved: tokens in cache_read on the original request (Anthropic
//     cache_read_input_tokens or OpenAI cached_tokens). These were billed at
//     costCachedInput1M instead of costPrompt1M.
//   - cacheCreationTokensSaved: tokens in cache_creation on the original request
//     (Anthropic cache_creation_input_tokens). These were billed at
//     costCacheWrite1M (often HIGHER than costPrompt1M, e.g. 1.25x on Anthropic 5m).
//
// Allocation rule: promptTokensSaved = freshSaved + cacheReadSaved + cacheCreationSaved.
// If the worker passes cacheRead/creation tokens that exceed promptTokensSaved,
// the remainder is treated as fresh input (defensive).
func CalculateSavingsByClass(
	provider, model string,
	promptTokensSaved, completionTokensSaved, cacheReadTokensSaved, cacheCreationTokensSaved int,
) SavingsBreakdown {
	if promptTokensSaved <= 0 && completionTokensSaved <= 0 &&
		cacheReadTokensSaved <= 0 && cacheCreationTokensSaved <= 0 {
		return SavingsBreakdown{}
	}

	pricing := db.GetModelPricing(provider, model)

	var b SavingsBreakdown

	// 1. Output saved
	if completionTokensSaved > 0 {
		b.OutputSaved = float64(completionTokensSaved) * (pricing.CostCompletion1M / 1000000.0)
	}

	// 2. Cache_creation: typically MORE expensive than input. If L3 reduced the prefix
	//    written to cache, the saving is (inputRate - writeRate) per token — can be
	//    NEGATIVE if write > input (the case for Anthropic 5m and 1h).
	if cacheCreationTokensSaved > 0 {
		writeRate := pricing.CostCacheWrite1M
		if writeRate == 0 {
			writeRate = pricing.CostPrompt1M * 1.25 // Anthropic 5m default
		}
		delta := (pricing.CostPrompt1M - writeRate) / 1000000.0
		b.CacheCreationSaved = float64(cacheCreationTokensSaved) * delta
	}

	// 3. Cache_read: cheaper than fresh. Saving = (input - cacheRead) per token.
	if cacheReadTokensSaved > 0 {
		cacheReadRate := pricing.CostCachedInput1M
		if cacheReadRate == 0 {
			cacheReadRate = pricing.CostPrompt1M * 0.1 // Anthropic/OpenAI default
		}
		delta := (pricing.CostPrompt1M - cacheReadRate) / 1000000.0
		b.CacheReadSaved = float64(cacheReadTokensSaved) * delta
	}

	// 4. Fresh input: remainder of promptTokensSaved.
	freshSaved := promptTokensSaved - cacheReadTokensSaved - cacheCreationTokensSaved
	if freshSaved < 0 {
		freshSaved = 0
	}
	if freshSaved > 0 {
		b.InputFreshSaved = float64(freshSaved) * (pricing.CostPrompt1M / 1000000.0)
	}

	return b
}
