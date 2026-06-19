package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"synapse-proxy/internal/db"
)

// CachePurgeHandler handles HTTP requests to purge Redis L1 and L2 cache entries
func CachePurgeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := context.Background()
	rdb := db.GetRedis()
	var deleted int64

	// Optional: purge only for a specific virtual key
	vk := r.URL.Query().Get("vk")
	
	var l1Pattern, l2Pattern string
	if vk != "" {
		l1Pattern = "synapse:l1cache:" + vk + ":*"
		l2Pattern = "synapse:l2cache:" + vk + ":*"
	} else {
		l1Pattern = "synapse:l1cache:*"
		l2Pattern = "synapse:l2cache:*"
	}

	// Purge L1 cache
	l1Keys, err := rdb.Keys(ctx, l1Pattern).Result()
	if err == nil && len(l1Keys) > 0 {
		n, _ := rdb.Del(ctx, l1Keys...).Result()
		deleted += n
	}

	// Purge L2 cache
	l2Keys, err := rdb.Keys(ctx, l2Pattern).Result()
	if err == nil && len(l2Keys) > 0 {
		n, _ := rdb.Del(ctx, l2Keys...).Result()
		deleted += n
	}

	log.Printf("Cache purged: %d keys deleted (vk=%s)", deleted, vk)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": deleted,
		"message": fmt.Sprintf("Purged %d cache entries (L1 + L2)", deleted),
	})
}
