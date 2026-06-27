# Prod Dump Integration — 2026-06-25

Dumped `167.233.60.226/optitoken_db` (prod) into the
local test DB.

**What was integrated** :

| Table | Rows | Use |
|-------|------|-----|
| ProviderModel | 46 | Pricing matrix for the dashboard |
| User | 2 | Admin user for testing |
| ApiKey | 3 | Virtual keys (realKeyEnc anonymized) |
| EmailTemplate | 3 | SMTP templates for verification |
| SystemConfig | 1 | Site-wide settings |
| AlertRule | n | Alert rules for the HUD |
| BlogPost | n | Blog content |
| StripePlan | n | Plan definitions |
| LandingPageContent | n | Marketing pages |
| Prospect | n | Waitlist prospects |

**What was excluded** :

- `RequestLog` : volume + privacy (per-user request logs)
- `_prisma_migrations` : Prisma manages this locally

**How** :

```
docker exec synapse-postgres pg_dump \
  -h 167.233.60.226 -U optitoken_admin -d optitoken_db \
  --no-owner --no-acl \
  --exclude-table=RequestLog \
  --exclude-table=_prisma_migrations \
  -f /tmp/prod_dump.sql

# Sanitize (replace realKeyEnc with __ANONYMIZED__,
# emails with admin@synapse.local)

docker exec synapse-postgres psql -U optitoken -d optitoken_db \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

docker exec synapse-postgres psql -U optitoken -d optitoken_db \
  -f /tmp/prod_dump.sql
```

**Result** : the local DB now mirrors prod's structure
and data. The dashboard displays real pricing for real
models. Tests can exercise the actual ProviderModel
matrix.

**Caveat** : the 3 realKeyEnc in ApiKey cannot be
decrypted locally because ENCRYPTION_KEY differs from
prod. The virtual keys (sk-opt-...) still authenticate
(via the DB row), but requests fail when trying to
call the upstream provider with the anonymized real
key. To re-test against real upstream, re-encrypt with
the local ENCRYPTION_KEY or generate new keys.