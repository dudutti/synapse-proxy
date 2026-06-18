#!/bin/sh
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db -c '
SELECT id, "createdAt", "promptTokensOrig", "promptTokensOpt", "completionTokensOrig", "completionTokensOpt", "latencyOriginalMs", "latencyOptimizedMs", "aiReliabilityScore"
FROM "BenchmarkLog"
WHERE "createdAt" > NOW() - INTERVAL '"'"'1 hour'"'"'
ORDER BY "createdAt" ASC
LIMIT 10;
'
