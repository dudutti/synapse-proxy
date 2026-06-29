// Test helpers for optiagent/hook_*_test.go.
//
// Provides an in-memory hookRedis backend that the hooks consult via
// the hookRedis interface. This avoids the impedance mismatch of
// mocking *redis.Client directly — *redis.Client is a concrete struct
// with hundreds of methods and a full RESP wire protocol, which is
// impractical to fake.
//
// Production wiring uses a real *redis.Client wrapped by a redisBackend
// (see hook_session_circuit_breaker.go). Tests inject a stubBackend.

package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// stubBackend is an in-memory fake of hookRedis. Thread-safe.
//
// Fields are documented next to their accessors; see the seed*
// methods below for the canonical "set up state for a test"
// pattern.
type stubBackend struct {
	mu             sync.Mutex
	counts         map[string]int64
	storedBodies   map[string][]byte
	storedTTLs     map[string]time.Duration
	denyKeyToTools map[string][]string
	denyError      error
	returnErr      error
	calls          []stubBackendCall

	// ZSET mock state: zsetMembers[key] = map[member]score.
	// ZCard returns len(zsetMembers[key]).
	zsetMembers map[string]map[string]float64

	// Cached first responses for LoopDetection. Keyed on
	// "synapse:loops:<vk>:<hash>:first" (the canonical suffix).
	firstResponses map[string][]byte

	// SISMEMBER mock state: sismemberHits["<set>:<member>"] = true
	// for matches. Used by ModelRadarHook.
	sismemberHits map[string]bool

	// EXISTS mock state: keys present in this set return true.
	// Used by ModelRadarHook.
	existsKeys map[string]struct{}
}

type stubBackendCall struct {
	// Op identifies the Redis call kind: "incrbyexpire", "smembers",
	// "saddset", "get", "set", "incr", "expire", "zadd", "zcard",
	// or "zremrangebyscore". Set automatically by the corresponding
	// method; tests assert on it to filter by op.
	Op      string
	Key     string
	Value   int64
	TTL     time.Duration
	Members []string // for SAddSet calls
	Body    []byte   // for Set / Get payload bytes
}

func newStubRedis() *stubBackend {
	return &stubBackend{
		counts:       map[string]int64{},
		storedBodies: map[string][]byte{},
		storedTTLs:   map[string]time.Duration{},
	}
}

// newStubBackendWithCount returns a stub pre-seeded so the FIRST
// IncrByExpire call returns (seeded+value). Used to simulate a session
// that's already accumulated usage.
func newStubBackendWithCount(seed int64) *stubBackend {
	return &stubBackend{counts: map[string]int64{"__seed__": seed - 1}}
}

// setDenyList installs a denylist for a given VK. Convenience used
// by tests that want the canonical "vk-X has these denied tools"
// setup without exposing the map directly.
func (s *stubBackend) setDenyList(vk string, tools []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denyKeyToTools == nil {
		s.denyKeyToTools = map[string][]string{}
	}
	s.denyKeyToTools[denyKey(vk)] = tools
}

// setStoredBody pre-seeds a key→body entry that subsequent Get
// calls will return. Used by ToolDedupHook tests to simulate
// "this file body was stored by an earlier request".
func (s *stubBackend) setStoredBody(vk, filePath string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storedBodies == nil {
		s.storedBodies = map[string][]byte{}
	}
	if s.storedTTLs == nil {
		s.storedTTLs = map[string]time.Duration{}
	}
	s.storedBodies[fileDedupKey(vk, filePath)] = body
}

// seedLoopCount pre-seeds the ZSET cardinality for the
// canonical loop key. The hook's ZCard call will return `count`
// (i.e. "this is the (count+1)th call in the window"). Member
// scores are synthetic timestamps; the production code doesn't
// read them back, it only uses ZCard.
func (s *stubBackend) seedLoopCount(vk, payloadHash string, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.zsetMembers == nil {
		s.zsetMembers = map[string]map[string]float64{}
	}
	key := loopZKey(vk, payloadHash)
	if count <= 0 {
		delete(s.zsetMembers, key)
		return
	}
	members := make(map[string]float64, count)
	for i := 0; i < count; i++ {
		members[loopMemberName(i)] = float64(i)
	}
	s.zsetMembers[key] = members
}

// seedFirstResponse pre-seeds the "<key>:first" cache that the
// LoopDetectionHook reads on the 3rd+ call to short-circuit
// the upstream.
func (s *stubBackend) seedFirstResponse(vk, payloadHash string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstResponses == nil {
		s.firstResponses = map[string][]byte{}
	}
	s.firstResponses[loopFirstKey(vk, payloadHash)] = body
}

