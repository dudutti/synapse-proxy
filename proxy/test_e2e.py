"""
End-to-end test harness for the Synapse Proxy stack running in
Docker. Runs a sequence of curl-equivalent requests and reports
which hooks fire and what status codes come back.

Usage:
  python test_e2e.py
"""
import json
import os
import subprocess
import sys
import time
from typing import Optional

# Pre-encrypted real_key seeded in Redis via the dashboard flow.
VK = "sk-opti-b233384d75018316abba4e3c718febe3"
BASE = "http://localhost:8080"
REDIS_CONTAINER = "synapse-redis"


def curl(method: str, path: str, body: Optional[dict] = None, expect: Optional[int] = None, key: Optional[str] = None, bypass_cache: bool = False) -> tuple[int, str]:
    auth_key = key if key is not None else VK
    args = ["curl", "-i", "-s", "-X", method, BASE + path, "-H", "Content-Type: application/json", "-H", f"Authorization: Bearer {auth_key}", "-w", "\nHTTP %{http_code}\n"]
    if bypass_cache:
        args += ["-H", "X-Bypass-Cache: true"]
    if body is not None:
        args += ["-d", json.dumps(body)]
    r = subprocess.run(args, capture_output=True, text=True, timeout=30)
    out = r.stdout
    code = None
    for line in reversed(out.splitlines()):
        if line.startswith("HTTP "):
            try:
                code = int(line.split()[1])
            except Exception:
                pass
            break
    if expect is not None and code != expect:
        print(f"  [FAIL] expected {expect}, got {code}")
        print("  body:", out[:400])
    else:
        print(f"  [OK]   {method} {path} -> {code}")
    return code, out


def redis_cli(*args: str) -> str:
    r = subprocess.run(["docker", "exec", REDIS_CONTAINER, "redis-cli", "-a", "localdev-redis-pw", *args], capture_output=True, text=True, timeout=10)
    return r.stdout.strip()


def test(name: str, fn):
    print(f"\n=== {name} ===")
    fn()
    print()


def test_health():
    curl("GET", "/healthz", expect=200)
    curl("GET", "/readyz", expect=200)


def test_unknown_key():
    curl("POST", "/v1/chat/completions", body={"model": "x", "messages": []}, expect=401, key="sk-unknown-key")


def test_simple_chat():
    """End-to-end: encrypted real_key is decrypted, request forwarded
    upstream."""
    # Clear any pending loop / denylist state from previous tests.
    redis_cli("DEL", "synapse:denied_tools:" + VK)
    redis_cli("DEL", "synapse:loops:" + VK + ":test")
    redis_cli("HSET", "synapse:keys:" + VK, "kill_switch", "false")
    redis_cli("HDEL", "synapse:radar:known_models", "openai:gpt-4o-mini")
    redis_cli("DEL", "synapse:radar:models:gpt-4o-mini")

    payload1 = {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": "hello"}],
    }
    payload2 = {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": "hello  "}],
    }
    print("  First call (expect upstream)...")
    code1, out1 = curl("POST", "/v1/chat/completions", body=payload1, expect=200)
    time.sleep(1)
    print("  Second call (expect cache hit)...")
    code2, out2 = curl("POST", "/v1/chat/completions", body=payload2, expect=200)
    print("--- HEADERS & BODY ---")
    print(out2)


def test_loop_killswitch():
    """3 identical calls with kill_switch=true: 1,2 = 200/upstream, 3 = 400."""
    redis_cli("HSET", "synapse:keys:" + VK, "kill_switch", "true")
    payload = {"model": "gpt-4o-mini", "messages": [{"role": "user", "content": f"LOOP_TEST_{int(time.time()*1000)}"}]}
    for i in range(1, 4):
        code, _ = curl("POST", "/v1/chat/completions", body=payload)
        print(f"  attempt {i}: HTTP {code}")
    redis_cli("HSET", "synapse:keys:" + VK, "kill_switch", "false")


def test_tool_denylist():
    """Tool call to a denylisted name returns 403."""
    redis_cli("SADD", "synapse:denied_tools:" + VK, "dangerous_tool")
    code, _ = curl("POST", "/v1/chat/completions", body={
        "model": "gpt-4o-mini",
        "messages": [
            {"role": "user", "content": "hi"},
            {"role": "assistant", "tool_calls": [
                {"id": "c1", "type": "function", "function": {"name": "dangerous_tool", "arguments": "{}"}}
            ]},
        ],
    })
    print(f"  expected 403: got {code}")
    redis_cli("SREM", "synapse:denied_tools:" + VK, "dangerous_tool")


def test_tool_allowlist():
    """Strict mode with allowlist: unknown tool returns 400."""
    redis_cli("HSET", "synapse:keys:" + VK, "block_unknown_tools", "true", "allowed_tools", "read_file,ls")
    code, _ = curl("POST", "/v1/chat/completions", body={
        "model": "gpt-4o-mini",
        "messages": [
            {"role": "user", "content": "hi"},
            {"role": "assistant", "tool_calls": [
                {"id": "c1", "type": "function", "function": {"name": "rm_rf", "arguments": "{}"}}
            ]},
        ],
    })
    print(f"  expected 400: got {code}")
    redis_cli("HSET", "synapse:keys:" + VK, "block_unknown_tools", "false", "allowed_tools", "")


