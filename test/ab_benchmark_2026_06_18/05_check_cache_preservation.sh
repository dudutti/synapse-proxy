#!/bin/sh
# Compare 2 consecutive identical requests to see if the
# originalPrompt bytes are byte-exact identical (the cache
# preservation invariant).
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db -c "
SELECT
  id,
  \"createdAt\",
  md5(\"originalPrompt\") as orig_md5,
  md5(\"optimizedPrompt\") as opt_md5,
  LENGTH(\"originalPrompt\") as orig_len,
  LENGTH(\"optimizedPrompt\") as opt_len,
  LENGTH(\"originalResponse\") as orig_resp_len,
  LENGTH(\"optimizedResponse\") as opt_resp_len
FROM \"BenchmarkLog\"
WHERE \"createdAt\" > NOW() - INTERVAL '1 hour'
ORDER BY \"createdAt\" DESC
LIMIT 5;
"
