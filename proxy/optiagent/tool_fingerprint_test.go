package optiagent

import (
	"context"
	"testing"
	"time"

	"synapse-proxy/internal/db"
)

func TestShouldReuseCache_NoTools(t *testing.T) {
	// Empty payload / no tools called
	payload := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	
	// ShouldReuseCache returns true immediately for requests with no tools without contacting Redis.
	res := ShouldReuseCache(context.Background(), nil, payload, "key", 86400, "")
	if !res {
		t.Error("expected ShouldReuseCache to return true for request with no tools")
	}
}

func TestShouldReuseCache_CustomTtls(t *testing.T) {
	db.InitRedis()
	rdb := db.GetRedis()
	if rdb == nil {
		t.Skip("Redis is not available, skipping Redis-dependent tests")
		return
	}
	ctx := context.Background()

	// 1. Tool with custom TTL = 0 (disabled caching)
	payload := []byte(`{
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [{"id":"c1","type":"function","function":{"name":"write_file","arguments":"{}"}}]
			}
		]
	}`)
	
	res0 := ShouldReuseCache(ctx, rdb, payload, "synapse:l1cache:testkey", 86400, `{"write_file":0}`)
	if res0 {
		t.Error("expected false for TTL 0")
	}

	// 2. Custom TTL set to 300s. Set key in redis with 86400 - 10s = 86390s remaining (age = 10s)
	cacheKey := "synapse:l1cache:testkey_custom"
	rdb.Set(ctx, cacheKey, "dummy_value", 86390*time.Second)
	defer rdb.Del(ctx, cacheKey)

	res300 := ShouldReuseCache(ctx, rdb, payload, cacheKey, 86400, `{"write_file":300}`)
	if !res300 {
		t.Error("expected true for age 10s and TTL 300")
	}

	// Set key in redis with 86400 - 310s = 86090s remaining (age = 310s)
	rdb.Set(ctx, cacheKey, "dummy_value", 86090*time.Second)
	resStale := ShouldReuseCache(ctx, rdb, payload, cacheKey, 86400, `{"write_file":300}`)
	if resStale {
		t.Error("expected false for age 310s and TTL 300")
	}
}

func TestShouldReuseCache_FallbackRules(t *testing.T) {
	db.InitRedis()
	rdb := db.GetRedis()
	if rdb == nil {
		t.Skip("Redis is not available")
		return
	}
	ctx := context.Background()

	// 1. Read-only tool should fallback to infinity (always reuse)
	roPayload := []byte(`{
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [{"id":"c2","type":"function","function":{"name":"web_search","arguments":"{}"}}]
			}
		]
	}`)
	roKey := "synapse:l1cache:testkey_ro"
	rdb.Set(ctx, roKey, "val", 10*time.Second) // age = 86390s (almost expired)
	defer rdb.Del(ctx, roKey)

	resRo := ShouldReuseCache(ctx, rdb, roPayload, roKey, 86400, "")
	if !resRo {
		t.Error("expected true for read-only tool (infinity TTL fallback)")
	}

	// 2. Stateful tool should fallback to 60s
	stPayload := []byte(`{
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [{"id":"c3","type":"function","function":{"name":"write_file","arguments":"{}"}}]
			}
		]
	}`)
	stKey := "synapse:l1cache:testkey_st"
	rdb.Set(ctx, stKey, "val", 86390*time.Second) // age = 10s
	defer rdb.Del(ctx, stKey)

	resStUnder60 := ShouldReuseCache(ctx, rdb, stPayload, stKey, 86400, "")
	if !resStUnder60 {
		t.Error("expected true for stateful tool with age <= 60s")
	}

	rdb.Set(ctx, stKey, "val", 86300*time.Second) // age = 100s (> 60s)
	resStOver60 := ShouldReuseCache(ctx, rdb, stPayload, stKey, 86400, "")
	if resStOver60 {
		t.Error("expected false for stateful tool with age > 60s")
	}
}
