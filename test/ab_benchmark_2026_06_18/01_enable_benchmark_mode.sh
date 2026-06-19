#!/bin/sh
# Enable benchmark mode for the user's virtual key so the proxy
# fires both a control and an optimized request for every call.
docker exec synapse-proxy-postgres psql -U synapse-proxy_admin -d synapse-proxy_db <<SQL
UPDATE "ApiKey" SET "benchmarkMode" = true WHERE "virtualKey" = '${VIRTUAL_KEY}';
SELECT "virtualKey", "benchmarkMode" FROM "ApiKey" WHERE "virtualKey" = '${VIRTUAL_KEY}';
SQL

# Sync to Redis (the proxy reads from Redis, not Postgres, at runtime)
echo "--- Redis before sync ---"
docker exec synapse-proxy-redis redis-cli HGETALL 'Synapse Proxy:keys:${VIRTUAL_KEY}' | head -20

# Update the Redis hash to set benchmark_mode=true
docker exec synapse-proxy-redis redis-cli HSET 'Synapse Proxy:keys:${VIRTUAL_KEY}' benchmark_mode true

echo "--- Redis after sync ---"
docker exec synapse-proxy-redis redis-cli HGET 'Synapse Proxy:keys:${VIRTUAL_KEY}' benchmark_mode
