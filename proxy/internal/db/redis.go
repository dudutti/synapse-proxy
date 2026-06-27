package db

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

var rdbClient *redis.Client

// InitRedis connects to Redis and returns the client.
// SECURITY: if REDIS_PASSWORD is set in the environment, the
// client will AUTH on every connection. This is the only way
// to talk to a production-grade Redis that has `requirepass`
// configured (see docs/SECURITY.md for the incident that
// motivated this change: a Redis 7.4.7 instance exposed on
// the public internet without authentication, on the prod
// network at 167.233.60.226).
func InitRedis() {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")
	rdbClient = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword, // empty = no AUTH
		DB:       0,             // use default DB
		Protocol: 2,             // Force RESP2 to return arrays for FT.SEARCH
	})
}

// GetRedis returns the active Redis client
func GetRedis() *redis.Client {
	return rdbClient
}

// InitRedisIndex creates the Vector Similarity Search index required by L2 Cache and Tool Cache
func InitRedisIndex() {
	ctx := context.Background()
	
	// Drop old index to recreate with vk TAG field
	rdbClient.Do(ctx, "FT.DROPINDEX", "idx:l2cache").Err()
	
	// Create index with vk TAG for per-user filtering (384 dims for paraphrase-multilingual-MiniLM-L12-v2)
	err := rdbClient.Do(ctx, "FT.CREATE", "idx:l2cache", "ON", "HASH", "PREFIX", "1", "synapse:l2cache:", "SCHEMA", "vk", "TAG", "vector", "VECTOR", "FLAT", "6", "TYPE", "FLOAT32", "DIM", "384", "DISTANCE_METRIC", "COSINE", "response", "TEXT").Err()
	
	if err != nil && !strings.Contains(err.Error(), "Index already exists") {
		log.Printf("Redis VSS Index creation issue (safe to ignore if not redis-stack): %v", err)
	}

	// Create tool VSS index
	rdbClient.Do(ctx, "FT.DROPINDEX", "idx:toolcache").Err()
	errTool := rdbClient.Do(ctx, "FT.CREATE", "idx:toolcache", "ON", "HASH", "PREFIX", "1", "synapse:toolcache:", "SCHEMA", "vk", "TAG", "tool", "TAG", "vector", "VECTOR", "FLAT", "6", "TYPE", "FLOAT32", "DIM", "384", "DISTANCE_METRIC", "COSINE", "arguments", "TEXT", "response", "TEXT").Err()
	if errTool != nil && !strings.Contains(errTool.Error(), "Index already exists") {
		log.Printf("Redis Tool VSS Index creation issue: %v", errTool)
	}
}
