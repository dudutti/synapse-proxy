# OptiToken Production Deployment Guide

> **Note on secrets:** This guide intentionally **does not** include real passwords. The actual values live in `~/optitoken/Optitoken/.env` on the server (chmod 600). Use this file as a *template*: copy the block, replace the placeholders, set restrictive file permissions.

## Prerequisites
- A server (tested on Hetzner CCX13, 4 vCPU / 16 GB RAM / 160 GB SSD)
- Docker + Docker Compose v2 installed
- A DNS A record pointing to your server (e.g. `optitoken.net → <server-ip>`)
- Ports 80 + 443 open (firewall / cloud security group)

## Architecture

```
            ┌──────────────────┐
            │   Caddy (TLS)    │   :80 / :443 (Let's Encrypt auto-renew)
            └─────────┬────────┘
                      │
        ┌─────────────┴──────────────┐
        ▼                            ▼
┌─────────────────┐         ┌─────────────────┐
│ optitoken-proxy │         │ optitoken-      │
│   (Go) :8080    │         │  dashboard      │
│   in docker net │         │  (closed-source)│
└────────┬────────┘         │   :3000         │
         │                  └────────┬────────┘
         │                           │
         ▼                           ▼
   ┌──────────────────────────────────────┐
   │      optitoken-redis (Stack)        │   :6379 (in docker net only)
   │  + Vector Search (FT.CREATE VSS)    │
   └──────────────────────────────────────┘

   ┌──────────────────┐
   │ optitoken-postgres│   :5432 (in docker net only)
   └──────────────────┘
```

All services are on the internal `optitoken_default` docker network. Only Caddy exposes ports 80/443 publicly.

## One-time setup (on the server)

```bash
# 1. Install Docker (if not already done)
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER

# 2. Create the project directory
mkdir -p ~/optitoken
cd ~/optitoken

# 3. Clone the repo (or upload a deploy bundle)
git clone https://github.com/dudutti/Optitoken.git
cd Optitoken
```

## Configuration: `.env` file

Create `~/optitoken/Optitoken/.env` with the following template (replace placeholders):

```bash
# Domain
DOMAIN_NAME=optitoken.net
NEXTAUTH_URL=https://optitoken.net

# Postgres
POSTGRES_USER=optitoken_admin
POSTGRES_PASSWORD=__change_me_to_a_strong_password__
POSTGRES_DB=optitoken_db

# NextAuth / Dashboard
NEXTAUTH_SECRET=__32_byte_hex_string_run_openssl_rand_hex_32__
ENCRYPTION_KEY=__32_byte_hex_string_used_to_encrypt_provider_keys_in_db__

# SMTP (for password resets, billing alerts)
SMTP_HOST=optitoken.net
SMTP_PORT=465
SMTP_USER=contact@optitoken.net
SMTP_PASS=__change_me__
SMTP_FROM=OptiToken <contact@optitoken.net>
```

Then lock down the file:

```bash
chmod 600 ~/optitoken/Optitoken/.env
```

> 🔒 **Never** commit `.env` to git. It's covered by `.gitignore` already.

## Deploy a new version

```bash
cd ~/optitoken/Optitoken

# 1. Pull the latest code
git pull origin main

# 2. Run the deploy script
chmod +x deploy-prod.sh
./deploy-prod.sh
```

## What `deploy-prod.sh` does

1. ✅ Verifies prerequisites (Docker, disk space, `.env`)
2. ✅ Pulls latest code from git
3. ✅ Stops running containers
4. ✅ **Drops the postgres volume** (this clears all DB data — back up first if needed)
5. ✅ Swaps Caddyfile to HTTPS production mode
6. ✅ Rebuilds all Docker images (5-10 min)
7. ✅ Starts the stack
8. ✅ Pushes Prisma schema (creates tables)
9. ✅ Seeds admin user + 46 model prices
10. ✅ Verifies all services are healthy

## Post-deployment smoke tests

```bash
# Proxy health
curl -sI https://optitoken.net/v1/models | head -1   # → HTTP/2 200
docker logs --tail=20 optitoken-proxy

# Dashboard health
curl -sI https://optitoken.net | head -1             # → HTTP/2 200
docker logs --tail=20 optitoken-dashboard

# Redis health (must show maxmemory + allkeys-lru)
docker exec optitoken-redis redis-cli CONFIG GET maxmemory
docker exec optitoken-redis redis-cli CONFIG GET maxmemory-policy

# End-to-end test
VK=$(docker run --rm --network optitoken_default appropriate/curl -s http://dashboard:3000/api/keys | jq -r '.[0].virtualKey')
docker run --rm --network optitoken_default appropriate/curl -s -X POST \
  https://optitoken.net/v1/chat/completions \
  -H "Authorization: Bearer $VK" \
  -H "Content-Type: application/json" \
  -d '{"model":"MiniMax-M3","messages":[{"role":"user","content":"ping"}]}'
```

