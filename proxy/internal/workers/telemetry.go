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
func PushTelemetry(
	vk, provider, model string,
	promptOrig, completionOrig, promptOpt, completionOpt, reasoningTokens int,
	cacheLevel string, duration time.Duration,
	origPayload, optPayload string,
	cacheCreationTokens, cacheReadTokens, cacheHitTokens, cacheMissTokens int,
	agentID, agentLabel, sessionID string,
	zeroLog bool,
) {
	ctx := context.Background()
	rdb := db.GetRedis()

	// Calculate the 4-class savings breakdown using the per-class pricing model.
	// cacheRead and cacheCreation tokens in the ORIG are the ones that contributed
	// to the input bill at the cache rate. We assume the L3/optimization kept the
	// same cache profile (proportional reduction), so cacheReadSaved/cacheCreationSaved
	// are estimated as the share of promptSaved that came from those classes.
	promptSaved := promptOrig - promptOpt
	if promptSaved < 0 {
		promptSaved = 0
	}
	compSaved := completionOrig - completionOpt
	if compSaved < 0 {
		compSaved = 0
	}
	// Estimate per-class deltas: proportional to the original cache share.
	// If the original prompt had cacheRead = 30% of input, we assume 30% of promptSaved
	// also came from cache_read. This is an approximation; the exact decomposition would
	// require the optimized-side cache profile which we don't have here.
	totalInputOrig := promptOrig
	if totalInputOrig <= 0 {
		totalInputOrig = 1
	}
	shareRead := float64(cacheReadTokens) / float64(totalInputOrig)
	shareCreation := float64(cacheCreationTokens) / float64(totalInputOrig)
	estimatedReadSaved := int(float64(promptSaved) * shareRead)
	estimatedCreationSaved := int(float64(promptSaved) * shareCreation)

	breakdown := utils.CalculateSavingsByClass(
		provider, model,
		promptSaved, compSaved,
		estimatedReadSaved, estimatedCreationSaved,
	)

	logData := map[string]interface{}{
		"virtual_key":           vk,
		"provider":              provider,
		"model":                 model,
		"prompt_orig":           promptOrig,
		"comp_orig":             completionOrig,
		"prompt_opt":            promptOpt,
		"comp_opt":              completionOpt,
		"reasoning_tokens":      reasoningTokens,
		"cache_level":           cacheLevel,
		"cache_creation_tokens": cacheCreationTokens,
		"cache_read_tokens":     cacheReadTokens,
		"cache_hit_tokens":      cacheHitTokens,
		"cache_miss_tokens":     cacheMissTokens,
		"savings_input_fresh":   breakdown.InputFreshSaved,
		"savings_cache_read":    breakdown.CacheReadSaved,
		"savings_cache_creation": breakdown.CacheCreationSaved,
		"savings_output":        breakdown.OutputSaved,
		"duration_ms":           duration.Milliseconds(),
		"agent_id":              agentID,
		"agent_label":           agentLabel,
		"session_id":            sessionID,
	}

	// Zero-Log Mode: drop the prompt/response content. The metadata
	// (token counts, latency, agent id) is still pushed. We use empty
	// strings instead of skipping XAdd entirely so the worker still
	// observes the request volume (otherwise dashboards would show
	// zero traffic on Zero-Log keys, which is misleading).
	if zeroLog {
		origPayload = ""
		optPayload = ""
	}
	logData["orig_payload"] = origPayload
	logData["opt_payload"] = optPayload
	_ = zeroLog // already used above

	_, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: "optitoken:telemetry:logs",
		// Cap the stream at 100k entries. The "~" is approximate
		// trimming (O(1) amortized) so we don't pay a linear cost on
		// every XAdd. At 100k entries (~50 KB each on average) the
		// stream is bounded around 5 MB; old entries are evicted
		// automatically as new ones arrive.
		MaxLen: 100000,
		Approx: true,
		Values: logData,
	}).Result()

	if err != nil {
		log.Printf("Failed to push telemetry: %v", err)
	}
}

