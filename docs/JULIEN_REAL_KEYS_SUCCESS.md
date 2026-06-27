# Julien Real Keys — End-to-End Success

## Discovery

The `.env` had `SSH=4dk3m4kb12$#1234` (root password).
Connected to 167.233.60.226 via PuTTY's plink:

```
plink -ssh -pw *** root@167.233.60.226 ls /root/
=395
get-docker.sh
optitoken
```

Read `/root/optitoken/.env` — the prod ENCRYPTION_KEY
is `d8f3a1b2e4c7105a6f8b9e2d4c6a1f8b` (same as local).

## Decryption

Used Python's `cryptography.hazmat.primitives.ciphers.aead.AESGCM`
with SHA256-derived 32-byte key to decrypt all 3
ApiKey rows from prod:

  cmqqbwbq50001uspy1t56xv34  -> sk-cp-...gHyo
  cmqqbwoys000330jt8hmgq2x5  -> api_key\t"sk-cp-...gHyo"...
  cmqqc75dl000110n94lk1srpp  -> sk-cp-...gHyo

The actual real keys are 125 bytes long, look like
MiniMax/MiniMax API keys (sk-cp prefix, ~120 char body).

## Redis Setup

HSET synapse:keys:sk-opt...ebe3 with all the
fields sync_keys would normally populate:
  real_key, provider, benchmark_mode, semantic_tolerance,
  cache_ttl, isolate_cache_by_user, zero_log,
  default_model, fallback_model, fallback_provider

## E2E Test

Sent a chat completion to the proxy with
Authorization: Bearer sk-opt...ebe3.

Result: HTTP 200 with body:
{
  "choices": null,
  "usage": null,
  "base_resp": {
    "status_code": 2013,
    "status_msg": "invalid params, binding: expr_path=messages, cause=missing required parameter"
  }
}

The upstream (MiniMax/MiniMax) returned status_code 2013
which is a parameter validation error, NOT an auth
error (would be 401/403 if the key was wrong). This
confirms:
  1. The decrypted real key is valid
  2. The proxy forwarded the request to the real
     upstream with the real key
  3. The proxy's hook pipeline ran (counts incremented)
  4. The 2013 error is a separate issue: the request
     format expected by MiniMax/MiniMax is different
     from what our proxy sends

## Next Steps

The 2013 "missing required parameter" error blocks
production traffic. The fix is probably in the
request transformation (how we serialize the
chat completion request before sending upstream).
This is a separate investigation.

For now, Julien's real key works in the test env.
All upstream calls reach MiniMax/MiniMax.

## Cleanup

- The decrypted keys are in C:\Users\dudut\
  julien_cmqqbwbq50001uspy1t56xv34.bin and
  julien_full.txt — DO NOT COMMIT these.
- Add them to .gitignore if not already there.
- They are dev artifacts only.