// seedLoopCountForPayload is a convenience wrapper: hashes the
// payload (same algorithm as the production hook) and seeds the
// canonical loop key. Tests that have a known payload prefer this
// over seedLoopCount because they don't have to hand-compute the
// SHA-256 of the payload.
func (s *stubBackend) seedLoopCountForPayload(vk string, payload []byte, count int) {
	s.seedLoopCount(vk, sha256Hex(payload), count)
}

// seedFirstResponseForPayload is the matching convenience for
// the cached first-response.
func (s *stubBackend) seedFirstResponseForPayload(vk string, payload []byte, body []byte) {
	s.seedFirstResponse(vk, sha256Hex(payload), body)
}

func (s *stubBackend) IncrByExpire(_ context.Context, key string, value int64, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "incrbyexpire", Key: key, Value: value, TTL: ttl})
	if s.returnErr != nil {
		return 0, s.returnErr
	}
	// Seed: if the key is fresh and a __seed__ value exists, treat
	// the first IncrByExpire as if we were already at that count.
	if _, exists := s.counts[key]; !exists {
		if seed, hasSeed := s.counts["__seed__"]; hasSeed {
			s.counts[key] = seed
		}
	}
	s.counts[key] += value
	return s.counts[key], nil
}

// SMembers returns the mocked denylist for the given Redis key.
func (s *stubBackend) SMembers(_ context.Context, key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denyError != nil {
		return nil, s.denyError
	}
	if s.denyKeyToTools == nil {
		return nil, nil
	}
	out := s.denyKeyToTools[key]
	return out, nil
}

// SAddSet records the call in the snapshot. The stub does NOT
// actually mutate any state — discovery is observation-only.
func (s *stubBackend) SAddSet(_ context.Context, key string, members []string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "saddset", Key: key, Members: members, TTL: ttl})
	return s.returnErr
}

// Get returns the pre-seeded body for the key, or (nil, nil) on
// miss. Lookup order:
//   1. firstResponses[<key>]   — LoopDetection "first" cache
//   2. storedBodies[<key>]     — ToolDedup file body
func (s *stubBackend) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	if s.firstResponses != nil {
		if body, ok := s.firstResponses[key]; ok {
			out := make([]byte, len(body))
			copy(out, body)
			return out, nil
		}
	}
	if s.storedBodies == nil {
		return nil, nil
	}
	body, ok := s.storedBodies[key]
	if !ok {
		return nil, nil
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

// Set stores the value in storedBodies (so subsequent Get
// returns it) and records the call in the snapshot. This
// matches Redis SET semantics: the value is retrievable
// until the TTL elapses.
func (s *stubBackend) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	body := make([]byte, len(value))
	copy(body, value)
	s.storedBodies[key] = body
	s.storedTTLs[key] = ttl
	s.calls = append(s.calls, stubBackendCall{Op: "set", Key: key, Body: body, TTL: ttl})
	return s.returnErr
}

// SetNX is the "set if not exists" variant. The stub's
// behavior:
//   - If storedBodies already has an entry for key, return
//     (false, nil) — the call was a no-op.
//   - Otherwise, set the entry and return (true, nil).
// This mirrors the Redis SET ... NX semantics: first
// writer wins, later writers are no-ops.
func (s *stubBackend) SetNX(_ context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.storedBodies[key]; exists {
		return false, nil
	}
	body := make([]byte, len(value))
	copy(body, value)
	s.storedBodies[key] = body
	s.storedTTLs[key] = ttl
	s.calls = append(s.calls, stubBackendCall{Op: "setnx", Key: key, Body: body, TTL: ttl})
	return true, nil
}

// Incr records the call and returns a synthetic counter (1 per call).
func (s *stubBackend) Incr(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "incr", Key: key, Value: 1})
	if s.returnErr != nil {
		return 0, s.returnErr
	}
	return 1, nil
}

// Expire records the call. Stub does not track TTL.
func (s *stubBackend) Expire(_ context.Context, key string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "expire", Key: key, TTL: ttl})
	return s.returnErr
}

// ZAdd records the call and updates the in-memory ZSET so
// subsequent ZCard reflects the new cardinality.
func (s *stubBackend) ZAdd(_ context.Context, key string, score float64, member string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "zadd", Key: key, Value: int64(score)})
	if s.returnErr != nil {
		return s.returnErr
	}
	if s.zsetMembers == nil {
		s.zsetMembers = map[string]map[string]float64{}
	}
	if s.zsetMembers[key] == nil {
		s.zsetMembers[key] = map[string]float64{}
	}
	s.zsetMembers[key][member] = score
	return nil
}

