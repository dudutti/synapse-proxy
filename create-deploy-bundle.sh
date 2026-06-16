#!/bin/bash
# ============================================================================
# Bundle creation script for production deployment
# ============================================================================
# Creates a tar.gz with all modified files (proxy + dashboard + config).
# The dashboard/ folder is gitignored in the main repo, so we need to bundle
# it explicitly.
# ============================================================================

set -euo pipefail

PROJECT_DIR="${PROJECT_DIR:-$HOME/optitoken/Optitoken}"
cd "$PROJECT_DIR"

BUNDLE_NAME="optitoken-deploy-$(date +%Y%m%d-%H%M%S).tar.gz"
BUNDLE_DIR="/tmp/optitoken-bundle-$$"

echo "==> Creating deployment bundle: $BUNDLE_NAME"
mkdir -p "$BUNDLE_DIR"

# 1. Include modified proxy + root files (from git status)
echo "  - Adding modified proxy + root files..."

# Use git diff to find modified files (excluding dashboard/), copy them preserving paths
git status --porcelain | grep -v '^??.*dashboard/' | awk '{print $2}' | while read -r f; do
    if [ -f "$f" ]; then
        mkdir -p "$BUNDLE_DIR/$(dirname "$f")"
        cp "$f" "$BUNDLE_DIR/$f"
        echo "    + $f"
    fi
done

# 2. Include untracked root files (Caddyfile, deploy-prod.sh, etc.)
echo "  - Adding new root files..."
for f in Caddyfile Caddyfile.prod deploy-prod.sh docker-compose.prod.yml; do
    if [ -f "$f" ]; then
        mkdir -p "$BUNDLE_DIR/$(dirname "$f")"
        cp "$f" "$BUNDLE_DIR/$f"
        echo "    + $f"
    fi
done

# 3. Include the entire dashboard/ folder (gitignored)
echo "  - Adding dashboard/ folder (gitignored, not in git)..."
mkdir -p "$BUNDLE_DIR/dashboard"
cp -r dashboard/. "$BUNDLE_DIR/dashboard/" 2>/dev/null || echo "    (skipped node_modules / .next)"

# Exclude heavy/build artifacts from dashboard
rm -rf "$BUNDLE_DIR/dashboard/node_modules" 2>/dev/null || true
rm -rf "$BUNDLE_DIR/dashboard/.next" 2>/dev/null || true
rm -rf "$BUNDLE_DIR/dashboard/.turbo" 2>/dev/null || true

# 4. Include proxy seeds
if [ -d "proxy/seeds" ]; then
    echo "  - Adding proxy/seeds/"
    cp -r proxy/seeds "$BUNDLE_DIR/proxy/"
fi

# 5. Create the tar.gz
echo "  - Compressing..."
cd "$(dirname "$BUNDLE_DIR")"
tar czf "$BUNDLE_NAME" "$(basename "$BUNDLE_DIR")"

# Cleanup
rm -rf "$BUNDLE_DIR"

echo ""
echo "==> Bundle created: $PROJECT_DIR/$BUNDLE_NAME"
echo "    Size: $(du -h "$BUNDLE_NAME" | cut -f1)"
echo ""
echo "To deploy:"
echo "  1. scp $BUNDLE_NAME user@server:~/optitoken/"
echo "  2. On the server: tar xzf $BUNDLE_NAME -C /tmp/"
echo "  3. cd ~/optitoken/Optitoken && rsync -av --delete /tmp/optitoken-bundle-*/ ./"
echo "  4. Run ./deploy-prod.sh"
