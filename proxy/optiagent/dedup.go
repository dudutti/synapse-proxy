package optiagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// HashPayload returns the SHA-256 hex digest of a request payload,
// matching the format used by ProcessRequest internally so the L0
// lock key is identical to the one we would use for L1.
func HashPayload(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

// L0 dedup: collapse identical in-flight requests into a single
// upstream call. Two callers hitting the proxy at the same time
// with the same payload (same SHA-256) will be coalesced: the
// first one acquires the L0 lock and does the work, the others
// wait up to 30s for the cached response.
//
// Storage layout:
//
//	optitoken:l0:lock:<vk>:<sha256>   STRING, "in-flight:<uuid>", TTL 30s
//	optitoken:l0:resp:<vk>:<sha256>   STRING, the JSON response bytes, TTL 30s
//
// The proxy must call ReleaseL0 after the upstream call completes
// (success OR failure) so the lock is freed even on errors.

const (
	L0LockTTLSec = 30
	L0RespTTLSec = 30
	L0PollMS     = 50
)

// L0Acquire tries to become the leader for this (vk, hash).
// Returns (true, "") on success, (false, "") if another worker
// already holds the lock.
func L0Acquire(ctx context.Context, rdb *redis.Client, virtualKey, payloadHash string) (bool, string) {
	lockKey := "optitoken:l0:lock:" + virtualKey + ":" + payloadHash
	suffix := make([]byte, 8)
	_, _ = rand.Read(suffix)
	workerID := "in-flight:" + hex.EncodeToString(suffix)
	ok, err := rdb.SetNX(ctx, lockKey, workerID, time.Duration(L0LockTTLSec)*time.Second).Result()
	if err != nil {
		// Redis error: fail-open, let caller proceed.
		log.Printf("[L0] SetNX error: %v (failing open)", err)
		return true, workerID
	}
	return ok, workerID
}

// L0Wait polls the response key. Returns the response bytes if a
// leader published one within the timeout, or an error on timeout.
// Polls every L0PollMS.
func L0Wait(ctx context.Context, rdb *redis.Client, virtualKey, payloadHash string) ([]byte, error) {
	respKey := "optitoken:l0:resp:" + virtualKey + ":" + payloadHash
	deadline := time.Now().Add(time.Duration(L0LockTTLSec) * time.Second)
	for {
		resp, err := rdb.Get(ctx, respKey).Bytes()
		if err == nil && len(resp) > 0 {
			return resp, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("L0 wait timeout")
		}
		// honour context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(L0PollMS * time.Millisecond):
		}
	}
}

// L0Release publishes the response and clears the lock. Safe to
// call even if the caller never acquired (no-op).
func L0Release(ctx context.Context, rdb *redis.Client, virtualKey, payloadHash, workerID string, response []byte) {
	respKey := "optitoken:l0:resp:" + virtualKey + ":" + payloadHash
	lockKey := "optitoken:l0:lock:" + virtualKey + ":" + payloadHash

	// Lua: only DEL the lock if the value matches our worker ID, so
	// we don't accidentally release a lock taken by another worker
	// (e.g. if our TTL expired and another worker acquired after).
	script := redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		end
		return 0
	`)
	if _, err := script.Run(ctx, rdb, []string{lockKey}, workerID).Result(); err != nil {
		log.Printf("[L0] lock release error: %v", err)
	}
	if len(response) > 0 {
		if err := rdb.Set(ctx, respKey, response, time.Duration(L0RespTTLSec)*time.Second).Err(); err != nil {
			log.Printf("[L0] resp publish error: %v", err)
		}
	}
}
