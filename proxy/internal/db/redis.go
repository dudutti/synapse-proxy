package db

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

var rdbClient *redis.Client

// InitRedis connects to Redis and returns the client
func InitRedis() {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdbClient = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
		Protocol: 2,  // Force RESP2 to return arrays for FT.SEARCH
	})
}

// GetRedis returns the active Redis client
func GetRedis() *redis.Client {
	return rdbClient
}

// InitRedisIndex creates the Vector Similarity Search index required by L2 Cache
func InitRedisIndex() {
	ctx := context.Background()
	
	// Drop old index to recreate with vk TAG field
	rdbClient.Do(ctx, "FT.DROPINDEX", "idx:l2cache").Err()
	
	// Create index with vk TAG for per-user filtering (384 dims for paraphrase-multilingual-MiniLM-L12-v2)
	err := rdbClient.Do(ctx, "FT.CREATE", "idx:l2cache", "ON", "HASH", "PREFIX", "1", "optitoken:l2cache:", "SCHEMA", "vk", "TAG", "vector", "VECTOR", "FLAT", "6", "TYPE", "FLOAT32", "DIM", "384", "DISTANCE_METRIC", "COSINE", "response", "TEXT").Err()
	
	if err != nil && !strings.Contains(err.Error(), "Index already exists") {
		log.Printf("Redis VSS Index creation issue (safe to ignore if not redis-stack): %v", err)
	}
}
