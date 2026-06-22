package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"synapse-proxy/internal/db"
)

// CachePurgeHandler handles HTTP requests to purge Redis L1 and L2 cache entries.
//
// IMPORTANT: never use KEYS in production — KEYS blocks the entire Redis
// instance for the duration of the scan, which freezes the proxy hot path
// for every concurrent request. We use SCAN + UNLINK instead.
//
// SCAN is incremental and non-blocking; UNLINK reclaims memory in a
// background thread instead of synchronously. We batch deletes at 500
// keys so we still get good throughput without spiking Redis memory.
func CachePurgeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	rdb := db.GetRedis()
	var deleted int64

	// Optional: purge only for a specific virtual key.
	vk := r.URL.Query().Get("vk")

	var l1Pattern, l2Pattern string
	if vk != "" {
		l1Pattern = "synapse:l1cache:" + vk + ":*"
		l2Pattern = "synapse:l2cache:" + vk + ":*"
	} else {
		l1Pattern = "synapse:l1cache:*"
		l2Pattern = "synapse:l2cache:*"
	}

	deleted += scanAndUnlink(ctx, rdb, l1Pattern, "L1")
	deleted += scanAndUnlink(ctx, rdb, l2Pattern, "L2")

	log.Printf("Cache purged: %d keys deleted (vk=%s)", deleted, vk)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": deleted,
		"message": fmt.Sprintf("Purged %d cache entries (L1 + L2)", deleted),
	})
}

// scanAndUnlink walks the keyspace with SCAN (cursor-based, non-blocking)
// and batches deletes with UNLINK so the call doesn't block the Redis
// event loop on large keyspaces.
func scanAndUnlink(ctx context.Context, rdb *redis.Client, pattern, label string) int64 {
	var (
		cursor  uint64
		batch   []string
		deleted int64
	)

	for {
		// SCAN with COUNT=500 keeps each round-trip light; the cursor
		// returns 0 once the entire keyspace has been visited.
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			log.Printf("[purge:%s] SCAN failed at cursor %d: %v", label, cursor, err)
			break
		}
		batch = append(batch, keys...)
		if len(batch) >= 500 {
			n, err := rdb.Unlink(ctx, batch...).Result()
			if err != nil {
				log.Printf("[purge:%s] UNLINK failed: %v", label, err)
			} else {
				deleted += n
			}
			batch = batch[:0]
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	if len(batch) > 0 {
		n, err := rdb.Unlink(ctx, batch...).Result()
		if err != nil {
			log.Printf("[purge:%s] UNLINK failed (final batch): %v", label, err)
		} else {
			deleted += n
		}
	}
	return deleted
}