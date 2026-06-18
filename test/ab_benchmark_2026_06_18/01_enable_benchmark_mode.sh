#!/bin/sh
# Enable benchmark mode for the user's virtual key so the proxy
# fires both a control and an optimized request for every call.
docker exec optitoken-postgres psql -U optitoken_admin -d optitoken_db <<SQL
UPDATE "ApiKey" SET "benchmarkMode" = true WHERE "virtualKey" = '${VIRTUAL_KEY}';
SELECT "virtualKey", "benchmarkMode" FROM "ApiKey" WHERE "virtualKey" = '${VIRTUAL_KEY}';
SQL

# Sync to Redis (the proxy reads from Redis, not Postgres, at runtime)
echo "--- Redis before sync ---"
docker exec optitoken-redis redis-cli HGETALL 'optitoken:keys:${VIRTUAL_KEY}' | head -20

# Update the Redis hash to set benchmark_mode=true
docker exec optitoken-redis redis-cli HSET 'optitoken:keys:${VIRTUAL_KEY}' benchmark_mode true

echo "--- Redis after sync ---"
docker exec optitoken-redis redis-cli HGET 'optitoken:keys:${VIRTUAL_KEY}' benchmark_mode
