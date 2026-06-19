// Package optiagent provides request optimization utilities for the
// Synapse Proxy: L1 exact cache, L2 semantic cache, L3 compression,
// loop detection, and tool-call deduplication.
package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// LoopDetectResult describes what we found when we checked for a loop
// on a payload hash inside a time window.
type LoopDetectResult struct {
	IsLoop       bool   // true if we believe this is the 2nd, 3rd... call in a loop
	LoopCount    int    // 1 = first call, 2 = second call, etc.
	WindowSecs        int    // the rolling window the counter uses
	ShouldReuse       bool   // true if the call should be served from the loop's cached response
	ReusePayload      []byte // the response to reuse (only set when ShouldReuse && IsLoop)
	TriggerKillSwitch bool   // true if the agent loop was blocked by the firewall kill switch
}

const (
	// LOOP_THRESHOLD is the count at which we start serving from cache.
	// 3rd call (count >= 3) gets the cached 1st-call response.
	LOOP_THRESHOLD = 3

	// LOOP_WINDOW_SECS is the rolling time window for the counter.
	LOOP_WINDOW_SECS = 60
)

// DetectLoop records the current call and decides whether it is part of a
// loop. The contract is:
//
//   - On the FIRST call in a window: IsLoop=false, LoopCount=1
//   - On the 2nd call (within LOOP_WINDOW_SECS): IsLoop=true, LoopCount=2,
//     ShouldReuse=false (still let it through, just record the pattern)
//   - On the 3rd+ call: IsLoop=true, ShouldReuse=true, and the cached
//     response from the FIRST call is returned so the proxy can serve it
//     without calling upstream.
//
// Storage layout (per virtual key):
//
//	Synapse Proxy:loops:<vk>:<payloadHash>         (ZSET, members=call ids, scores=ts)
//	Synapse Proxy:loops:<vk>:<payloadHash>:first   (STRING, the cached 1st-call response)
func DetectLoop(ctx context.Context, rdb *redis.Client, virtualKey, payloadHash string, killSwitch bool) LoopDetectResult {
	now := time.Now()
	nowNs := now.UnixNano()
	cutoffNs := now.Add(-time.Duration(LOOP_WINDOW_SECS) * time.Second).UnixNano()

	zKey := "synapse:loops:" + virtualKey + ":" + payloadHash
	firstKey := "synapse:loops:" + virtualKey + ":" + payloadHash + ":first"

	// 1. Evict entries older than the window.
	if err := rdb.ZRemRangeByScore(ctx, zKey, "-inf", strconv.FormatInt(cutoffNs, 10)).Err(); err != nil {
		// non-fatal
	}

	// 2. Count how many calls in the current window.
	count, err := rdb.ZCard(ctx, zKey).Result()
	if err != nil {
		count = 0
	}

	// 3. Record this call.
	member := strconv.FormatInt(nowNs, 10) + "-" + randSuffix()
	if err := rdb.ZAdd(ctx, zKey, redis.Z{Score: float64(nowNs), Member: member}).Err(); err != nil {
		// non-fatal
	}
	_ = rdb.Expire(ctx, zKey, time.Duration(LOOP_WINDOW_SECS+10)*time.Second).Err()

	loopCount := int(count) + 1

	if loopCount == 1 {
		return LoopDetectResult{
			IsLoop:     false,
			LoopCount:  loopCount,
			WindowSecs: LOOP_WINDOW_SECS,
		}
	}

	if loopCount >= LOOP_THRESHOLD {
		if killSwitch {
			return LoopDetectResult{
				IsLoop:            true,
				LoopCount:         loopCount,
				WindowSecs:        LOOP_WINDOW_SECS,
				TriggerKillSwitch: true,
			}
		}

		cached, err := rdb.Get(ctx, firstKey).Bytes()
		if err == nil && len(cached) > 0 {
			_ = rdb.Expire(ctx, firstKey, time.Duration(LOOP_WINDOW_SECS+10)*time.Second).Err()
			return LoopDetectResult{
				IsLoop:       true,
				LoopCount:    loopCount,
				WindowSecs:   LOOP_WINDOW_SECS,
				ShouldReuse:  true,
				ReusePayload: cached,
			}
		}
		// Cached response missing (expired, evicted, first call's upstream
		// error, ...) -> fall through to a normal upstream call. The proxy
		// will refresh :first via StoreLoopFirstResponse.
	}

	return LoopDetectResult{
		IsLoop:     true,
		LoopCount:  loopCount,
		WindowSecs: LOOP_WINDOW_SECS,
	}
}

// StoreLoopFirstResponse is called by the proxy after upstream returns
// successfully for what was the first call in a loop window.
//
// On the next identical call within LOOP_WINDOW_SECS, the 3rd+ call will
// pull this response from cache instead of re-hitting upstream.
func StoreLoopFirstResponse(ctx context.Context, rdb *redis.Client, virtualKey, payloadHash string, response []byte) {
	if len(response) == 0 {
		return
	}
	firstKey := "synapse:loops:" + virtualKey + ":" + payloadHash + ":first"
	_ = rdb.Set(ctx, firstKey, response, time.Duration(LOOP_WINDOW_SECS+10)*time.Second).Err()
}

func randSuffix() string {
	ts := uint32(time.Now().UnixNano() & 0xFFFFFFFF)
	b := make([]byte, 4)
	for i := 0; i < 4; i++ {
		b[i] = byte(ts >> (8 * i))
	}
	return hex.EncodeToString(b)
}

func hashForLogging(payload []byte) string {
	h := sha256.Sum256(payload)
	s := hex.EncodeToString(h[:])
	return s[:12]
}

func containsToolCallHeuristic(payload []byte) bool {
	s := string(payload)
	return strings.Contains(s, `"tool_calls"`) ||
		strings.Contains(s, `"function_call"`) ||
		strings.Contains(s, `"name":"`)
}
