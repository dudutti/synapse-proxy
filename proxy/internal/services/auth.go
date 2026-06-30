package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"synapse-proxy/internal/db"
)

// VirtualKeyConfig holds the properties associated with an API key
type VirtualKeyConfig struct {
	VirtualKey         string
	RealKey            string
	Provider           string
	FallbackKey        string
	FallbackProvider   string
	FallbackModel      string
	IsBenchmark        bool
	SemanticTolerance  float64
	CacheTtl           int
	DefaultModel       string
	IsolateCache       bool
	ZeroLog            bool
	
	// Optimization Toggles
	EnableL1           bool
	EnableL2           bool
	EnableL3           bool
	
	// Firewall Options
	KillSwitch           bool
	FingerprintLoopDetect bool
	SessionTokenLimit    int
	AllowedTools         string
	BlockUnknownTools    bool
	RedactPII            bool
	ToolTtls             string
	
	// Tier Constraints
	LimitExceeded      bool

	// UseAnthropicEndpoint: when true AND provider is minimax,
	// the proxy forwards to /anthropic/v1/messages instead of
	// /v1/chat/completions. This unlocks Minimax's prompt cache
	// (99% cache hit on byte-stable prefixes per the vendor
	// docs). See proxy.go executeRequest and the
	// OpenAIToAnthropic translator for the conversion logic.
	UseAnthropicEndpoint bool
}

// ValidateVirtualKey checks the Authorization header and fetches the virtual key config from Redis
func ValidateVirtualKey(ctx context.Context, authHeader string) (*VirtualKeyConfig, error) {
	if !strings.HasPrefix(authHeader, "Bearer sk-opt") { // accept both sk-opti- (prod) and sk-opt... (test)
		return nil, fmt.Errorf("invalid authorization header")
	}

	virtualKey := strings.TrimPrefix(authHeader, "Bearer ")
	rdb := db.GetRedis()

	val, err := rdb.HGetAll(ctx, "synapse:keys:"+virtualKey).Result()
	if err != nil || len(val) == 0 {
		return nil, fmt.Errorf("invalid api key")
	}

	semanticTolerance := 0.15
	if st, ok := val["semantic_tolerance"]; ok {
		if f, err := strconv.ParseFloat(st, 64); err == nil {
			semanticTolerance = f
		}
	}

	cacheTtl := 86400
	if ttlStr, ok := val["cache_ttl"]; ok {
		if t, err := strconv.Atoi(ttlStr); err == nil {
			cacheTtl = t
		}
	}

	// Decrypt the real key. The dashboard stores it AES-256-GCM
	// encrypted with the shared ENCRYPTION_KEY.
	realKey, err := DecryptRealKey(val["real_key"])
	if err != nil {
		return nil, fmt.Errorf("decrypt real_key failed: %w", err)
	}
	fallbackKey, err := DecryptRealKey(val["fallback_key"])
	if err != nil && val["fallback_key"] != "" {
		return nil, fmt.Errorf("decrypt fallback_key failed: %w", err)
	}
	
	sessionTokenLimit := 0
	if limitStr, ok := val["session_token_limit"]; ok {
		if t, err := strconv.Atoi(limitStr); err == nil {
			sessionTokenLimit = t
		}
	}
	
	config := &VirtualKeyConfig{
		VirtualKey:         virtualKey,
		RealKey:            realKey,
		Provider:           val["provider"],
		FallbackKey:        fallbackKey,
		FallbackProvider:   val["fallback_provider"],
		FallbackModel:      val["fallback_model"],
		IsBenchmark:        val["benchmark_mode"] == "true",
		SemanticTolerance:  semanticTolerance,
		CacheTtl:           cacheTtl,
		DefaultModel:       val["default_model"],
		IsolateCache:       val["isolate_cache_by_user"] == "true",
		ZeroLog:            val["zero_log"] == "true",
		
		EnableL1:           val["enable_l1"] != "false", // Default to true if missing
		EnableL2:           val["enable_l2"] != "false",
		EnableL3:           val["enable_l3"] != "false",
		
		KillSwitch:           val["kill_switch"] == "true",
		FingerprintLoopDetect: val["fingerprint_loop_detect"] == "true",
		SessionTokenLimit:    sessionTokenLimit,
		AllowedTools:         val["allowed_tools"],
		BlockUnknownTools:    val["block_unknown_tools"] == "true",
		RedactPII:            val["redact_pii"] == "true",
		ToolTtls:             val["tool_ttls"],
		LimitExceeded:      val["limit_exceeded"] == "true",

		UseAnthropicEndpoint: val["use_anthropic_endpoint"] == "true",
	}

	return config, nil
}

// LookupSessionTag returns the active session id for a virtual key,
// or "" if no session is recording. The dashboard writes the key
// `synapse:session:vk:<virtualKey>` to Redis when the user
// clicks "Record Session" and removes it on stop. The proxy
// reads it on every request and, if non-empty, tags the resulting
// RequestLog row with the same session id. This lets the user
// record a session without having to touch the agent (Hermes,
// curl, anything) â€” the proxy does the tagging transparently.
func LookupSessionTag(ctx context.Context, virtualKey string) string {
	rdb := db.GetRedis()
	v, err := rdb.Get(ctx, "synapse:session:vk:"+virtualKey).Result()
	if err != nil {
		// redis.Nil is the expected case when no session is
		// active. Any other error (connection, etc.) is also
		// non-fatal: the request proceeds without a session
		// tag rather than 500ing.
		return ""
	}
	return v
}
