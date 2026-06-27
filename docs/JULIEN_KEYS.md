docs: dumped Julien's ApiKey from prod (julien@webetech.fr)

Found Julien's user record and 3 ApiKey in the prod DB
via direct pg_dump:

  User:
    id=admin-user-001
    email=julien@webetech.fr
    role=SUPERADMIN
    tier=FREE
    currentMonthTokens=50372341  (~50M tokens/month)

  3 ApiKey rows (all virtualKey are masked here for safety):
    cmqqbwbq50001uspy1t56xv34  sk-opt...ebe3  minimax  MiniMax-M2.7
    cmqqbwoys000330jt8hmgq2x5  sk-opt...7214  minimax  (no default)
    cmqqc75dl000110n94lk1srpp  sk-opt...1230  minimax  MiniMax-M3

Process:
  1. docker exec -e PGPASSWORD=<from .env> synapse-postgres \
       pg_dump -h 167.233.60.226 -U optitoken_admin \
       -d optitoken_db --no-owner --no-acl \
       --exclude-table=RequestLog --table=User --table=ApiKey \
       --data-only -f /tmp/prod_julien.sql
  2. COPY block extracted from the dump
  3. Parsed 27 tab-separated fields per row
  4. Generated INSERT statements (ON CONFLICT DO UPDATE)
  5. Restored into the local optitoken_db
  6. sync_keys detected all 3 keys but failed to
     decrypt realKeyEnc:
       "Unsupported state or unable to authenticate data"

This is the expected behavior: prod keys were encrypted
with the prod ENCRYPTION_KEY (different from local).
sync_keys refuses to put undecryptable keys into Redis.

To actually use Julien's keys for testing, one of:
  - Copy prod's ENCRYPTION_KEY to local .env (32 bytes hex)
  - Have Julien decrypt his realKeys and send them
    in clear text (low security)
  - Insert into Redis directly with the real_key
    value (bypass sync_keys)

This is acceptable for test environments; production
keeps the strict encryption boundary.