#!/usr/bin/env bash
set -euo pipefail

# Plati Database Seeder
# Seeds secrets and templates via the API (server must be running).
#
# Usage: ./scripts/seed-db.sh [BASE_URL] [ADMIN_PASSWORD]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../.env
[ -f "$SCRIPT_DIR/../.env" ] && source "$SCRIPT_DIR/../.env"

BASE_URL="${1:-http://localhost:8080}"
ADMIN_PASSWORD="${2:-${ADMIN_PASSWORD:?Set ADMIN_PASSWORD in .env or pass as arg}}"
COOKIE_JAR=$(mktemp)

trap "rm -f $COOKIE_JAR" EXIT

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}  OK${NC}: $1"; }
fail() { echo -e "${RED}  FAIL${NC}: $1"; exit 1; }
info() { echo -e "${YELLOW}  INFO${NC}: $1"; }

api() {
    local method="$1"
    local path="$2"
    shift 2
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
        -X "$method" \
        -H "Content-Type: application/json" \
        "${BASE_URL}${path}" "$@"
}

echo ""
echo "=== Plati Database Seeder ==="
echo "Backend: $BASE_URL"
echo ""

# -- Health check -------------------------------------------------
HEALTH=$(api GET /health)
STATUS=$(echo "$HEALTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null || echo "error")
if [ "$STATUS" != "ok" ]; then
    fail "Server not reachable at $BASE_URL"
fi

# -- Login --------------------------------------------------------
LOGIN=$(api POST /auth/login -d "{\"password\":\"$ADMIN_PASSWORD\"}")
MSG=$(echo "$LOGIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$MSG" != "ok" ]; then
    fail "Login failed: $LOGIN"
fi
pass "Admin login"

# -- Seed TAILSCALE_AUTH_KEY secret --------------------------------
echo ""
echo "Seeding secrets..."

TAILSCALE_KEY="${TAILSCALE_AUTH_KEY:?Set TAILSCALE_AUTH_KEY in .env}"

# Check if the secret already exists
SECRETS=$(api GET /api/v1/secrets)
HAS_TS=$(echo "$SECRETS" | python3 -c "import sys,json; ss=json.load(sys.stdin); print(any(s['name']=='TAILSCALE_AUTH_KEY' for s in ss))" 2>/dev/null || echo "False")

if [ "$HAS_TS" = "True" ]; then
    info "TAILSCALE_AUTH_KEY already exists, skipping"
else
    RESULT=$(api POST /api/v1/secrets -d "{\"name\":\"TAILSCALE_AUTH_KEY\",\"value\":\"$TAILSCALE_KEY\"}")
    SECRET_ID=$(echo "$RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
    if [ -n "$SECRET_ID" ]; then
        pass "TAILSCALE_AUTH_KEY secret created (id=$SECRET_ID)"
    else
        fail "Failed to create TAILSCALE_AUTH_KEY: $RESULT"
    fi
fi

# -- Import tailscale template ------------------------------------
echo ""
echo "Seeding templates..."

TEMPLATE_DIR="$SCRIPT_DIR/../config/templates"

if [ -f "$TEMPLATE_DIR/tailscale.yaml" ]; then
    TEMPLATES=$(api GET /api/v1/templates)
    HAS_TAILSCALE=$(echo "$TEMPLATES" | python3 -c "import sys,json; ts=json.load(sys.stdin); print(any(t['slug']=='tailscale' for t in ts))" 2>/dev/null || echo "False")

    if [ "$HAS_TAILSCALE" = "True" ]; then
        info "Tailscale template already exists, skipping"
    else
        IMPORT_RESULT=$(api POST /api/v1/admin/templates/import --data-binary "@$TEMPLATE_DIR/tailscale.yaml" -H "Content-Type: application/yaml")
        IMPORT_ID=$(echo "$IMPORT_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
        if [ -n "$IMPORT_ID" ]; then
            pass "Tailscale template imported (id=$IMPORT_ID)"
        else
            fail "Failed to import tailscale template: $IMPORT_RESULT"
        fi
    fi
else
    info "tailscale.yaml not found in $TEMPLATE_DIR, skipping"
fi

# -- Logout -------------------------------------------------------
api POST /auth/logout > /dev/null

echo ""
echo "=== Seed complete ==="
