package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"optitoken/internal/db"
)

// ValidateVirtualKey checks the Authorization header and fetches the virtual key config from Redis
func ValidateVirtualKey(ctx context.Context, authHeader string) (string, string, string, string, string, string, bool, float64, int, string, bool, bool, error) {
	if !strings.HasPrefix(authHeader, "Bearer sk-opti-") {
		return "", "", "", "", "", "", false, 0, 0, "", false, false, fmt.Errorf("invalid authorization header")
	}

	virtualKey := strings.TrimPrefix(authHeader, "Bearer ")
	rdb := db.GetRedis()

	val, err := rdb.HGetAll(ctx, "optitoken:keys:"+virtualKey).Result()
	if err != nil || len(val) == 0 {
		return "", "", "", "", "", "", false, 0, 0, "", false, false, fmt.Errorf("invalid api key")
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

	isBenchmark := val["benchmark_mode"] == "true"
	isolateCache := val["isolate_cache_by_user"] == "true"
	zeroLog := val["zero_log"] == "true"
	defaultModel := val["default_model"] // Empty string if not set
	fallbackModel := val["fallback_model"] // Empty string if not set

	// Decrypt the real key. The dashboard stores it AES-256-GCM
	// encrypted with the shared ENCRYPTION_KEY. Legacy keys seeded
	// before encryption was enabled are detected by DecryptRealKey
	// (which falls back to plaintext on GCM-open failure).
	realKey, err := DecryptRealKey(val["real_key"])
	if err != nil {
		return "", "", "", "", "", "", false, 0, 0, "", false, false, fmt.Errorf("decrypt real_key failed: %w", err)
	}
	fallbackKey, err := DecryptRealKey(val["fallback_key"])
	if err != nil && val["fallback_key"] != "" {
		return "", "", "", "", "", "", false, 0, 0, "", false, false, fmt.Errorf("decrypt fallback_key failed: %w", err)
	}

	return virtualKey, realKey, val["provider"], fallbackKey, val["fallback_provider"], fallbackModel, isBenchmark, semanticTolerance, cacheTtl, defaultModel, isolateCache, zeroLog, nil
}