// RunTelemetryMigrations applies the additive schema changes required by
// the telemetry worker. Idempotent; safe to call on every boot. Should
// be invoked once from main.go, not from the worker loop.
func RunTelemetryMigrations() {
	postgresDB := db.GetDB()
	if postgresDB == nil {
		return
	}
	// RequestLog columns (telemetry)
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "reasoningTokens" INTEGER NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (reasoningTokens): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "cacheCreationTokens" INTEGER NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (cacheCreationTokens): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "cacheReadTokens" INTEGER NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (cacheReadTokens): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "cacheHitTokens" INTEGER NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (cacheHitTokens): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "cacheMissTokens" INTEGER NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (cacheMissTokens): %v", err)
	}
	// Per-class savings (4 columns)
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "savingsInputFresh" DOUBLE PRECISION NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (savingsInputFresh): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "savingsCacheRead" DOUBLE PRECISION NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (savingsCacheRead): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "savingsCacheCreation" DOUBLE PRECISION NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (savingsCacheCreation): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "savingsOutput" DOUBLE PRECISION NOT NULL DEFAULT 0`); err != nil {
		log.Printf("Telemetry migration warning (savingsOutput): %v", err)
	}
	// Agent detection columns (for live telemetry grouping)
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "agentId" TEXT NOT NULL DEFAULT ''`); err != nil {
		log.Printf("Telemetry migration warning (agentId): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "agentLabel" TEXT NOT NULL DEFAULT ''`); err != nil {
		log.Printf("Telemetry migration warning (agentLabel): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "sessionId" TEXT NOT NULL DEFAULT ''`); err != nil {
		log.Printf("Telemetry migration warning (sessionId): %v", err)
	}
	// Indexes for fast grouping
	if _, err := postgresDB.Exec(`CREATE INDEX IF NOT EXISTS "RequestLog_agentId_idx" ON "RequestLog" ("agentId")`); err != nil {
		log.Printf("Telemetry migration warning (agentId idx): %v", err)
	}
	if _, err := postgresDB.Exec(`CREATE INDEX IF NOT EXISTS "RequestLog_sessionId_idx" ON "RequestLog" ("sessionId")`); err != nil {
		log.Printf("Telemetry migration warning (sessionId idx): %v", err)
	}
	if _, err := postgresDB.Exec(`CREATE INDEX IF NOT EXISTS "RequestLog_agentId_createdAt_idx" ON "RequestLog" ("agentId", "createdAt" DESC)`); err != nil {
		log.Printf("Telemetry migration warning (agentId_createdAt idx): %v", err)
	}
	// ProviderModel columns (pricing per class)
	if _, err := postgresDB.Exec(`ALTER TABLE "ProviderModel" ADD COLUMN IF NOT EXISTS "costCachedInputPer1M" DOUBLE PRECISION`); err != nil {
		log.Printf("Telemetry migration warning (ProviderModel.costCachedInputPer1M): %v", err)
	}
	if _, err := postgresDB.Exec(`ALTER TABLE "ProviderModel" ADD COLUMN IF NOT EXISTS "costCacheWritePer1M" DOUBLE PRECISION`); err != nil {
		log.Printf("Telemetry migration warning (ProviderModel.costCacheWritePer1M): %v", err)
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
			reasoningTokens, _ := strconv.Atoi(fmt.Sprint(msg.Values["reasoning_tokens"]))
			cacheCreationTokens, _ := strconv.Atoi(fmt.Sprint(msg.Values["cache_creation_tokens"]))
			cacheReadTokens, _ := strconv.Atoi(fmt.Sprint(msg.Values["cache_read_tokens"]))
			cacheHitTokens, _ := strconv.Atoi(fmt.Sprint(msg.Values["cache_hit_tokens"]))
			cacheMissTokens, _ := strconv.Atoi(fmt.Sprint(msg.Values["cache_miss_tokens"]))
			savingsInputFresh, _ := strconv.ParseFloat(fmt.Sprint(msg.Values["savings_input_fresh"]), 64)
			savingsCacheRead, _ := strconv.ParseFloat(fmt.Sprint(msg.Values["savings_cache_read"]), 64)
			savingsCacheCreation, _ := strconv.ParseFloat(fmt.Sprint(msg.Values["savings_cache_creation"]), 64)
			savingsOutput, _ := strconv.ParseFloat(fmt.Sprint(msg.Values["savings_output"]), 64)
			durMs, _ := strconv.Atoi(fmt.Sprint(msg.Values["duration_ms"]))
			agentID := fmt.Sprint(msg.Values["agent_id"])
			agentLabel := fmt.Sprint(msg.Values["agent_label"])
			sessionID := fmt.Sprint(msg.Values["session_id"])
			if agentID == "<nil>" {
				agentID = ""
			}
			if agentLabel == "<nil>" {
				agentLabel = ""
			}
			if sessionID == "<nil>" {
				sessionID = ""
			}

			promptSaved := promptOrig - promptOpt
			if promptSaved < 0 {
				promptSaved = 0
			}
			compSaved := compOrig - compOpt
			if compSaved < 0 {
				compSaved = 0
			}

			// costSaved is the SUM of the 4 per-class savings (more accurate than the
			// legacy CalculateSavings which priced everything at input rate).
			costSaved := savingsInputFresh + savingsCacheRead + savingsCacheCreation + savingsOutput

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
				INSERT INTO "RequestLog" (id, "apiKeyId", provider, model, "agentId", "agentLabel", "sessionId", "promptTokensOrig", "completionTokensOrig", "promptTokensOpt", "completionTokensOpt", "reasoningTokens", "cacheCreationTokens", "cacheReadTokens", "cacheHitTokens", "cacheMissTokens", "costSaved", "savingsInputFresh", "savingsCacheRead", "savingsCacheCreation", "savingsOutput", "cacheLevel", "durationMs", "originalPayload", "optimizedPayload", "createdAt")
				SELECT gen_random_uuid(), id, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, NOW() FROM "ApiKey" WHERE "virtualKey" = $1
			`
			_, err = postgresDB.Exec(query, vk, prov, model, agentID, agentLabel, sessionID, promptOrig, compOrig, promptOpt, compOpt, reasoningTokens, cacheCreationTokens, cacheReadTokens, cacheHitTokens, cacheMissTokens, costSaved, savingsInputFresh, savingsCacheRead, savingsCacheCreation, savingsOutput, cacheLvl, durMs, origPayload, optPayload)

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

	// Prompt-cache aggregates per provider (Anthropic, OpenAI, DeepSeek, Google)
	// Returns: provider, creation_tokens, read_tokens, hit_tokens, miss_tokens
	rowsCache, err := postgresDB.Query(`
		SELECT provider,
			COALESCE(SUM("cacheCreationTokens"), 0),
			COALESCE(SUM("cacheReadTokens"), 0),
			COALESCE(SUM("cacheHitTokens"), 0),
			COALESCE(SUM("cacheMissTokens"), 0),
			COUNT(id)
		FROM "RequestLog"
		GROUP BY provider
	`)
	type cacheAgg struct {
		Creation int `json:"creation"`
		Read     int `json:"read"`
		Hit      int `json:"hit"`
		Miss     int `json:"miss"`
		Count    int `json:"count"`
	}
	cacheByProvider := map[string]*cacheAgg{}
	if err == nil {
		defer rowsCache.Close()
		for rowsCache.Next() {
			var prov string
			var creation, read, hit, miss, cnt int
			if err := rowsCache.Scan(&prov, &creation, &read, &hit, &miss, &cnt); err == nil {
				cacheByProvider[prov] = &cacheAgg{Creation: creation, Read: read, Hit: hit, Miss: miss, Count: cnt}
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
		"totalRequests":     totalRequests,
		"totalCostSaved":    totalCostSaved,
		"tokensSent":        tokensSent,
		"tokensOptimized":   tokensOptimized,
		"tokensPurged":      tokensPurged,
		"compressionRatio":  compressionRatio,
		"cacheDistribution": cacheDist,
		"topModels":         topModels,
		"hourlyActivity":    hourlyActivity,
		"modelsDistribution": modelsDistribution,
		"cacheByProvider":   cacheByProvider,
		"lastUpdated":       time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(stats)
	if err == nil {
		rdb.Set(ctx, "optitoken:global_stats", jsonData, 10*time.Minute)
	} else {
		log.Printf("GlobalStats: Error marshaling json: %v", err)
	}
}
