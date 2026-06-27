#!/bin/bash
# Local test setup for synapse-proxy.
#
# What this does:
#  1. Starts Redis on :6379 (via Docker or a system service).
#  2. Seeds a test virtual key under synapse:keys:sk-opti-test so
#     you can hit the proxy without setting up the dashboard.
#  3. Tells you the curl command to use.
#
# Usage:
#   ./local-setup.sh                # default: real_key = empty (you fill in)
#   REAL_KEY=sk-... ./local-setup.sh  # use a real OpenAI key
#   ./local-setup.sh ollama         # point to local Ollama (free)

set -e

REDIS_CLI="${REDIS_CLI:-redis-cli}"
PROXY_URL="${PROXY_URL:-http://localhost:8080}"
TEST_KEY="sk-opti-test"
TEST_VK="synapse:keys:${TEST_KEY}"

# 1. Check Redis is up.
if ! $REDIS_CLI ping >/dev/null 2>&1; then
  echo "ERROR: Redis is not running on :6379. Start it with:"
  echo "  docker run -d -p 6379:6379 --name synapse-redis -v redis_data:/data redis:7-alpine"
  exit 1
fi
echo "Redis is up."

# 2. Choose provider + real_key.
if [ "$1" = "ollama" ]; then
  PROVIDER="openai"
  DEFAULT_MODEL="llama3.1:8b"
  REAL_KEY="${REAL_KEY:-ollama}"
  echo "Configuring for Ollama (set OLLAMA_BASE_URL=http://host.docker.internal:11434 if running proxy in Docker)"
elif [ -n "$REAL_KEY" ]; then
  PROVIDER="openai"
  DEFAULT_MODEL="gpt-4o-mini"
else
  # No real key — proxy will pass auth but upstream will reject.
  # Useful for testing the hook pipeline (auth, model radar, etc.)
  PROVIDER="openai"
  DEFAULT_MODEL="gpt-4o-mini"
  REAL_KEY="***"
  echo "WARNING: no REAL_KEY env var. Seeding with a placeholder; upstream will 401."
fi

# 3. Seed the test key.
$REDIS_CLI DEL "$TEST_VK" >/dev/null
$REDIS_CLI HMSET "$TEST_VK" \
  real_key "$REAL_KEY" \
  provider "$PROVIDER" \
  default_model "$DEFAULT_MODEL" \
  enable_l1 "true" \
  enable_l2 "true" \
  enable_l3 "true" \
  semantic_tolerance "0.15" \
  cache_ttl "86400" \
  kill_switch "false" \
  block_unknown_tools "false" \
  allowed_tools "" \
  fingerprint_loop_detect "true" \
  session_token_limit "0" \
  limit_exceeded "false" \
  fallback_key "" \
  fallback_provider "" \
  fallback_model "" \
  zero_log "false" \
  isolate_cache_by_user "false" \
  redact_pii "false" \
  tool_ttls "{}" >/dev/null
echo "Seeded $TEST_VK (provider=$PROVIDER, default_model=$DEFAULT_MODEL)"

# 4. Print the test curl.
cat <<EOF

=== Ready to test ===

# Health check
curl -s $PROXY_URL/healthz

# Chat completion (this WILL go upstream to $PROVIDER)
curl -s -X POST $PROXY_URL/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $TEST_KEY" \\
  -d '{"model":"$DEFAULT_MODEL","messages":[{"role":"user","content":"hello"}]}' | head -20

# Metrics (Prometheus)
curl -s $PROXY_URL/metrics | grep -E "(cache|panics|upstream)"

# Trigger the kill switch (set kill_switch=true on the key, then send 3 identical calls)
$REDIS_CLI HSET "$TEST_VK" kill_switch "true"
for i in 1 2 3; do
  curl -s -X POST $PROXY_URL/v1/chat/completions \\
    -H "Content-Type: application/json" \\
    -H "Authorization: Bearer $TEST_KEY" \\
    -d '{"model":"$DEFAULT_MODEL","messages":[{"role":"user","content":"loop test"}]}' -w "\\n[attempt \$i] HTTP %{http_code}\\n"
done
$REDIS_CLI HSET "$TEST_VK" kill_switch "false"

# Trigger the tool filter (denylist)
$REDIS_CLI SADD "synapse:denied_tools:${TEST_KEY}" "dangerous_tool"
curl -s -X POST $PROXY_URL/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $TEST_KEY" \\
  -d '{"model":"$DEFAULT_MODEL","messages":[{"role":"user","content":"hi"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"dangerous_tool","arguments":"{}"}}]}]}' -w "\\nHTTP %{http_code}\\n"
$REDIS_CLI SREM "synapse:denied_tools:${TEST_KEY}" "dangerous_tool"

EOF
echo "=== Setup complete ==="