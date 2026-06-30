// Package compress — Hook pipeline for the local-client.
//
// The local-client doesn't have the full optiagent hook registry;
// instead, we run a small ordered set of transformations on
// every chat-completion request and response. The order matches
// what the server's optiagent does so the local and SaaS
// behavior stay aligned.
//
// Pipeline order (BeforeRequest):
//   1. CompressBytePreserving — L3 trim, byte-stable prefix
//   2. AnthropicEndpoint — translate OpenAI → Anthropic shape
//                                when targeting an Anthropic-
//                                compatible upstream
//
// Pipeline order (AfterResponse):
//   1. AnthropicToOpenAI — translate Anthropic response back
//                          to OpenAI shape so callers don't have
//                          to know
//
// The pipeline returns the new payload and a metadata map
// (per-hook savings, features observed) that the proxy uses
// for local analytics.
package compress

import (
	"log"
)

// RunBefore applies the BeforeRequest pipeline to payload.
// Returns the new payload and a metadata map.
func RunBefore(payload []byte, provider, defaultModel string) (out []byte, meta map[string]interface{}) {
	meta = map[string]interface{}{}
	out = payload

	// Step 1: byte-preserving L3 compression.
	compressed, saved := CompressBytePreserving(out)
	if saved > 0 {
		meta["byte_preserving_compressor_bytes_saved"] = saved
		meta["byte_preserving_compressor_orig_bytes"] = len(out)
		meta["byte_preserving_compressor_opt_bytes"] = len(compressed)
	}
	out = compressed

	// Step 2: translate to Anthropic shape if the upstream
	// supports it. The provider is the same string the
	// X-Synapse-Provider header carries, lowercased.
	if isAnthropicCompatible(provider) {
		translated, err := OpenAIToAnthropic(out, defaultModel)
		if err == nil {
			meta["anthropic_endpoint_translated"] = true
			meta["anthropic_endpoint_model_remap"] = defaultModel
			out = translated
			log.Printf("[pipeline] translated OpenAI->Anthropic for provider=%s (model=%s, %d->%d bytes)",
				provider, defaultModel, len(compressed), len(translated))
		} else {
			meta["anthropic_endpoint_translation_error"] = err.Error()
			log.Printf("[pipeline] OpenAI->Anthropic translation failed: %v", err)
		}
	}
	return out, meta
}

// RunAfter applies the AfterResponse pipeline to a response
// body. When the response came from an Anthropic endpoint we
// translate it back to OpenAI shape.
func RunAfter(body []byte, provider, defaultModel string) []byte {
	if !isAnthropicCompatible(provider) {
		return body
	}
	translated, err := AnthropicToOpenAI(body, 0, defaultModel)
	if err != nil {
		return body
	}
	return translated
}

// isAnthropicCompatible returns true for the providers we route
// to /v1/messages (Anthropic, OpenAI's Anthropic-compat
// endpoint, Minimax, DeepSeek, etc.).
func isAnthropicCompatible(provider string) bool {
	switch provider {
	case "anthropic", "claude",
		"minimax", "minimax-anthropic",
		"deepseek",
		"bedrock",
		"vertex", "google", "gemini":
		return true
	default:
		return false
	}
}

// ProviderUsesAnthropicShape is the public version of
// isAnthropicCompatible, exposed so the proxy handler can pick
// the right upstream URL and headers.
func ProviderUsesAnthropicShape(provider string) bool {
	return isAnthropicCompatible(provider)
}