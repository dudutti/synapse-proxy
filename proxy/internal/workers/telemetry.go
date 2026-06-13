package workers

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"optitoken/internal/db"
	"optitoken/internal/utils"

	"github.com/redis/go-redis/v9"
)

// PushTelemetry logs request details into Redis Streams
func PushTelemetry(vk, provider, model string, promptOrig, completionOrig, promptOpt, completionOpt int, cacheLevel string, duration time.Duration) {
	ctx := context.Background()
	rdb := db.GetRedis()

	logData := map[string]interface{}{
		"virtual_key": vk,
		"provider":    provider,
		"model":       model,
		"prompt_orig": promptOrig,
		"comp_orig":   completionOrig,
		"prompt_opt":  promptOpt,
		"comp_opt":    completionOpt,
		"cache_level": cacheLevel,
		"duration_ms": duration.Milliseconds(),
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

			costSaved := utils.CalculateSavings(prov, promptOrig+compOrig, promptOpt+compOpt)

			// Safely parameterized query
			query := `
				INSERT INTO "RequestLog" (id, "apiKeyId", provider, model, "promptTokensOrig", "completionTokensOrig", "promptTokensOpt", "completionTokensOpt", "costSaved", "cacheLevel", "durationMs", "createdAt")
				SELECT gen_random_uuid(), id, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW() FROM "ApiKey" WHERE "virtualKey" = $1
			`
			_, err = postgresDB.Exec(query, vk, prov, model, promptOrig, compOrig, promptOpt, compOpt, costSaved, cacheLvl, durMs)

			if err == nil {
				rdb.XAck(ctx, "optitoken:telemetry:logs", "telemetry_group", msg.ID)
			} else {
				log.Printf("DB Insert failed: %v", err)
			}
		}
	}
}
