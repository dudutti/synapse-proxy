#!/bin/sh
echo "=== Last 10 BenchmarkLog rows (orig vs opt) ==="
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db -c "
SELECT
  id,
  \"createdAt\",
  \"promptTokensOrig\",
  \"promptTokensOpt\",
  \"completionTokensOrig\",
  \"completionTokensOpt\",
  \"latencyOriginalMs\",
  \"latencyOptimizedMs\",
  \"aiReliabilityScore\",
  LENGTH(\"originalPrompt\") as origPromptLen,
  LENGTH(\"optimizedPrompt\") as optPromptLen
FROM \"BenchmarkLog\"
ORDER BY \"createdAt\" DESC
LIMIT 10;
"

echo ""
echo "=== Aggregate stats ==="
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db -c "
SELECT
  COUNT(*) as n,
  AVG(\"promptTokensOrig\")::int as avg_orig_prompt,
  AVG(\"promptTokensOpt\")::int as avg_opt_prompt,
  AVG(\"completionTokensOrig\")::int as avg_orig_compl,
  AVG(\"completionTokensOpt\")::int as avg_opt_compl,
  AVG(\"latencyOriginalMs\")::int as avg_orig_ms,
  AVG(\"latencyOptimizedMs\")::int as avg_opt_ms,
  ROUND(AVG(\"aiReliabilityScore\")::numeric, 1) as avg_score
FROM \"BenchmarkLog\"
WHERE \"createdAt\" > NOW() - INTERVAL '1 hour';
"