// ZCard returns the cardinality of the in-memory ZSET. 0 on miss.
func (s *stubBackend) ZCard(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "zcard", Key: key})
	if s.returnErr != nil {
		return 0, s.returnErr
	}
	members, ok := s.zsetMembers[key]
	if !ok {
		return 0, nil
	}
	return int64(len(members)), nil
}

// ZRemRangeByScore is a no-op for the stub. The call is recorded.
func (s *stubBackend) ZRemRangeByScore(_ context.Context, key, min, max string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "zremrangebyscore", Key: key})
	return s.returnErr
}

// SIsMember returns true if member is in the configured known
// models set. Tests configure via stub.sismemberHits (a map from
// "<set>:<member>" to bool). Default: false.
func (s *stubBackend) SIsMember(_ context.Context, key, member string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "sismember", Key: key})
	if s.returnErr != nil {
		return false, s.returnErr
	}
	if s.sismemberHits == nil {
		return false, nil
	}
	hit, ok := s.sismemberHits[key+":"+member]
	return hit && ok, nil
}

// Exists returns true if the key has been pre-seeded via
// setExists. Default: false.
func (s *stubBackend) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubBackendCall{Op: "exists", Key: key})
	if s.returnErr != nil {
		return false, s.returnErr
	}
	if s.existsKeys == nil {
		return false, nil
	}
	_, ok := s.existsKeys[key]
	return ok, nil
}

// Del deletes a key from the stub's storedBodies and firstResponses.
func (s *stubBackend) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.storedBodies, key)
	delete(s.firstResponses, key)
	s.calls = append(s.calls, stubBackendCall{Op: "del", Key: key})
	return s.returnErr
}

// addKnownModel is a convenience for tests: marks a (provider,
// modelID) pair as known. ModelRadarHook's SIsMember call will
// return true for this pair.
func (s *stubBackend) addKnownModel(provider, modelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sismemberHits == nil {
		s.sismemberHits = map[string]bool{}
	}
	s.sismemberHits["synapse:radar:known_models:"+provider+":"+modelID] = true
}

// setExists marks a key as existing. Used by ModelRadarHook to
// simulate "radar entry already created on a previous request".
func (s *stubBackend) setExists(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.existsKeys == nil {
		s.existsKeys = map[string]struct{}{}
	}
	s.existsKeys[key] = struct{}{}
}

// seedRadarEntry is a convenience for tests: marks a
// `<key>` as existing AND pre-loads the entry body under the
// same key. The ModelRadarHook's warm path needs both —
// `Exists` returns true and `Get` returns the JSON to update.
func (s *stubBackend) seedRadarEntry(key string, body []byte) {
	s.setExists(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storedBodies == nil {
		s.storedBodies = map[string][]byte{}
	}
	s.storedBodies[key] = body
}

// ValueStr returns a string view of the Body bytes.
func (c stubBackendCall) ValueStr() string {
	return string(c.Body)
}

// Snapshot returns the recorded calls (for assertions).
func (s *stubBackend) Snapshot() []stubBackendCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubBackendCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// denyKey builds the canonical denylist Redis key for a VK.
func denyKey(vk string) string {
	return "synapse:denied_tools:" + vk
}

// fileDedupKey builds the canonical ToolDedup Redis key for a VK
// + file path.
func fileDedupKey(vk, filePath string) string {
	h := sha256.Sum256([]byte(filePath))
	return "synapse:tools:" + vk + ":" + hex.EncodeToString(h[:])
}

// loopZKey builds the canonical LoopDetection ZSET key.
func loopZKey(vk, payloadHash string) string {
	return "synapse:loops:" + vk + ":" + payloadHash
}

// loopFirstKey builds the canonical LoopDetection "first" cache key.
func loopFirstKey(vk, payloadHash string) string {
	return loopZKey(vk, payloadHash) + ":first"
}

// loopMemberName returns a deterministic member name for the
// given index. The production code uses a timestamp+random suffix
// but the stub only needs unique strings.
func loopMemberName(i int) string {
	return fmt.Sprintf("seed-%d", i)
}

// sha256Hex is the stub-side equivalent of the production hook's
// payload hash. Tests call this to compute the expected key shape
// when seeding state.
func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func testCtx() context.Context { return context.Background() }

// errRedisDown is a sentinel error for tests.
var errRedisDown = errors.New("redis down")