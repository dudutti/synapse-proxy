#!/bin/sh
# Send 5 sequential identical requests to the proxy with the
# benchmark-mode virtual key. Each request simulates a Hermes
# agent: a long system prompt, several user/assistant turns,
# and a recent CoT that the L3 compressor can prune. The
# prefix (system prompt + older history) is byte-identical
# across all 5 requests, so the provider cache should hit on
# requests 2..5 IF the prefix-preserving L3 is doing its job.

set -e

# Build a Hermes-style payload with:
#   - 1 system message (long, identical across requests)
#   - 2 user/assistant history turns (older, identical prefix)
#   - 1 user message (recent, same per call to allow L1 cache test)
#   - 1 assistant message (recent, same per call)
# We need >= 4 messages so CompressPayloadCachePreserving can
# split. The recent CoT is in the 3rd message (assistant old).

# Compose a payload with 5 messages and a 30k-token system
# prompt to make the cache savings visible.

SYSTEM_PROMPT=$(python3 -c "
import sys
# 30k chars = ~7.5k tokens (rough 4 chars/token)
print('You are Hermes, an expert software engineer. ' * 700)
")

# Older user/assistant history (prefix — should be byte-identical)
HISTORY='{"role":"user","content":"Refactor the auth middleware"},{"role":"assistant","content":"I will plan: 1) parse token 2) check redis 3) inject user into context"}'

# Recent (tail)
USER1='{"role":"user","content":"add JWT validation"}'
ASSISTANT1='{"role":"assistant","content":"<thought>Need to validate JWT signature, check expiration, and handle refresh tokens gracefully</thought>Adding JWT validation to the middleware chain now."}'
USER2='{"role":"user","content":"deploy"}'

PAYLOAD=$(cat <<EOF
{
  "model": "MiniMax-M3",
  "messages": [
    {"role":"system","content":"$SYSTEM_PROMPT"},
    $HISTORY,
    $USER1,
    $ASSISTANT1,
    $USER2
  ],
  "max_tokens": 100,
  "stream": false
}
EOF
)

echo "Payload size: $(echo -n "$PAYLOAD" | wc -c) bytes"
echo ""

# Send 5 identical requests
for i in 1 2 3 4 5; do
  echo "--- Request $i ---"
  docker exec optitoken-dashboard wget -qO- \
    --post-data="$PAYLOAD" \
    --header="Content-Type: application/json" \
    --header="Authorization: Bearer ${VIRTUAL_KEY}" \
    "http://proxy:8080/v1/chat/completions" \
    2>&1 | python3 -c "
import sys, json
try:
  d = json.load(sys.stdin)
  if 'usage' in d:
    print('  usage:', d['usage'])
  else:
    print('  response (truncated):', str(d)[:200])
except Exception as e:
  print('  parse error:', e, '| raw:', sys.stdin.read()[:200])
"
  sleep 3  # throttle to avoid hammering
done

echo ""
echo "--- Sleeping 15s for the benchmark worker to flush to DB ---"
sleep 15
