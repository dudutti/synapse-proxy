#!/bin/sh
docker exec synapse-proxy-postgres psql -U synapse-proxy_admin -d synapse-proxy_db -c "
SELECT
  id,
  \"createdAt\",
  \"promptTokensOrig\",
  \"promptTokensOpt\",
  \"completionTokensOrig\",
  \"completionTokensOpt\",
  LENGTH(\"originalPrompt\") as orig_len,
  LENGTH(\"optimizedPrompt\") as opt_len,
  \"aiReliabilityScore\"
FROM \"BenchmarkLog\"
WHERE \"createdAt\" > NOW() - INTERVAL '1 hour'
ORDER BY \"createdAt\" ASC
LIMIT 10;
"
