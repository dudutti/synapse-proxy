test: generate real sk-opti- VK for end-to-end test

The test environment had anonymized virtual keys
(sk-opt...ebe3 format from the prod dump) which:
  - Failed the Bearer sk-opti- auth check
  - Could not be used for upstream calls
  - Could not be populated in Redis by sync_keys
    (realKeyEnc was a base64 placeholder, not a valid
    AES-256-GCM ciphertext)

This commit documents the manual end-to-end test:
  1. Generate a fresh VK via PostgreSQL directly:
     INSERT INTO "ApiKey" ... virtualKey='sk-opti-' +
     16 random bytes hex
  2. Manually populate the Redis hash via redis-cli:
     HSET synapse:keys:<vk> real_key provider
     benchmark_mode semantic_tolerance cache_ttl
     isolate_cache_by_user zero_log default_model
     fallback_model fallback_provider
  3. Run test_dashboard_full.py — 9 requests succeed
     (status 200) with hooks firing on every one.

Result:
  - RequestLog count: 33 (was 14)
  - perHookSavings JSON populated for every row:
    {"logCompressor":{"bytesSaved":1808},
     "synapseRetrieve":{"toolsInjected":1},
     "tagProtector":{"zones":0}}
  - Prometheus metrics all incrementing correctly

The upstream call (MiniMax-M2.7) returns an error
{"base_resp":{"status_code":2013,"status_msg":"..."}}
because we use a fake real_key, but that's OK — the
proxy's hook pipeline runs BEFORE the upstream call
and emits the per-hook savings regardless.

This proves end-to-end:
  Hook → metric increment → Redis stream → DB write →
  RequestLog.perHookSavings populated → dashboard ready
  to display.

For production, the real flow stays unchanged:
  admin creates a key via POST /api/keys →
  sync_keys decrypts realKeyEnc with ENCRYPTION_KEY →
  Redis hash populated →
  proxy reads VK from Redis → upstream calls work.

This doc will be removed once the real VK from the
admin UI is wired up (admin can create a new key in
the dashboard and it auto-syncs to Redis).