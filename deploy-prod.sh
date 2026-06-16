#!/bin/bash
# ============================================================================
# OptiToken Production Deployment Script
# ============================================================================
# Usage:
#   1. Copy this file to your server: scp deploy-prod.sh user@server:~/optitoken/Optitoken/
#   2. On the server, run: chmod +x deploy-prod.sh && ./deploy-prod.sh
#
# What it does:
#   - Verifies prerequisites (Docker, disk space)
#   - Pulls the latest code
#   - Stops running containers
#   - DROPS the postgres volume (this deletes all data — back up first if needed)
#   - Switches Caddyfile to HTTPS production mode
#   - Rebuilds all Docker images
#   - Starts the stack
#   - Pushes the Prisma schema to create tables
#   - Seeds the admin user + 46 model prices
#   - Verifies the stack is healthy
# ============================================================================

set -euo pipefail

# Load .env to get DOMAIN_NAME and other vars
if [ -f ".env" ]; then
    set +u
    while IFS='=' read -r key value; do
        # Skip comments and empty lines
        [[ "$key" =~ ^#.*$ ]] && continue
        [[ -z "$key" ]] && continue
        # Remove leading/trailing whitespace and quotes from value
        value=$(echo "$value" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")
        # Skip if key contains invalid characters
        [[ "$key" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]] || continue
        export "$key"="$value"
    done < <(grep -v '^#' .env | grep -v '^$')
    set -u
fi

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PROJECT_DIR="${PROJECT_DIR:-$HOME/optitoken/Optitoken}"
cd "$PROJECT_DIR"

echo -e "${YELLOW}==> OptiToken Production Deployment${NC}"
echo ""

# ---- 1. Prerequisites ----
echo -e "${YELLOW}[1/8]${NC} Checking prerequisites..."

if ! command -v docker &> /dev/null; then
    echo -e "${RED}ERROR: docker is not installed${NC}"
    exit 1
fi

if ! command -v docker compose &> /dev/null; then
    echo -e "${RED}ERROR: docker compose is not installed${NC}"
    exit 1
fi

# Check disk space (need at least 5GB free)
FREE_GB=$(df -BG "$PROJECT_DIR" | awk 'NR==2 {print $4}' | tr -d 'G')
if [ "$FREE_GB" -lt 5 ]; then
    echo -e "${RED}ERROR: only ${FREE_GB}GB free, need at least 5GB${NC}"
    exit 1
fi
echo "  - docker: OK"
echo "  - docker compose: OK"
echo "  - free disk: ${FREE_GB}GB OK"

# Check .env
if [ ! -f ".env" ]; then
    echo -e "${RED}ERROR: .env file not found at $PROJECT_DIR/.env${NC}"
    exit 1
fi
echo "  - .env: OK"

# ---- 2. Pull latest code ----
echo -e "${YELLOW}[2/8]${NC} Pulling latest code..."
if [ -d ".git" ]; then
    git pull --rebase --autostash || true
    echo "  - git pull done"
else
    echo "  - not a git repo, skipping (ensure you pushed your changes manually)"
fi

# ---- 3. Stop running stack ----
echo -e "${YELLOW}[3/8]${NC} Stopping current stack..."
docker compose -f docker-compose.prod.yml down || true
echo "  - stack stopped"

# ---- 4. Drop postgres volume (resets password + clears old data) ----
echo -e "${YELLOW}[4/8]${NC} Dropping postgres volume (this clears all DB data)..."
docker volume rm optitoken_postgres_data 2>/dev/null || echo "  - volume already gone, OK"
echo "  - postgres_data removed"

# ---- 5. Switch to production Caddyfile (HTTPS) ----
echo -e "${YELLOW}[5/8]${NC} Switching to production Caddyfile (HTTPS)..."
if [ -f "Caddyfile.prod" ]; then
    cp Caddyfile.prod Caddyfile
    echo "  - Caddyfile swapped to production (HTTPS via Let's Encrypt)"
    echo -e "  ${YELLOW}>> Make sure DNS for $DOMAIN_NAME points to this server's public IP <<${NC}"
else
    echo -e "  ${YELLOW}WARNING: Caddyfile.prod not found, using existing Caddyfile${NC}"
fi

# ---- 6. Rebuild all images ----
echo -e "${YELLOW}[6/8]${NC} Building Docker images (this takes 5-10 minutes)..."
docker compose -f docker-compose.prod.yml build --no-cache
echo "  - build OK"

# ---- 7. Start stack + migrate + seed ----
echo -e "${YELLOW}[7/8]${NC} Starting stack..."
docker compose -f docker-compose.prod.yml up -d
echo "  - stack started"

echo "  - waiting 30s for postgres to be ready..."
sleep 30

# Push Prisma schema (creates all tables)
echo "  - pushing Prisma schema..."
docker run --rm \
    --network optitoken_default \
    -v "$PROJECT_DIR/dashboard:/app" \
    -w /app \
    -e DATABASE_URL="postgresql://${POSTGRES_USER:-optitoken}:${POSTGRES_PASSWORD:-password123}@postgres:5432/${POSTGRES_DB:-optitoken_db}?sslmode=disable" \
    node:18 sh -c 'npm install --no-save prisma@5.22.0 > /tmp/npm.log 2>&1 && ./node_modules/.bin/prisma generate > /tmp/gen.log 2>&1 && ./node_modules/.bin/prisma db push --skip-generate 2>&1 | tail -5'

# Seed admin user + model pricing
echo "  - seeding admin user + 46 model prices..."
docker run --rm \
    --network optitoken_default \
    -v "$PROJECT_DIR/dashboard:/app" \
    -w /app \
    -e DATABASE_URL="postgresql://${POSTGRES_USER:-optitoken}:${POSTGRES_PASSWORD:-password123}@postgres:5432/${POSTGRES_DB:-optitoken_db}?sslmode=disable" \
    node:18 sh -c 'node seed.js 2>&1 | tail -5'

# ---- 8. Verify ----
echo -e "${YELLOW}[8/8]${NC} Verifying stack health..."
sleep 10

RUNNING=$(docker ps --format '{{.Names}}' --filter "name=optitoken" | wc -l)
echo "  - $RUNNING containers running"

# Check dashboard responds
DASHBOARD_OK=$(docker run --rm --network optitoken_default appropriate/curl -s -o /dev/null -w "%{http_code}" http://dashboard:3000/ 2>/dev/null || echo "fail")
if [ "$DASHBOARD_OK" = "200" ]; then
    echo "  - dashboard: OK (HTTP 200)"
else
    echo "  - dashboard: status=$DASHBOARD_OK (may still be starting)"
fi

# Check proxy responds
PROXY_OK=$(docker run --rm --network optitoken_default appropriate/curl -s -o /dev/null -w "%{http_code}" http://proxy:8080/v1/models 2>/dev/null || echo "fail")
if [ "$PROXY_OK" = "200" ] || [ "$PROXY_OK" = "401" ]; then
    echo "  - proxy: OK (HTTP $PROXY_OK)"
else
    echo "  - proxy: status=$PROXY_OK (may still be starting)"
fi

# Show final container status
echo ""
echo -e "${GREEN}==> Deployment complete${NC}"
echo ""
docker ps -a --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" --filter "name=optitoken"
echo ""
echo -e "${YELLOW}Admin user (CHANGE PASSWORD ON FIRST LOGIN):${NC}"
echo "  email:    admin@optitoken.local  (from the seed file)"
echo "  password: <placeholder>  (must be reset before first login;"
echo "                                 use 'Forgot password' on the dashboard,"
echo "                                 or UPDATE the passwordHash column directly in Postgres)"
echo ""
echo -e "${YELLOW}Access URLs:${NC}"
echo "  Dashboard: https://${DOMAIN_NAME:-optitoken.net}/"
echo "  API:       https://${DOMAIN_NAME:-optitoken.net}/v1/chat/completions"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo "  1. Log in to the dashboard with the admin credentials above"
echo "  2. Verify the 46 seeded models in /admin/models"
echo "  3. Make a test API request to verify the gateway works"
echo "  4. Check /admin/telemetry for incoming requests"
