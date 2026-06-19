#!/bin/sh
docker exec synapse-proxy-postgres psql -U synapse-proxy_admin -d synapse-proxy_db -c '
SELECT id, "createdAt", "promptTokensOrig", "promptTokensOpt", "completionTokensOrig", "completionTokensOpt", "latencyOriginalMs", "latencyOptimizedMs", "aiReliabilityScore"
FROM "BenchmarkLog"
WHERE "createdAt" > NOW() - INTERVAL '"'"'1 hour'"'"'
ORDER BY "createdAt" ASC
LIMIT 10;
'
