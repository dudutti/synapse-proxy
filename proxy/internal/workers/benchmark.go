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

// ConsumeBenchmarkWorker continuously reads benchmark logs from Redis and inserts them into Postgres
func ConsumeBenchmarkWorker() {
	ctx := context.Background()
	rdb := db.GetRedis()
	postgresDB := db.GetDB()

	rdb.XGroupCreateMkStream(ctx, "optitoken:benchmark_logs", "benchmark_group", "0").Err()
	log.Println("Background Benchmark Worker Started")

	for {
		res, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    "benchmark_group",
			Consumer: "worker-1",
			Streams:  []string{"optitoken:benchmark_logs", ">"},
			Count:    5,
			Block:    2 * time.Second,
		}).Result()

		if err != nil || len(res) == 0 {
			continue
		}

		for _, msg := range res[0].Messages {
			vk := fmt.Sprint(msg.Values["vk"])
			origPrompt := fmt.Sprint(msg.Values["orig_prompt"])
			optPrompt := origPrompt
			if op, ok := msg.Values["opt_prompt"]; ok && op != nil {
				optPrompt = fmt.Sprint(op)
			}
			optResp := fmt.Sprint(msg.Values["opt_resp"])
			origResp := fmt.Sprint(msg.Values["orig_resp"])
			optMs, _ := strconv.Atoi(fmt.Sprint(msg.Values["opt_ms"]))
			origMs, _ := strconv.Atoi(fmt.Sprint(msg.Values["orig_ms"]))
			
			// Extract mock score and feedback
			score := 95
			feedback := "Fallback mocked score"
			if s, ok := msg.Values["score"]; ok {
				score, _ = strconv.Atoi(fmt.Sprint(s))
			}
			if f, ok := msg.Values["feedback"]; ok {
				feedback = fmt.Sprint(f)
			}

			// Exact tokens from optimization engine using tokenizer
			promptOrig := utils.CountTokens(origPrompt)
			completionOrig := utils.CountTokens(origResp)

			// Parse real usage if present in the unoptimized response
			pOrig, cOrig := utils.ExtractUsage([]byte(origResp))
			if pOrig > 0 { promptOrig = pOrig }
			if cOrig > 0 { completionOrig = cOrig }

			promptOpt := 0
			if po, ok := msg.Values["opt_prompt_tokens"]; ok {
				promptOpt, _ = strconv.Atoi(fmt.Sprint(po))
			}
			completionOpt := 0
			if co, ok := msg.Values["opt_completion_tokens"]; ok {
				completionOpt, _ = strconv.Atoi(fmt.Sprint(co))
			}

			query := `
				INSERT INTO "BenchmarkLog" (id, "apiKeyId", "originalPrompt", "optimizedPrompt", "originalResponse", "optimizedResponse", "latencyOriginalMs", "latencyOptimizedMs", "promptTokensOrig", "completionTokensOrig", "promptTokensOpt", "completionTokensOpt", "aiReliabilityScore", "aiFeedback", "createdAt")
				SELECT gen_random_uuid(), id, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW() FROM "ApiKey" WHERE "virtualKey" = $1
			`
			_, err = postgresDB.Exec(query, vk, origPrompt, optPrompt, origResp, optResp, origMs, optMs, promptOrig, completionOrig, promptOpt, completionOpt, score, feedback)

			if err == nil {
				rdb.XAck(ctx, "optitoken:benchmark_logs", "benchmark_group", msg.ID)
			} else {
				log.Printf("Benchmark DB Insert failed: %v", err)
			}
		}
	}
}
