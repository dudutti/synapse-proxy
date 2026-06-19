package workers

import (
	"context"
	"log"
	"time"

	"synapse-proxy/internal/db"
)

// RetentionWorker runs periodically to enforce data retention policies based on tier.
// - FREE tier: 7 days retention
// - Premium tiers (PRO_1, PRO_2): 30 days retention
func RetentionWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Println("Retention Worker Started")

	for {
		runRetentionCleanup()
		<-ticker.C
	}
}

func runRetentionCleanup() {
	postgresDB := db.GetDB()
	if postgresDB == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Clean FREE tier (7 days)
	resFree, err := postgresDB.ExecContext(ctx, `
		DELETE FROM "RequestLog"
		USING "ApiKey"
		WHERE "RequestLog"."apiKeyId" = "ApiKey"."id"
		  AND "ApiKey"."tier" = 'FREE'
		  AND "RequestLog"."createdAt" < NOW() - INTERVAL '7 days'
	`)
	if err != nil {
		log.Printf("Retention Worker: failed to clean FREE tier logs: %v", err)
	} else {
		affected, _ := resFree.RowsAffected()
		if affected > 0 {
			log.Printf("Retention Worker: deleted %d rows for FREE tier (older than 7 days)", affected)
		}
	}

	// 2. Clean Premium tiers (30 days)
	resPro, err := postgresDB.ExecContext(ctx, `
		DELETE FROM "RequestLog"
		USING "ApiKey"
		WHERE "RequestLog"."apiKeyId" = "ApiKey"."id"
		  AND "ApiKey"."tier" != 'FREE'
		  AND "RequestLog"."createdAt" < NOW() - INTERVAL '30 days'
	`)
	if err != nil {
		log.Printf("Retention Worker: failed to clean PRO tier logs: %v", err)
	} else {
		affected, _ := resPro.RowsAffected()
		if affected > 0 {
			log.Printf("Retention Worker: deleted %d rows for PRO tiers (older than 30 days)", affected)
		}
	}
}
