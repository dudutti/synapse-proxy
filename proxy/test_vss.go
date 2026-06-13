package main

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
)

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", Protocol: 2})
	ctx := context.Background()

	// List keys
	keys, _ := rdb.Keys(ctx, "optitoken:l2cache:*").Result()
	fmt.Println("L2 Cache Keys:", keys)

	for _, k := range keys {
		val, _ := rdb.HGetAll(ctx, k).Result()
		fmt.Printf("Key %s has vector: %v (len %d), response: %v\n", k, val["vector"] != "", len(val["vector"]), val["response"] != "")
	}

    // Attempt a KNN search if any key exists
    if len(keys) > 0 {
        vector, _ := rdb.HGet(ctx, keys[0], "vector").Bytes()
        res, err := rdb.Do(ctx, "FT.SEARCH", "idx:l2cache", "*=>[KNN 10 @vector $query_vec AS score]", "PARAMS", "2", "query_vec", vector, "RETURN", "2", "score", "response", "DIALECT", "2").Result()
        fmt.Println("Search error:", err)
        if err == nil {
            resArr := res.([]interface{})
            fmt.Println("Number of hits:", resArr[0])
            if len(resArr) > 2 {
                for i := 2; i < len(resArr); i+=2 {
                    fields := resArr[i].([]interface{})
                    fmt.Printf("Doc %d fields: %v\n", i/2, fields)
                }
            }
        }
    }
}
