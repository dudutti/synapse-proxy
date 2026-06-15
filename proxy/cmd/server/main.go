package main

import (
	"fmt"
	"log"
	"net/http"

	"optitoken/internal/db"
	"optitoken/internal/handlers"
	"optitoken/internal/utils"
	"optitoken/internal/workers"
)

func main() {
	log.Println("OptiToken Proxy initializing...")

	// 1. Initialize Utils (Tiktoken)
	utils.InitTiktoken()

	// 2. Initialize Database Connections
	db.InitRedis()
	db.InitPostgres()

	// 3. Setup Redis Vector Index
	go db.InitRedisIndex()

	// 4. Start Background Workers
	go workers.ConsumeTelemetryWorker()
	go workers.ConsumeBenchmarkWorker()
	go workers.GlobalStatsWorker() // Aggregates global stats into Redis every 5min (public login page)

	// 5. Mount Routes
	http.HandleFunc("/v1/chat/completions", handlers.ProxyHandler)
	http.HandleFunc("/v1/cache/purge", handlers.CachePurgeHandler)
	http.HandleFunc("/v1/providers/models", handlers.FetchModelsHandler)

	// 6. Start Server
	fmt.Println("OptiToken Data Plane listening on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
