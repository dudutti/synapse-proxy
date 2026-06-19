#!/bin/sh
# Disable benchmark mode on the test key so future traffic is
# single-request again (no extra control-request overhead).
docker exec synapse-proxy-postgres psql -U synapse-proxy_admin -d synapse-proxy_db -c "
UPDATE \"ApiKey\" SET \"benchmarkMode\" = false WHERE \"virtualKey\" = '${VIRTUAL_KEY}';
"
docker exec synapse-proxy-redis redis-cli HSET 'Synapse Proxy:keys:${VIRTUAL_KEY}' benchmark_mode false
echo "Benchmark mode disabled for sk-opti-dcdccb..."
