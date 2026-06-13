package utils

// CalculateSavings returns the estimated cost saved based on token reduction
func CalculateSavings(provider string, orig, opt int) float64 {
	savedTokens := orig - opt
	if savedTokens <= 0 {
		return 0
	}
	// Mock pricing table: ~$1.00 per 1M tokens average across major providers
	return float64(savedTokens) * 1.0 / 1000000.0
}
