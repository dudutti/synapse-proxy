package utils

// ModelsForProvider returns the set of model IDs advertised by a given
// provider. Used by the proxy to decide whether a client-requested
// model can be forwarded as-is, or whether the request should be
// silently aliased to the key's defaultModel.
//
// The set is intentionally permissive: if a model is missing from the
// table but happens to be served by the provider, the proxy will still
// try to forward it. The table is only used to *detect* obviously-wrong
// requests (e.g. a Gemma model sent to a MiniMax-backed key) and alias
// them to the default.
func ModelsForProvider(provider string) map[string]struct{} {
	switch provider {
	case "minimax":
		return setOf([]string{
			"MiniMax-M1",
			"MiniMax-M2",
			"MiniMax-M2.1",
			"MiniMax-M3",
			"MiniMax-M2-hermes",
		})
	case "anthropic":
		return setOf([]string{
			"claude-3-5-sonnet-20241022",
			"claude-3-5-sonnet-20240620",
			"claude-3-5-haiku-20241022",
			"claude-3-opus-20240229",
			"claude-3-haiku-20240307",
			"claude-sonnet-4-20250514",
			"claude-opus-4-20250514",
		})
	case "openai":
		return setOf([]string{
			"gpt-4o",
			"gpt-4o-mini",
			"gpt-4-turbo",
			"gpt-3.5-turbo",
			"o1-preview",
			"o1-mini",
		})
	case "google":
		return setOf([]string{
			"gemini-2.5-flash",
			"gemini-2.5-pro",
			"gemini-1.5-pro",
			"gemini-1.5-flash",
		})
	case "openrouter":
		// OpenRouter proxies anything, so the table is empty → no
		// aliasing happens, all model names pass through.
		return setOf([]string{})
	case "groq":
		return setOf([]string{
			"llama-3.1-70b-versatile",
			"llama-3.1-8b-instant",
			"mixtral-8x7b-32768",
		})
	case "deepseek":
		return setOf([]string{
			"deepseek-chat",
			"deepseek-reasoner",
		})
	case "mistral":
		return setOf([]string{
			"mistral-large-latest",
			"mistral-medium-latest",
			"mistral-small-latest",
		})
	default:
		// Unknown provider → trust the client
		return setOf([]string{})
	}
}

func setOf(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, x := range items {
		m[x] = struct{}{}
	}
	return m
}
