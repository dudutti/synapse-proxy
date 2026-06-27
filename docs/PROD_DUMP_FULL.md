# Full Prod Dump Integration — 2026-06-25

Complete pg_dump from `167.233.60.226/optitoken_db` into
the local test DB. **Everything except `RequestLog`** (volume
+ privacy).

## What was integrated (11 tables)

| Table | Rows | Use |
|-------|------|-----|
| ProviderModel | 46 | Pricing matrix for the dashboard |
| User | 2 | Admin + USER accounts |
| ApiKey | 3 | Virtual keys (realKeyEnc anonymized) |
| EmailTemplate | 3 | SMTP templates (welcome, verify, etc.) |
| BlogPost | 2 | Full marketing posts (FR: "Synapse Proxy vs LiteLLM...") |
| LandingPageContent | 2 | Marketing pages |
| SystemConfig | 1 | Site-wide settings |
| Prospect | 1 | Waitlist prospect |

StripePlan and AlertRule/AlertEvent were empty in prod.

## What was sanitized

The sanitization script replaces:
- `realKeyEnc` (250+ char hex strings in TSV COPY blocks) → `__ANONYMIZED_HEX__`
- 80+ char base64 strings → `__ANONYMIZED_B64__`
- Email addresses → `admin@synapse.local`

The `virtualKey` values were already masked in the prod dump
(sk-opt...ebe3), so no further action needed.

## How

```
docker exec synapse-postgres bash -c "
  PGPASSWORD=*** pg_dump -h 167.233.60.226 -U optitoken_admin \
  -d optitoken_db --no-owner --no-acl --exclude-table=RequestLog \
  -f /tmp/prod_full_dump.sql
"
# Sanitize via Python (regex on COPY data, preserve schema)
# Drop schema and restore
docker exec synapse-postgres psql -U optitoken -d optitoken_db -c "
  DROP SCHEMA public CASCADE; CREATE SCHEMA public;
"
docker exec synapse-postgres psql -U optitoken -d optitoken_db \
  -f /tmp/prod_full_dump.sql
```

## What's now visible in the dashboard

- The marketing blog posts (real ones from prod) appear in
  the admin/blog page.
- The landing page content is real.
- The welcome email template can be previewed in admin/emails.
- ProviderModel list is real (46 models with real pricing).
- The admin can browse everything as if it were prod.

Caveat: the virtualKey values are masked (sk-opt...ebe3), so
no real upstream calls can be made. The realKeyEnc is
__ANONYMIZED_HEX__ so the dashboard cannot decrypt them. To
test against a real upstream, generate new keys via the
seed-admin script (Admin!Synapse2026! password).

## Commit

See `bee34b8b` for the first dump integration (sanitized
emails + realKeyEnc placeholder). This doc supplements it
with the full content dump.