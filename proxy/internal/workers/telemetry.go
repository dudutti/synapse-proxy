package workers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"optitoken/internal/db"
	"optitoken/internal/utils"

	"github.com/redis/go-redis/v9"
)

// PushTelemetry logs request details into Redis Streams
func PushTelemetry(vk, provider, model string, promptOrig, completionOrig, promptOpt, completionOpt int, cacheLevel string, duration time.Duration, origPayload string, optPayload string) {
	ctx := context.Background()
	rdb := db.GetRedis()

	logData := map[string]interface{}{
		"virtual_key":   vk,
		"provider":      provider,
		"model":         model,
		"prompt_orig":   promptOrig,
		"comp_orig":     completionOrig,
		"prompt_opt":    promptOpt,
		"comp_opt":      completionOpt,
		"cache_level":   cacheLevel,
		"duration_ms":   duration.Milliseconds(),
		"orig_payload":  origPayload,
		"opt_payload":   optPayload,
	}

	_, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "optitoken:telemetry:logs",
		Values: logData,
	}).Result()

	if err != nil {
		log.Printf("Failed to push telemetry: %v", err)
	}
}

// ConsumeTelemetryWorker continuously reads telemetry logs from Redis and bulk inserts into Postgres
func ConsumeTelemetryWorker() {
	ctx := context.Background()
	rdb := db.GetRedis()
	postgresDB := db.GetDB()

	rdb.XGroupCreateMkStream(ctx, "optitoken:telemetry:logs", "telemetry_group", "0").Err()
	log.Println("Background Telemetry Worker Started")

	for {
		res, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    "telemetry_group",
			Consumer: "worker-1",
			Streams:  []string{"optitoken:telemetry:logs", ">"},
			Count:    10,
			Block:    2 * time.Second,
		}).Result()

		if err != nil || len(res) == 0 {
			continue
		}

		for _, msg := range res[0].Messages {
			vk := fmt.Sprint(msg.Values["virtual_key"])
			prov := fmt.Sprint(msg.Values["provider"])
			model := fmt.Sprint(msg.Values["model"])
			cacheLvl := fmt.Sprint(msg.Values["cache_level"])
			
			promptOrig, _ := strconv.Atoi(fmt.Sprint(msg.Values["prompt_orig"]))
			compOrig, _ := strconv.Atoi(fmt.Sprint(msg.Values["comp_orig"]))
			promptOpt, _ := strconv.Atoi(fmt.Sprint(msg.Values["prompt_opt"]))
			compOpt, _ := strconv.Atoi(fmt.Sprint(msg.Values["comp_opt"]))
			durMs, _ := strconv.Atoi(fmt.Sprint(msg.Values["duration_ms"]))

			promptSaved := promptOrig - promptOpt
			if promptSaved < 0 {
				promptSaved = 0
			}
			compSaved := compOrig - compOpt
			if compSaved < 0 {
				compSaved = 0
			}

			costSaved := utils.CalculateSavings(prov, model, promptSaved, compSaved)

			origPayload := fmt.Sprint(msg.Values["orig_payload"])
			optPayload := fmt.Sprint(msg.Values["opt_payload"])
			
			// If payload is empty, handle it as null or empty string, Sprint of nil is "<nil>"
			if origPayload == "<nil>" {
				origPayload = ""
			}
			if optPayload == "<nil>" {
				optPayload = ""
			}

			// Safely parameterized query
			query := `
				INSERT INTO "RequestLog" (id, "apiKeyId", provider, model, "promptTokensOrig", "completionTokensOrig", "promptTokensOpt", "completionTokensOpt", "costSaved", "cacheLevel", "durationMs", "originalPayload", "optimizedPayload", "createdAt")
				SELECT gen_random_uuid(), id, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW() FROM "ApiKey" WHERE "virtualKey" = $1
			`
			_, err = postgresDB.Exec(query, vk, prov, model, promptOrig, compOrig, promptOpt, compOpt, costSaved, cacheLvl, durMs, origPayload, optPayload)

			if err == nil {
				rdb.XAck(ctx, "optitoken:telemetry:logs", "telemetry_group", msg.ID)
			} else {
				log.Printf("DB Insert failed: %v", err)
			}
		}
	}
}

