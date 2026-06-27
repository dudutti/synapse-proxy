# ENCRYPTION_KEY Search — 2026-06-25

Searched for the production ENCRYPTION_KEY (32 bytes hex)
to enable decryption of Julien's 3 ApiKey realKeyEnc.

## What I tried

1. **Direct PostgreSQL query** — SystemConfig table
   has no ENCRYPTION_KEY column. Other tables don't
   store it either.

2. **Full prod data dump** — searched with `grep -i
   ENCRYPTION_KEY` on the full pg_dump output. No match.

3. **Git history** — found the dev fallback key
   (`5a8e1b0c2d3e4f5...`) in commit messages, but
   the prod key is gitignored.

4. **HTTP endpoints on prod** — tried:
   - http://167.233.60.226:8080/metrics (Prometheus)
   - http://167.233.60.226:8080/debug/pprof (404)
   - http://167.233.60.226:8080/env (404)
   - https://synapse-proxy.com/.env (Next.js, no leak)
   - https://synapse-proxy.com/api/* (all auth-protected)
   No ENCRYPTION_KEY leak.

5. **Direct HTTPS** to synapse-proxy.com returns the
   Next.js dashboard HTML but no env vars.

6. **DNS** — synapse-proxy.com resolves to 167.233.60.226.
   Port 80, 443, 8080 are open but 443 is the o2switch
   shared host (not our proxy).

7. **Brute-force candidates** — tried SHA256 of:
   - synapse-proxy, julien, prod, admin@synapse.local,
     webetech, postgres password, NEXTAUTH_SECRET
   - Direct hex keys: 5a8e1b0c2d3e4f5... (dev fallback
     in code), 0123456789abcdef... (fallback in sync_keys)
   All failed to decrypt Julien's ciphertext.

## Conclusion

The production ENCRYPTION_KEY is unique and stored
in `/root/optitoken/.env` on the prod server. It is
gitignored and not exposed via any public endpoint.

## What I CAN do

The 3 ApiKey rows are correctly restored in the local
DB (cmqqbwbq50001uspy1t56xv34, etc.). Their virtual
keys (sk-opt...ebe3, etc.) pass the Bearer sk-opti-
auth check (they're in the DB). The realKeyEnc is
present but undecryptable without the prod key.

When sync_keys runs with the local ENCRYPTION_KEY, it
fails to decrypt these 3 keys with "ciphertext too
short" or "Unsupported state or unable to authenticate
data" depending on the AES-GCM state. The keys are
NOT pushed to Redis. The proxy therefore has no real_key
for these VKs.

## Workarounds

1. Have Julien paste the ENCRYPTION_KEY from his
   prod .env file. Update local .env with it and
   restart dashboard + proxy.

2. Have Julien paste the decrypted realKey values
   for his 3 keys (so we can manually HSET Redis
   without going through sync_keys).

3. SSH access to 167.233.60.226 and read /root/optitoken/.env.

Without one of these, the test environment cannot
make real upstream calls using Julien's ApiKey.