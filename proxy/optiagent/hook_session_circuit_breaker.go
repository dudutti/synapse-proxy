// Package optiagent — Session Circuit Breaker hook.
//
// Migrated from proxy.go (formerly lines ~299-311 inline block).
//
// Behaviour: when the virtual key config has SessionTokenLimit > 0 and
// the request has a SessionID, this hook:
//
//  1. Approximates the token count of the request payload.
//  2. Atomically increments `synapse:session_usage:<sessionID>` and
//     sets a 24h TTL.
//  3. If the post-increment total exceeds the configured limit, sets
//     ShortCircuitStatus=400 + an OpenAI-compatible JSON error body so
//     the proxy returns it without forwarding upstream.
//
// Fail-open: any backend error is swallowed and the request proceeds.
//
// Priority 110 = runs right after FingerprintHook (100) but before
// payload-mutating hooks (200+). This ordering matters because the
// circuit breaker measures the payload that downstream hooks may
// compress — i.e. the post-compression cost. We currently measure the
// pre-compression bytes/4 (matches the legacy inline code); future
// work could pass exact tokens from L3.
//
// # Backend abstraction
//
// The hook talks to a tiny hookRedis interface (one method). Production
// wires a redisBackend wrapping *redis.Client. Tests wire a stubBackend
// (in hook_test_helpers.go) by calling SetSessionCBBackendForTest. The
// wire-through is a single atomic.Pointer[redisBackend] so production
// reads are lock-free and typed.

package optiagent

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionCircuitBreakerHook enforces the per-session token budget.
type SessionCircuitBreakerHook struct{}

// Name returns the stable hook identifier (used in metrics labels).
func (h *SessionCircuitBreakerHook) Name() string { return "session_circuit_breaker" }

// Priority 110 = early counter (after fingerprint, before mutators).
func (h *SessionCircuitBreakerHook) Priority() int { return 110 }

// IsEnabled gates on a non-empty VK. The finer-grained checks
// (limit > 0, session present) live in BeforeRequest because they
// need HookContext, not just the VK.
func (h *SessionCircuitBreakerHook) IsEnabled(vk string) bool { return vk != "" }

// BeforeRequest applies the circuit breaker. See file docstring.
//
// Returns (nil, nil) on the happy path or on backend failure (fail-open).
// On limit breach: payload is nil, ShortCircuitStatus and Body are set;
// the runner detects this and returns the body without calling upstream.
func (h *SessionCircuitBreakerHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(h.Name(), hctx.VK)

	// Gate 1: a positive limit must be configured.
	limitI, ok := hctx.Feature("session_token_limit")
	if !ok {
		return nil, nil
	}
	limit, ok := limitI.(int)
	if !ok || limit <= 0 {
		return nil, nil
	}

	// Gate 2: a session id must be present (circuit breaker is per-session).
	if hctx.SessionID == "" {
		return nil, nil
	}

	// Resolve the backend. nil (= sentinel == "no backend") => fail open.
	backend := currentSessionCBBackend()
	if backend == nil {
		return nil, nil
	}

	payload := currentPayload(hctx)
	approxTokens := len(payload) / 4 // Cheap approximation matching the legacy inline code.

	usageKey := "synapse:session_usage:" + hctx.SessionID
	newTotal, err := backend.IncrByExpire(ctx, usageKey, int64(approxTokens), 24*time.Hour)
	if err != nil {
		log.Printf("[session-cb] IncrByExpire failed for session=%s vk=%s: %v (fail-open)", hctx.SessionID, hctx.VK, err)
		return nil, nil
	}

	if newTotal > int64(limit) {
		body := `{"error":{"message":"Agent Firewall: Session token limit exceeded (` +
			strconv.Itoa(limit) + ` tokens).","type":"session_circuit_breaker","code":400}}`
		hctx.ShortCircuitStatus = http.StatusBadRequest
		hctx.ShortCircuitBody = []byte(body)
		log.Printf("[session-cb] TRIPPED: session=%s vk=%s usage=%d > limit=%d",
			hctx.SessionID, hctx.VK, newTotal, limit)
		return nil, nil
	}

	return nil, nil
}

