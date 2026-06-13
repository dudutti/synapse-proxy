package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"optitoken/internal/db"
)

// ValidateVirtualKey checks the Authorization header and fetches the virtual key config from Redis
func ValidateVirtualKey(ctx context.Context, authHeader string) (string, string, string, string, string, bool, float64, int, string, bool, error) {
	if !strings.HasPrefix(authHeader, "Bearer sk-opti-") {
		return "", "", "", "", "", false, 0, 0, "", false, fmt.Errorf("invalid authorization header")
	}

	virtualKey := strings.TrimPrefix(authHeader, "Bearer ")
	rdb := db.GetRedis()
	
	val, err := rdb.HGetAll(ctx, "optitoken:keys:"+virtualKey).Result()
	if err != nil || len(val) == 0 {
		return "", "", "", "", "", false, 0, 0, "", false, fmt.Errorf("invalid api key")
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
	defaultModel := val["default_model"] // Empty string if not set

	return virtualKey, val["real_key"], val["provider"], val["fallback_key"], val["fallback_provider"], isBenchmark, semanticTolerance, cacheTtl, defaultModel, isolateCache, nil
}
