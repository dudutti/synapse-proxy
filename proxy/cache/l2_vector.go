package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	// Note: in a real implementation, you would use a Go binding for ONNX
	// such as "github.com/yalue/onnxruntime_go" to run the local model.
)

type VectorCache struct {
	rdb *redis.Client
}

func NewVectorCache(redisClient *redis.Client) *VectorCache {
	return &VectorCache{rdb: redisClient}
}

// CheckL1Cache does an exact string match lookup (< 1ms)
func (vc *VectorCache) CheckL1Cache(ctx context.Context, prompt string) (string, bool) {
	// Strip whitespace and hash
	cleanPrompt := strings.TrimSpace(prompt)
	hash := sha256.Sum256([]byte(cleanPrompt))
	hashStr := hex.EncodeToString(hash[:])

	val, err := vc.rdb.Get(ctx, "optitoken:l1:"+hashStr).Result()
	if err == redis.Nil {
		return "", false // MISS
	} else if err != nil {
		fmt.Printf("Redis L1 Cache Error: %v\n", err)
		return "", false
	}

	return val, true // HIT
}

// GenerateEmbedding creates a 384-dimensional vector using a local ONNX model
func (vc *VectorCache) GenerateEmbedding(text string) ([]float32, error) {
	// Boilerplate: This is where you load `all-MiniLM-L6-v2.onnx` into memory via onnxruntime_go
	// session, err := onnxruntime_go.NewSession(...)
	// err = session.Run(...)
	
	// Mock embedding generation for boilerplate
	mockVector := make([]float32, 384)
	mockVector[0] = 0.1 // stub
	return mockVector, nil
}

// Convert float32 array to bytes for Redis RediSearch binary vector format
func float32ToByte(f []float32) []byte {
	// In production, use math.Float32bits and binary.LittleEndian.PutUint32
	// to properly pack the floats into a byte array.
	return []byte("mock_byte_data")
}

// CheckL2Cache performs a KNN search on the Redis vector index (< 20ms)
func (vc *VectorCache) CheckL2Cache(ctx context.Context, prompt string) (string, bool) {
	vector, err := vc.GenerateEmbedding(prompt)
	if err != nil {
		fmt.Printf("Embedding Generation Error: %v\n", err)
		return "", false
	}

	vectorBytes := float32ToByte(vector)

	// Execute a KNN search using RediSearch module
	// Query: "*=>[KNN 1 @embedding $query_vec AS score]"
	query := "*=>[KNN 1 @embedding $query_vec AS score]"
	
	res, err := vc.rdb.Do(ctx, 
		"FT.SEARCH", 
		"optitoken:cache", 
		query, 
		"PARAMS", "2", "query_vec", vectorBytes,
		"DIALECT", "2",
		"RETURN", "2", "cached_response", "score",
	).Result()

	if err != nil {
		fmt.Printf("Redis L2 Cache Search Error: %v\n", err)
		return "", false
	}

	// Parse response
	// The result is typically an array where res[0] is the total count, 
	// res[1] is the key, res[2] is an array of [field, value, field, value]
	resSlice, ok := res.([]interface{})
	if !ok || len(resSlice) < 3 {
		return "", false // MISS
	}

	fields, ok := resSlice[2].([]interface{})
	if !ok {
		return "", false // MISS
	}

	var cachedResponse string
	var score string

	for i := 0; i < len(fields); i += 2 {
		key := string(fields[i].([]byte))
		val := string(fields[i+1].([]byte))
		if key == "cached_response" {
			cachedResponse = val
		} else if key == "score" {
			score = val
		}
	}

	// Cosine Similarity check: RediSearch returns cosine distance (1 - similarity).
	// So a similarity >= 0.92 means distance <= 0.08
	// Parsing logic omitted for brevity in boilerplate
	fmt.Printf("Found match with score: %s\n", score)

	if cachedResponse != "" {
		return cachedResponse, true // HIT
	}

	return "", false // MISS
}
