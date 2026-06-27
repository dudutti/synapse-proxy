// Package optiagent — CompressionStore.
//
// When the LogCompressor (or any other transform hook)
// compresses a content block, it should also store the
// ORIGINAL in a CompressionStore under a stable
// cache_key. The LLM can later request the original
// via the headroom_retrieve tool (P0.6).
//
// This is the Headroom `enable_ccr: true` behavior,
// simplified to a key-value interface. Phase 1 uses
// an in-memory implementation for tests. Phase 2
// (P0.6) will add a Redis backend.
//
// The cache_key is a hex-encoded SHA-256 of the
// original content, truncated to 32 chars. This gives
// us 128 bits of collision resistance, which is enough
// to detect any practical duplicate without storing
// the key in a separate index.

package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
	"synapse-proxy/internal/db"
)

// CompressionStoreEntry is one row in the store.
type CompressionStoreEntry struct {
	Key   string
	Value []byte
}

// CompressionStore is the interface for a CCR
// compression store. Phase 1 has InMemoryCompressionStore;
// Phase 2 will add RedisCompressionStore.
type CompressionStore interface {
	// Save stores value under the given cache_key.
	// Returns the key (for chaining).
	Save(key string, value []byte) (string, error)
	// Lookup returns the value for a key, or nil if
	// not found.
	Lookup(key string) ([]byte, error)
	// Count returns the number of entries.
	Count() int
	// Reset clears all entries.
	Reset()
	// Entries returns a copy of all entries.
	Entries() []CompressionStoreEntry
}

// inMemoryCompressionStore is a thread-safe in-memory
// implementation. Used for tests and as a fallback if
// Redis is unavailable.
type inMemoryCompressionStore struct {
	mu      sync.Mutex
	entries map[string][]byte
}

// NewInMemoryCompressionStore returns a fresh
// in-memory store.
func NewInMemoryCompressionStore() CompressionStore {
	return &inMemoryCompressionStore{
		entries: make(map[string][]byte),
	}
}

func newInMemoryCompressionStore() CompressionStore {
	return NewInMemoryCompressionStore()
}

// Save stores value under the given key.
func (s *inMemoryCompressionStore) Save(key string, value []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string][]byte)
	}
	s.entries[key] = value
	return key, nil
}

// Lookup returns the value for key.
func (s *inMemoryCompressionStore) Lookup(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.entries[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// Count returns the entry count.
func (s *inMemoryCompressionStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Reset clears all entries.
func (s *inMemoryCompressionStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string][]byte)
}

// Entries returns a copy of all entries.
func (s *inMemoryCompressionStore) Entries() []CompressionStoreEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CompressionStoreEntry, 0, len(s.entries))
	for k, v := range s.entries {
		out = append(out, CompressionStoreEntry{Key: k, Value: v})
	}
	return out
}

// cacheKeyFor returns a stable cache_key for the given
// content. We use the first 32 hex chars of the SHA-256
// (128 bits of collision resistance).
func cacheKeyFor(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:32]
}

// globalCompressionStore is the default in-memory store
// used when no explicit store is configured. Phase 2
// will switch this to a Redis-backed store.
var globalCompressionStore CompressionStore = newInMemoryCompressionStore()

// GetGlobalCompressionStore returns the global store.
func GetGlobalCompressionStore() CompressionStore {
	return globalCompressionStore
}

// SetGlobalCompressionStore replaces the global store.
// Used in main.go to wire the Redis backend in prod.
func SetGlobalCompressionStore(s CompressionStore) {
	globalCompressionStore = s
}

// RedisCompressionStore is a Redis-backed implementation of CompressionStore.
type RedisCompressionStore struct{}

// NewRedisCompressionStore returns a new Redis-backed CompressionStore.
func NewRedisCompressionStore() *RedisCompressionStore {
	return &RedisCompressionStore{}
}

// Save stores value under the given key in Redis.
func (s *RedisCompressionStore) Save(key string, value []byte) (string, error) {
	rdb := db.GetRedis()
	if rdb == nil {
		return "", errors.New("redis client not initialized")
	}
	ctx := context.Background()
	redisKey := "synapse:ccr:" + key
	err := rdb.Set(ctx, redisKey, value, CCRRetrieveTTL).Err()
	if err != nil {
		return "", err
	}
	return key, nil
}

// Lookup retrieves the value for key from Redis.
func (s *RedisCompressionStore) Lookup(key string) ([]byte, error) {
	rdb := db.GetRedis()
	if rdb == nil {
		return nil, errors.New("redis client not initialized")
	}
	ctx := context.Background()
	redisKey := "synapse:ccr:" + key
	val, err := rdb.Get(ctx, redisKey).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Count returns the number of entries under prefix "synapse:ccr:" in Redis.
func (s *RedisCompressionStore) Count() int {
	rdb := db.GetRedis()
	if rdb == nil {
		return 0
	}
	ctx := context.Background()
	var count int
	var cursor uint64
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, "synapse:ccr:*", 0).Result()
		if err != nil {
			return 0
		}
		count += len(keys)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return count
}

// Reset clears all entries under prefix "synapse:ccr:" in Redis.
func (s *RedisCompressionStore) Reset() {
	rdb := db.GetRedis()
	if rdb == nil {
		return
	}
	ctx := context.Background()
	var cursor uint64
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, "synapse:ccr:*", 0).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			rdb.Del(ctx, keys...)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// Entries returns a copy of all entries under prefix "synapse:ccr:" in Redis.
func (s *RedisCompressionStore) Entries() []CompressionStoreEntry {
	rdb := db.GetRedis()
	if rdb == nil {
		return nil
	}
	ctx := context.Background()
	var cursor uint64
	var entries []CompressionStoreEntry
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, "synapse:ccr:*", 0).Result()
		if err != nil {
			return nil
		}
		for _, key := range keys {
			val, err := rdb.Get(ctx, key).Bytes()
			if err == nil {
				originalKey := strings.TrimPrefix(key, "synapse:ccr:")
				entries = append(entries, CompressionStoreEntry{
					Key:   originalKey,
					Value: val,
				})
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return entries
}
