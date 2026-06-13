package main

import (
	"context"
	"fmt"
	"reflect"
	"github.com/redis/go-redis/v9"
)

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", Protocol: 2})
	res, err := rdb.Do(context.Background(), "FT.SEARCH", "idx:l2cache", "*").Result()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Type of res:", reflect.TypeOf(res))
	if arr, ok := res.([]interface{}); ok {
		fmt.Println("Is Array, len:", len(arr))
		if len(arr) > 2 {
			fmt.Println("Type of arr[2]:", reflect.TypeOf(arr[2]))
		}
	} else if m, ok := res.(map[interface{}]interface{}); ok {
		fmt.Println("Is Map")
	} else {
		fmt.Printf("Is something else: %T\n", res)
	}
}
