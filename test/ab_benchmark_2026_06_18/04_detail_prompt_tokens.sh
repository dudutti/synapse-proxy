#!/bin/sh
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db -c "
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