// AfterResponse is a no-op (the circuit breaker only acts on the request side).
func (h *SessionCircuitBreakerHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// --- Backend wiring --------------------------------------------------

// hookRedis is the minimal Redis surface our hooks need: a single
// atomic IncrBy with a TTL set in one round-trip, plus SMembers for
// the ToolFilterHook denylist. Production implementations adapt
// *redis.Client (see redisBackend below); tests provide their own
// (see stubBackend in hook_test_helpers.go).
type hookRedis interface {
	IncrByExpire(ctx context.Context, key string, value int64, ttl time.Duration) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	// SAddSet records multiple members in a Redis set in a single
	// round-trip and refreshes the key's TTL. The TTL parameter
	// governs how long the set persists before being forgotten
	// (the operator's "abandoned agent" cleanup).
	SAddSet(ctx context.Context, key string, members []string, ttl time.Duration) error
	// Get returns the value at the given key. Returns (nil, nil) on
	// miss (matches Redis semantics: nil is not an error).
	Get(ctx context.Context, key string) ([]byte, error)
	// Set stores value at the given key with a TTL. Used by
	// ToolDedupHook to record the placeholder body for a fresh
	// file read.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// SetNX is "set if not exists": it stores the value at
	// key only if the key does not already have a value.
	// Returns (true, nil) if the value was stored, (false, nil)
	// if the key already existed. Used by CCRStoreHook so
	// concurrent requests for the same canonical payload
	// don't overwrite each other (first writer wins).
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	// Incr atomically increments the counter at the given key by
	// 1 and returns the new value. Used by ToolDedupHook to count
	// dedup hits.
	Incr(ctx context.Context, key string) (int64, error)
	// Expire sets a TTL on the given key. Used by LoopDetectionHook
	// to bound the lifetime of the per-window ZSET.
	Expire(ctx context.Context, key string, ttl time.Duration) error
	// ZAdd adds a member with the given score to a sorted set. Used
	// by LoopDetectionHook to record the current call's timestamp.
	ZAdd(ctx context.Context, key string, score float64, member string) error
	// ZCard returns the cardinality (number of members) of a sorted
	// set. Used by LoopDetectionHook to count calls in the window.
	ZCard(ctx context.Context, key string) (int64, error)
	// ZRemRangeByScore removes all members with score in [min, max]
	// from a sorted set. Used by LoopDetectionHook to evict entries
	// outside the rolling 60s window.
	ZRemRangeByScore(ctx context.Context, key, min, max string) error
	// SIsMember returns true if the given member is in the set at
	// the given key. Used by ModelRadarHook.
	SIsMember(ctx context.Context, key, member string) (bool, error)
	// Exists returns true if the given key has any entries (in
	// Redis: TTL > 0). Used by ModelRadarHook.
	Exists(ctx context.Context, key string) (bool, error)
}

// errSessionCBBackendNil is returned when redisBackend is asked to
// IncrByExpire with a nil inner state. Defensive only.
var errSessionCBBackendNil = errors.New("session-cb: backend is nil")

// redisBackend adapts a *redis.Client to hookRedis. Used in production.
// The optional testHook field lets a test inject a fake hookRedis via
// SetSessionCBBackendForTest without changing the public type.
type redisBackend struct {
	rdb      *redis.Client
	testHook hookRedis // nil in production; set only in tests
}

// IncrByExpire dispatches to the test hook if set, otherwise to the
// real Redis pipeline. Single round-trip: INCRBY + EXPIRE.
func (b *redisBackend) IncrByExpire(ctx context.Context, key string, value int64, ttl time.Duration) (int64, error) {
	if b == nil {
		return 0, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.IncrByExpire(ctx, key, value, ttl)
	}
	if b.rdb == nil {
		return 0, errSessionCBBackendNil
	}
	pipe := b.rdb.Pipeline()
	incr := pipe.IncrBy(ctx, key, value)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// SMembers delegates to the test hook if set, otherwise to the real
// Redis client. Used by ToolFilterHook to fetch the operator-curated
// denylist. Returns nil (not an error) when the key doesn't exist —
// this matches Redis semantics: SMEMBERS on a missing key returns
// an empty set, not an error.
func (b *redisBackend) SMembers(ctx context.Context, key string) ([]string, error) {
	if b == nil {
		return nil, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.SMembers(ctx, key)
	}
	if b.rdb == nil {
		return nil, errSessionCBBackendNil
	}
	return b.rdb.SMembers(ctx, key).Result()
}

// SAddSet records members in a Redis set in a single round-trip
// and refreshes the TTL. Used by AgentDiscoveryHook. Production
// uses a pipeline; the test hook records the call for assertions.
func (b *redisBackend) SAddSet(ctx context.Context, key string, members []string, ttl time.Duration) error {
	if b == nil {
		return errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.SAddSet(ctx, key, members, ttl)
	}
	if b.rdb == nil {
		return errSessionCBBackendNil
	}
	pipe := b.rdb.Pipeline()
	pipe.SAdd(ctx, key, toIfaceSlice(members)...)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// toIfaceSlice converts []string to []interface{} for the variadic
// SAdd API. Lives next to its sole caller.
func toIfaceSlice(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// Get delegates to the test hook if set, otherwise to the real
// Redis client. Used by ToolDedupHook to fetch a previously-stored
// file body. Returns (nil, nil) on miss (Redis semantics).
func (b *redisBackend) Get(ctx context.Context, key string) ([]byte, error) {
	if b == nil {
		return nil, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.Get(ctx, key)
	}
	if b.rdb == nil {
		return nil, errSessionCBBackendNil
	}
	return b.rdb.Get(ctx, key).Bytes()
}

// Set delegates to the test hook if set, otherwise to the real
// Redis client. Used by ToolDedupHook to record the placeholder
// body for a fresh file read.
func (b *redisBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if b == nil {
		return errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.Set(ctx, key, value, ttl)
	}
	if b.rdb == nil {
		return errSessionCBBackendNil
	}
	return b.rdb.Set(ctx, key, value, ttl).Err()
}

// SetNX delegates to the test hook if set, otherwise to the
// real Redis client. The go-redis API returns a *Bool: true
// if the value was set, false if the key already existed.
func (b *redisBackend) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if b == nil {
		return false, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.SetNX(ctx, key, value, ttl)
	}
	if b.rdb == nil {
		return false, errSessionCBBackendNil
	}
	return b.rdb.SetNX(ctx, key, value, ttl).Result()
}

// Incr delegates to the test hook if set, otherwise to the real
// Redis client. Used by ToolDedupHook to count dedup hits.
func (b *redisBackend) Incr(ctx context.Context, key string) (int64, error) {
	if b == nil {
		return 0, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.Incr(ctx, key)
	}
	if b.rdb == nil {
		return 0, errSessionCBBackendNil
	}
	return b.rdb.Incr(ctx, key).Result()
}

// Expire delegates to the test hook if set, otherwise to the real
// Redis client. Used by LoopDetectionHook.
func (b *redisBackend) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if b == nil {
		return errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.Expire(ctx, key, ttl)
	}
	if b.rdb == nil {
		return errSessionCBBackendNil
	}
	return b.rdb.Expire(ctx, key, ttl).Err()
}

// ZAdd delegates to the test hook if set, otherwise to the real
// Redis client. Used by LoopDetectionHook to record a call's
// timestamp in the rolling-window ZSET.
func (b *redisBackend) ZAdd(ctx context.Context, key string, score float64, member string) error {
	if b == nil {
		return errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.ZAdd(ctx, key, score, member)
	}
	if b.rdb == nil {
		return errSessionCBBackendNil
	}
	return b.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

// ZCard delegates to the test hook if set, otherwise to the real
// Redis client. Returns 0 on a missing key (Redis semantics).
func (b *redisBackend) ZCard(ctx context.Context, key string) (int64, error) {
	if b == nil {
		return 0, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.ZCard(ctx, key)
	}
	if b.rdb == nil {
		return 0, errSessionCBBackendNil
	}
	return b.rdb.ZCard(ctx, key).Result()
}

// ZRemRangeByScore delegates to the test hook if set, otherwise
// to the real Redis client. Used by LoopDetectionHook to evict
// entries outside the rolling window.
func (b *redisBackend) ZRemRangeByScore(ctx context.Context, key, min, max string) error {
	if b == nil {
		return errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.ZRemRangeByScore(ctx, key, min, max)
	}
	if b.rdb == nil {
		return errSessionCBBackendNil
	}
	return b.rdb.ZRemRangeByScore(ctx, key, min, max).Err()
}

// SIsMember delegates to the test hook if set, otherwise to the
// real Redis client. Used by ModelRadarHook.
func (b *redisBackend) SIsMember(ctx context.Context, key, member string) (bool, error) {
	if b == nil {
		return false, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.SIsMember(ctx, key, member)
	}
	if b.rdb == nil {
		return false, errSessionCBBackendNil
	}
	return b.rdb.SIsMember(ctx, key, member).Result()
}

// Exists delegates to the test hook if set, otherwise to the
// real Redis client. Used by ModelRadarHook.
func (b *redisBackend) Exists(ctx context.Context, key string) (bool, error) {
	if b == nil {
		return false, errSessionCBBackendNil
	}
	if b.testHook != nil {
		return b.testHook.Exists(ctx, key)
	}
	if b.rdb == nil {
		return false, errSessionCBBackendNil
	}
	n, err := b.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

// sessionCBBackend holds the active *redisBackend. atomic.Pointer gives
// lock-free reads on the hot path with strict typing. nilSentinel
// represents "no backend wired" without falling foul of atomic.Pointer's
// requirement that the type be a pointer-to-struct.
var sessionCBBackend atomic.Pointer[redisBackend]

// nilSentinel marks the no-backend state. Stored via Swap so the
// restore function in SetSessionCBBackendForTest always has a non-nil
// prev to put back.
var nilSentinel = (*redisBackend)(nil)

func init() {
	sessionCBBackend.Store(nilSentinel)
}

// currentSessionCBBackend returns the active backend, or nil if none
// is wired. Safe to call from any goroutine.
func currentSessionCBBackend() *redisBackend {
	return sessionCBBackend.Load()
}

// getCCRBackend returns the active backend as a hookRedis
// interface, dispatching correctly between test (stub) and
// production (real Redis). The CCR hooks (Retrieve/Store)
// use this so they work both in unit tests and in the
// running proxy. Returns nil if no backend is wired.
func getCCRBackend() hookRedis {
	b := currentSessionCBBackend()
	if b == nil || b == nilSentinel {
		return nil
	}
	if b.testHook != nil {
		return b.testHook
	}
	if b.rdb != nil {
		return &redisBackend{testHook: nil, rdb: b.rdb}
	}
	return nil
}

// SetSessionCBBackend is called from main.go after Redis is initialised.
// Pass nil to disable the hook at runtime.
func SetSessionCBBackend(rdb *redis.Client) {
	if rdb == nil {
		sessionCBBackend.Store(nilSentinel)
		return
	}
	sessionCBBackend.Store(&redisBackend{rdb: rdb})
}

// SetSessionCBBackendForTest lets tests inject a custom hookRedis
// (typically a stubBackend). The backend is wrapped in a redisBackend
// with the testHook field set, so it lands in the same atomic.Pointer
// slot as production backends. The returned restore function MUST be
// deferred.
//
// Passing nil is allowed and means "no backend" for the duration of
// the test.
func SetSessionCBBackendForTest(b hookRedis) func() {
	var wrapper *redisBackend
	if b == nil {
		wrapper = nilSentinel
	} else {
		wrapper = &redisBackend{testHook: b}
	}
	prev := sessionCBBackend.Swap(wrapper)
	return func() { sessionCBBackend.Store(prev) }
}

func init() {
	// Auto-register the hook so existing main.go code (which already
	// calls RunBeforeHooks) picks it up without code changes.
	RegisterHook(&SessionCircuitBreakerHook{})
}
