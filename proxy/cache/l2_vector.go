package cache

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/redis/go-redis/v9"
)

type VectorCache struct {
	rdb      *redis.Client
embedder Embedder
}

func NewVectorCache(redisClient *redis.Client, embedder Embedder) *VectorCache {
	return &VectorCache{rdb: redisClient, embedder: embedder}
}

// CheckL1Cache does an exact string match lookup (< 1ms)
func (vc *VectorCache) CheckL1Cache(ctx context.Context, prompt string) (string, bool) {
	// Strip whitespace and hash
	cleanPrompt := strings.TrimSpace(prompt)
	hash := sha256.Sum256([]byte(cleanPrompt))
	hashStr := hex.EncodeToString(hash[:])

	val, err := vc.rdb.Get(ctx, "synapse:l1:"+hashStr).Result()
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
	if vc.embedder == nil {
		return nil, errors.New("native embedder is not initialized")
	}
	return vc.embedder.GenerateEmbedding(text)
}

// Convert float32 array to bytes for Redis RediSearch binary vector format
func float32ToByte(f []float32) []byte {
	buf := make([]byte, len(f)*4)
	for i, val := range f {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(val))
	}
	return buf
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
	query := "*=>[KNN 1 @embedding $query_vec AS score]"
	
	res, err := vc.rdb.Do(ctx, 
		"FT.SEARCH", 
		"synapse:cache", 
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

	fmt.Printf("Found match with score: %s\n", score)

	if cachedResponse != "" {
		return cachedResponse, true // HIT
	}

	return "", false // MISS
}