## Daily operations

```bash
# View logs
docker logs -f optitoken-proxy
docker logs -f optitoken-caddy

# Restart a service
docker compose -f docker-compose.prod.yml restart proxy

# Update pricing for a model
docker exec -e PGPASSWORD=$POSTGRES_PASSWORD optitoken-postgres \
  psql -h postgres -U $POSTGRES_USER $POSTGRES_DB \
  -c "UPDATE \"ProviderModel\" SET \"costPromptPer1M\"=4.00, \"costCachedInputPer1M\"=0.40 WHERE provider='anthropic' AND \"modelName\"='claude-opus-4.8';"

# Backup Postgres (do this DAILY in production)
docker exec optitoken-postgres pg_dump -U $POSTGRES_USER $POSTGRES_DB | gzip > backup-$(date +%Y%m%d).sql.gz
# Then `scp` the dump to a separate backup location

# Restore Postgres
gunzip -c backup-20260616.sql.gz | docker exec -i optitoken-postgres psql -U $POSTGRES_USER $POSTGRES_DB

# Inspect Redis state
docker exec optitoken-redis redis-cli INFO keyspace
docker exec optitoken-redis redis-cli --scan --pattern 'optitoken:l1cache:*' | head -5

# Flush a poisoned cache (rare, only if a model returns poison)
docker exec optitoken-redis redis-cli --scan --pattern 'optitoken:l1cache:*' | xargs -r docker exec -i optitoken-redis redis-cli DEL
docker exec optitoken-redis redis-cli --scan --pattern 'optitoken:l2cache:*' | xargs -r docker exec -i optitoken-redis redis-cli DEL
docker exec optitoken-redis redis-cli --scan --pattern 'optitoken:loops:*' | xargs -r docker exec -i optitoken-redis redis-cli DEL
```

## Adding a new provider

1. Add the model list in `proxy/internal/handlers/models.go` (or fetch live from `GET /v1/models`)
2. Add pricing in `proxy/internal/db/pricing.go`
3. Add a provider alias map in `proxy/internal/utils/provider_models.go` (if needed for smart-aliasing)
4. Seed via `RegisterKnownModel` (called on dashboard dropdown fetch)
5. Restart proxy: `docker compose -f docker-compose.prod.yml restart proxy`

## Rollback

If the new version breaks something, you can roll back to a previous version:

```bash
cd ~/optitoken
# Stop current stack
cd Optitoken && docker compose -f docker-compose.prod.yml down

# Restore previous version
git log --oneline -10          # find the commit to roll back to
git checkout <previous-commit-hash>

# Restart
docker compose -f docker-compose.prod.yml up -d
```

## Security checklist

- [ ] `.env` is chmod 600 on the server
- [ ] SMTP password is rotated regularly
- [ ] Postgres password is rotated every 90 days
- [ ] NEXTAUTH_SECRET is rotated every 90 days
- [ ] ENCRYPTION_KEY is rotated every 90 days (warning: rotating this invalidates all stored provider keys in the `ApiKey` table)
- [ ] SSL certificate auto-renews (Caddy handles this)
- [ ] Caddy ports 80/443 are the only public-facing ports
- [ ] Dashboard and proxy are NOT directly exposed (only via Caddy)
- [ ] Admin user password is changed from the default
- [ ] Firewall blocks direct access to internal ports (3000, 5432, 6379, 8080)
- [ ] `protected-mode` on Redis is disabled (set `--protected-mode no` in `docker-compose.prod.yml`) **only because** the dashboard and proxy are on the same docker network. **Do not** expose Redis to the public internet.

## What `proxy/internal/handlers/proxy.go` does NOT log

Zero-Log Mode guarantees that when a key has `zeroLog=true`:
- `RequestLog.originalPayload` is empty string
- `RequestLog.optimizedPayload` is empty string
- No L1/L2/loop cache entry exists
- No Model Radar sample is collected

Token counts, latency, cost savings, model, agent, sessionId are still recorded. Verify this in `dashboard/app/settings/page.tsx → Zero-Log toggle`.
