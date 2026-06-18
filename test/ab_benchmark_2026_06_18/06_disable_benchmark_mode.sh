#!/bin/sh
# Disable benchmark mode on the test key so future traffic is
# single-request again (no extra control-request overhead).
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db -c "
UPDATE \"ApiKey\" SET \"benchmarkMode\" = false WHERE \"virtualKey\" = '${VIRTUAL_KEY}';
"
docker exec optitoken-redis redis-cli HSET 'optitoken:keys:${VIRTUAL_KEY}' benchmark_mode false
echo "Benchmark mode disabled for sk-opti-dcdccb..."
