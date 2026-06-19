#!/bin/bash
# End-to-end verification script for Option 3 tool cache logic and soft loops.

set -eu

VIRTUAL_KEY="sk-opti-65adbf50bead3173b0062a09c63e785e"
PROXY_URL="http://optitoken-proxy:8080/v1/chat/completions"

echo "=== Option 3 E2E Verification ==="
echo "Using virtual key: $VIRTUAL_KEY"

# Define temporary payload files inside the runner container
cat << 'EOF' > /tmp/payload_readonly.json
{
  "model": "MiniMax-M3",
  "messages": [
    {"role": "user", "content": "Check my todo list"},
    {
      "role": "assistant",
      "content": null,
      "tool_calls": [
        {
          "id": "call_ro_1",
          "type": "function",
          "function": {
            "name": "todo",
            "arguments": "{\"action\":\"list\"}"
          }
        }
      ]
    },
    {
      "role": "tool",
      "name": "todo",
      "tool_call_id": "call_ro_1",
      "content": "[\"buy milk\", \"call bob\"]"
    },
    {"role": "user", "content": "Is there anything else?"}
  ]
}
EOF

cat << 'EOF' > /tmp/payload_stateful.json
{
  "model": "MiniMax-M3",
  "messages": [
    {"role": "user", "content": "Save my note to file"},
    {
      "role": "assistant",
      "content": null,
      "tool_calls": [
        {
          "id": "call_st_1",
          "type": "function",
          "function": {
            "name": "write_file",
            "arguments": "{\"path\":\"note.txt\",\"content\":\"hello\"}"
          }
        }
      ]
    },
    {
      "role": "tool",
      "name": "write_file",
      "tool_call_id": "call_st_1",
      "content": "success"
    },
    {"role": "user", "content": "Thank you."}
  ]
}
EOF

echo ""
echo "--- 1. Testing Read-Only Tool (todo) ---"
echo "Request 1 (ReadOnly): Should go upstream (MISS)..."
curl -s -i -X POST \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/payload_readonly.json \
  "$PROXY_URL" > /tmp/resp_ro_1.txt

# Extract cache headers
grep -E -i "x-synapseproxy-cache|HTTP" /tmp/resp_ro_1.txt || true

echo "Sleeping 5s..."
sleep 5

echo "Request 2 (ReadOnly, 5s later): Should be a HIT..."
curl -s -i -X POST \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/payload_readonly.json \
  "$PROXY_URL" > /tmp/resp_ro_2.txt
grep -E -i "x-synapseproxy-cache|HTTP" /tmp/resp_ro_2.txt || true

echo "Sleeping 65s to verify read-only bypasses the 60s cache replay limit..."
sleep 65

echo "Request 3 (ReadOnly, 70s later): Should STILL be a HIT (re-served indefinitely)..."
curl -s -i -X POST \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/payload_readonly.json \
  "$PROXY_URL" > /tmp/resp_ro_3.txt
grep -E -i "x-synapseproxy-cache|HTTP" /tmp/resp_ro_3.txt || true


echo ""
echo "--- 2. Testing Stateful Tool (write_file) ---"
echo "Request 1 (Stateful): Should go upstream (MISS)..."
curl -s -i -X POST \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/payload_stateful.json \
  "$PROXY_URL" > /tmp/resp_st_1.txt
grep -E -i "x-synapseproxy-cache|HTTP" /tmp/resp_st_1.txt || true

echo "Sleeping 10s..."
sleep 10

echo "Request 2 (Stateful, 10s later): Should be a HIT (age <= 60s)..."
curl -s -i -X POST \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/payload_stateful.json \
  "$PROXY_URL" > /tmp/resp_st_2.txt
grep -E -i "x-synapseproxy-cache|HTTP" /tmp/resp_st_2.txt || true

echo "Sleeping 65s..."
sleep 65

echo "Request 3 (Stateful, 75s later): Should be a MISS (age > 60s limit)..."
curl -s -i -X POST \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d @/tmp/payload_stateful.json \
  "$PROXY_URL" > /tmp/resp_st_3.txt
grep -E -i "x-synapseproxy-cache|HTTP" /tmp/resp_st_3.txt || true

echo ""
echo "--- 3. Testing Soft Loop Detect (429) ---"
echo "Sending 5 distinct stateful requests in rapid succession to trigger the loop counter..."
for i in 1 2 3 4 5; do
  cat << EOF > /tmp/payload_loop.json
{
  "model": "MiniMax-M3",
  "messages": [
    {"role": "user", "content": "Save my note to file $i"},
    {
      "role": "assistant",
      "content": null,
      "tool_calls": [
        {
          "id": "call_loop_$i",
          "type": "function",
          "function": {
            "name": "write_file",
            "arguments": "{\"path\":\"note.txt\",\"content\":\"hello\"}"
          }
        }
      ]
    },
    {
      "role": "tool",
      "name": "write_file",
      "tool_call_id": "call_loop_$i",
      "content": "success"
    },
    {"role": "user", "content": "Loop step $i"}
  ]
}
EOF
  echo "Sending loop request $i..."
  curl -s -i -X POST \
    -H "Authorization: Bearer $VIRTUAL_KEY" \
    -H "Content-Type: application/json" \
    -d @/tmp/payload_loop.json \
    "$PROXY_URL" > /tmp/resp_loop_$i.txt
  grep -E -i "x-synapseproxy-cache|HTTP|retry-after" /tmp/resp_loop_$i.txt || true
  sleep 1
done

echo "E2E verification completed."