// GlobalStatsWorker aggregates telemetry stats and saves them to Redis every 5 minutes
func GlobalStatsWorker() {
	ctx := context.Background()
	postgresDB := db.GetDB()
	rdb := db.GetRedis()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	log.Println("Global Stats Worker Started")

	for {
		aggregateAndSaveStats(ctx, postgresDB, rdb)
		<-ticker.C
	}
}

func aggregateAndSaveStats(ctx context.Context, postgresDB *sql.DB, rdb *redis.Client) {
	var totalRequests int
	var totalCostSaved float64
	var tokensSent int
	var tokensOptimized int
	
	err := postgresDB.QueryRow(`
		SELECT 
			COUNT(id), 
			COALESCE(SUM("costSaved"), 0),
			COALESCE(SUM("promptTokensOrig" + "completionTokensOrig"), 0),
			COALESCE(SUM("promptTokensOpt" + "completionTokensOpt"), 0)
		FROM "RequestLog"
	`).Scan(&totalRequests, &totalCostSaved, &tokensSent, &tokensOptimized)

	if err != nil {
		log.Printf("GlobalStats: Error querying aggregates: %v", err)
		return
	}

	rows, err := postgresDB.Query(`SELECT "cacheLevel", COUNT(id) FROM "RequestLog" GROUP BY "cacheLevel"`)
	cacheDist := map[string]int{"MISS": 0, "L1": 0, "L2": 0, "L3": 0}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var level string
			var count int
			if err := rows.Scan(&level, &count); err == nil {
				cacheDist[level] = count
			}
		}
	}

	rowsModels, err := postgresDB.Query(`
		SELECT model, COUNT(id) as c 
		FROM "RequestLog" 
		GROUP BY model 
		ORDER BY c DESC LIMIT 3
	`)
	var topModels []map[string]interface{}
	if err == nil {
		defer rowsModels.Close()
		for rowsModels.Next() {
			var model string
			var count int
			if err := rowsModels.Scan(&model, &count); err == nil {
				topModels = append(topModels, map[string]interface{}{"model": model, "count": count})
			}
		}
	}

	// Hourly Activity over the last 24h
	rowsHourly, err := postgresDB.Query(`
		SELECT DATE_TRUNC('hour', "createdAt") as h, COUNT(id) 
		FROM "RequestLog" 
		WHERE "createdAt" >= NOW() - INTERVAL '24 HOURS' 
		GROUP BY h 
		ORDER BY h ASC
	`)
	var hourlyActivity []map[string]interface{}
	if err == nil {
		defer rowsHourly.Close()
		for rowsHourly.Next() {
			var h time.Time
			var count int
			if err := rowsHourly.Scan(&h, &count); err == nil {
				hourlyActivity = append(hourlyActivity, map[string]interface{}{"hour": h.Format("15:00"), "requests": count})
			}
		}
	}

	// Models distribution (all for pie chart)
	rowsModelsDist, err := postgresDB.Query(`
		SELECT model, COUNT(id) as c 
		FROM "RequestLog" 
		GROUP BY model 
		ORDER BY c DESC LIMIT 10
	`)
	var modelsDistribution []map[string]interface{}
	if err == nil {
		defer rowsModelsDist.Close()
		for rowsModelsDist.Next() {
			var model string
			var count int
			if err := rowsModelsDist.Scan(&model, &count); err == nil {
				modelsDistribution = append(modelsDistribution, map[string]interface{}{"model": model, "count": count})
			}
		}
	}

	tokensPurged := tokensSent - tokensOptimized
	if tokensPurged < 0 {
		tokensPurged = 0
	}
	
	var compressionRatio float64 = 0
	if tokensSent > 0 {
		compressionRatio = float64(tokensPurged) / float64(tokensSent) * 100.0
	}

	stats := map[string]interface{}{
		"totalRequests": totalRequests,
		"totalCostSaved": totalCostSaved,
		"tokensSent": tokensSent,
		"tokensOptimized": tokensOptimized,
		"tokensPurged": tokensPurged,
		"compressionRatio": compressionRatio,
		"cacheDistribution": cacheDist,
		"topModels": topModels,
		"hourlyActivity": hourlyActivity,
		"modelsDistribution": modelsDistribution,
		"lastUpdated": time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(stats)
	if err == nil {
		rdb.Set(ctx, "optitoken:global_stats", jsonData, 10*time.Minute)
	} else {
		log.Printf("GlobalStats: Error marshaling json: %v", err)
	}
}