def test_session_circuit_breaker():
    """Session token limit = 100: 3 calls with same session should trip."""
    redis_cli("DEL", "synapse:session_usage:CB_TEST_SESS")
    redis_cli("HSET", "synapse:keys:" + VK, "session_token_limit", "100")
    payload = {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": "circuit breaker test"}],
    }
    headers = ["-H", "Content-Type: application/json", "-H", f"Authorization: Bearer {VK}", "-H", "X-SynapseProxy-Session: CB_TEST_SESS"]
    for i in range(1, 4):
        args = ["curl", "-s", "-X", "POST", BASE + "/v1/chat/completions", "-w", f"\nattempt {i} HTTP %{{http_code}}\n"] + headers + ["-d", json.dumps(payload)]
        r = subprocess.run(args, capture_output=True, text=True, timeout=30)
        print(f"  {r.stdout.strip()}")
    redis_cli("HSET", "synapse:keys:" + VK, "session_token_limit", "0")
    redis_cli("DEL", "synapse:session_usage:CB_TEST_SESS")


def test_ccr_retrieval():
    """Active CCR: seed a compression key in Redis, trigger a tool call in prompt,
    verify the proxy intercepts it and retrieves the original payload."""
    test_key = "abc123ccr"
    original_val = "This is the original log chunk that was compressed."
    
    # Seed the key in Redis under synapse:ccr:<key>
    redis_cli("SET", f"synapse:ccr:{test_key}", original_val)
    
    # Request that triggers tool call from upstream_mock
    payload = {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": f"TRIGGER_TOOL_CALL:{test_key} nonce:{int(time.time()*1000)}"}]
    }
    
    code, out = curl("POST", "/v1/chat/completions", body=payload, expect=200, bypass_cache=True)
    
    # Verify the intercepted response contains the original value
    if original_val in out:
        print("  [OK]   synapse_retrieve intercepted, original payload returned successfully")
    else:
        print("  [FAIL] synapse_retrieve interception failed")
        print("  output:", out)
        sys.exit(1)
        
    # Clean up
    redis_cli("DEL", f"synapse:ccr:{test_key}")


def get_upstream_last_log() -> str:
    r = subprocess.run(["docker", "logs", "--tail", "100", "synapse-upstream-mock"], capture_output=True, text=True, timeout=5)
    return r.stdout


def test_smart_crusher_e2e():
    """Verify that a large JSON array is compressed by SmartCrusher hook before reaching upstream."""
    # Build a homogeneous array of 15 items to trigger SmartCrusher (>800 chars)
    items = []
    for i in range(15):
        items.append({
            "id": i,
            "name": f"element_number_{i}_" + "x" * 50,
            "status": "active",
            "score": 99.9
        })
    array_str = json.dumps(items)
    
    payload = {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": array_str}]
    }
    
    curl("POST", "/v1/chat/completions", body=payload, expect=200, bypass_cache=True)
    
    time.sleep(1) # Let logs flush
    upstream_log = get_upstream_last_log()
    
    if "id,name,score,status" in upstream_log:
        print("  [OK]   SmartCrusher lossless CSV compaction applied and verified at upstream")
    else:
        print("  [FAIL] SmartCrusher compression not seen at upstream")
        print("  Upstream logs:", upstream_log)
        sys.exit(1)


def test_diff_compressor_e2e():
    """Verify that a large git diff is compressed by DiffCompressor hook before reaching upstream."""
    lines = [
        "diff --git a/src/main.go b/src/main.go",
        "index 8374289..2837498 100644",
        "--- a/src/main.go",
        "+++ b/src/main.go",
        "@@ -1,60 +1,62 @@"
    ]
    for i in range(25):
        lines.append(" line context before change")
    lines.append("-old line to delete")
    lines.append("+new line to add")
    for i in range(25):
        lines.append(" line context after change")
        
    diff_str = "\n".join(lines)
    payload = {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": diff_str}]
    }
    
    curl("POST", "/v1/chat/completions", body=payload, expect=200, bypass_cache=True)
    
    time.sleep(1) # Let logs flush
    upstream_log = get_upstream_last_log()
    
    if "<<ccr:" in upstream_log and "..." in upstream_log:
        print("  [OK]   DiffCompressor trim and CCR offloading applied and verified at upstream")
    else:
        print("  [FAIL] DiffCompressor compression not seen at upstream")
        print("  Upstream logs:", upstream_log)
        sys.exit(1)


def test_metrics():
    print("  Cache hits + hook invocations since startup:")
    r = subprocess.run(["curl", "-s", BASE + "/metrics"], capture_output=True, text=True, timeout=10)
    for line in r.stdout.splitlines():
        if line.startswith("synapse_proxy_") and not line.startswith("#"):
            print("  ", line)


if __name__ == "__main__":
    test("Health", test_health)
    test("Unknown key returns 401", test_unknown_key)
    test("Simple chat (encrypted real_key pipeline)", test_simple_chat)
    test("Tool denylist (forbidden tool = 403)", test_tool_denylist)
    test("Tool allowlist strict mode (unknown = 400)", test_tool_allowlist)
    test("Session circuit breaker (limit hit = 400)", test_session_circuit_breaker)
    test("Loop kill switch (3 identical = 400 on 3rd)", test_loop_killswitch)
    test("Active CCR synapse_retrieve interception", test_ccr_retrieval)
    test("SmartCrusher hook E2E compression", test_smart_crusher_e2e)
    test("DiffCompressor hook E2E compression", test_diff_compressor_e2e)
    test("Metrics snapshot", test_metrics)
    print("\n=== All tests done ===")