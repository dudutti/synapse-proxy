package utils

import "optitoken/internal/db"

// CalculateSavings returns the estimated cost saved based on distinct prompt and completion tokens saved
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
