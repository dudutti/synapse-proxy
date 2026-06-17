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

	// 3. Run idempotent schema migrations (called once, not from worker loops)
	workers.RunTelemetryMigrations()

	// 4. Setup Redis Vector Index
	go db.InitRedisIndex()

	// 5. Start Background Workers
	go workers.ConsumeTelemetryWorker()
	go workers.ConsumeBenchmarkWorker()
	go workers.GlobalStatsWorker() // Aggregates global stats into Redis every 5min (public login page)
	go db.InitPricingSyncer()     // Fetches model pricing from ProviderModel every hour (cost-saved accuracy)

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
