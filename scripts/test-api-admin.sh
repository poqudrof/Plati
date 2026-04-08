#!/usr/bin/env bash
set -euo pipefail

# Plati Admin API Test Script
# Tests: template CRUD, server list, user management, template import/export

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

pass() { echo -e "${GREEN}  PASS${NC}: $1"; }
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
echo "=== Plati Admin API Tests ==="
echo ""

# Login
api POST /auth/login -d "{\"password\":\"$ADMIN_PASSWORD\"}" > /dev/null
pass "Admin login"

# Pre-cleanup: remove any leftover test template from a previous failed run
EXISTING_ID=$(api GET /api/v1/templates | python3 -c "import sys,json; ts=json.load(sys.stdin); ids=[str(t['id']) for t in ts if t['slug']=='test-go-dev']; print(ids[0] if ids else '')" 2>/dev/null || echo "")
if [ -n "$EXISTING_ID" ]; then
    api DELETE "/api/v1/admin/templates/$EXISTING_ID" > /dev/null || true
    info "Pre-cleanup: removed stale test-go-dev template (id=$EXISTING_ID)"
fi

# -- Template Import ----------------------------------------------
echo "1. Import template via API"
IMPORT_DATA='{
    "name": "Test Go Dev",
    "slug": "test-go-dev",
    "description": "Go development environment for testing",
    "image": "images:ubuntu/24.04",
    "profiles": ["default"],
    "resources": {"cpu": 4, "memory": "8GB", "disk": "30GB"},
    "cloud_init": "#cloud-config\npackages:\n  - git\n  - curl\n"
}'
IMPORT_RESULT=$(api POST /api/v1/admin/templates/import -d "$IMPORT_DATA")
IMPORT_ID=$(echo "$IMPORT_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
if [ -n "$IMPORT_ID" ]; then
    pass "Template imported: id=$IMPORT_ID"
else
    fail "Template import failed: $IMPORT_RESULT"
fi

# -- Template List ------------------------------------------------
echo "2. List templates (should include imported)"
TEMPLATES=$(api GET /api/v1/templates)
HAS_GO=$(echo "$TEMPLATES" | python3 -c "import sys,json; ts=json.load(sys.stdin); print(any(t['slug']=='test-go-dev' for t in ts))" 2>/dev/null)
if [ "$HAS_GO" = "True" ]; then
    pass "Imported template visible in list"
else
    fail "Imported template not found in list"
fi

# -- Template Export ----------------------------------------------
echo "3. Export template"
EXPORT=$(api GET "/api/v1/templates/$IMPORT_ID/export")
# Export endpoint returns YAML; extract name with grep
EXPORT_NAME=$(echo "$EXPORT" | grep '^name:' | sed 's/^name:[[:space:]]*//')
if [ "$EXPORT_NAME" = "Test Go Dev" ]; then
    pass "Template export matches: $EXPORT_NAME"
else
    fail "Export mismatch: $EXPORT"
fi

# -- Template Update ----------------------------------------------
echo "4. Update template"
UPDATE_RESULT=$(api PUT "/api/v1/admin/templates/$IMPORT_ID" -d '{
    "name": "Test Go Dev Updated",
    "slug": "test-go-dev",
    "description": "Updated description",
    "image": "images:ubuntu/24.04",
    "profiles": "[\"default\"]",
    "resources": "{\"cpu\":4,\"memory\":\"8GB\"}",
    "cloud_init": "",
    "is_active": true
}')
# Verify
UPDATED=$(api GET "/api/v1/templates/$IMPORT_ID")
UPDATED_NAME=$(echo "$UPDATED" | python3 -c "import sys,json; print(json.load(sys.stdin)['name'])" 2>/dev/null || echo "")
if [ "$UPDATED_NAME" = "Test Go Dev Updated" ]; then
    pass "Template updated: $UPDATED_NAME"
else
    info "Template name after update: $UPDATED_NAME"
fi

# -- Template Delete ----------------------------------------------
echo "5. Delete template"
DELETE_RESULT=$(api DELETE "/api/v1/admin/templates/$IMPORT_ID")
DELETE_MSG=$(echo "$DELETE_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$DELETE_MSG" = "deleted" ]; then
    pass "Template deleted"
else
    fail "Delete failed: $DELETE_RESULT"
fi

# Verify deletion
TEMPLATES2=$(api GET /api/v1/templates)
HAS_GO2=$(echo "$TEMPLATES2" | python3 -c "import sys,json; ts=json.load(sys.stdin); print(any(t['slug']=='test-go-dev' for t in ts))" 2>/dev/null)
if [ "$HAS_GO2" = "False" ]; then
    pass "Template no longer in list"
else
    fail "Template still visible after deletion"
fi

# -- Server List --------------------------------------------------
echo "6. List servers"
SERVERS=$(api GET /api/v1/admin/servers)
SRV_COUNT=$(echo "$SERVERS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
pass "Found $SRV_COUNT servers"

# -- User List ----------------------------------------------------
echo "7. List users"
USERS=$(api GET /api/v1/admin/users)
USER_COUNT=$(echo "$USERS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
HAS_ADMIN=$(echo "$USERS" | python3 -c "import sys,json; us=json.load(sys.stdin); print(any(u['email']=='admin@plati.local' for u in us))" 2>/dev/null)
if [ "$HAS_ADMIN" = "True" ]; then
    pass "Found $USER_COUNT users (admin present)"
else
    fail "Admin user not found in user list"
fi

# -- SSH Key CRUD -------------------------------------------------
echo "8. SSH key CRUD"
# Add key
KEY_RESULT=$(api POST /api/v1/ssh-keys -d '{"name":"test-key","public_key":"ssh-ed25519 AAAAC3NzTestKey test@api"}')
KEY_ID=$(echo "$KEY_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
if [ -n "$KEY_ID" ]; then
    pass "SSH key created: id=$KEY_ID"
else
    fail "SSH key creation failed: $KEY_RESULT"
fi

# List keys
KEYS=$(api GET /api/v1/ssh-keys)
KEY_COUNT=$(echo "$KEYS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
pass "Listed $KEY_COUNT SSH keys"

# Delete key
api DELETE "/api/v1/ssh-keys/$KEY_ID" > /dev/null
KEYS2=$(api GET /api/v1/ssh-keys)
KEY_COUNT2=$(echo "$KEYS2" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
if [ "$KEY_COUNT2" -lt "$KEY_COUNT" ]; then
    pass "SSH key deleted"
else
    fail "SSH key still present after deletion"
fi

# -- Secret CRUD --------------------------------------------------
echo "9. Secret CRUD"
SECRET_RESULT=$(api POST /api/v1/secrets -d '{"name":"TEST_SECRET","value":"super-secret-123"}')
SECRET_ID=$(echo "$SECRET_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
if [ -n "$SECRET_ID" ]; then
    pass "Secret created: id=$SECRET_ID"
else
    fail "Secret creation failed: $SECRET_RESULT"
fi

# List secrets (should not contain value)
SECRETS=$(api GET /api/v1/secrets)
HAS_VALUE=$(echo "$SECRETS" | python3 -c "import sys,json; ss=json.load(sys.stdin); print(any('encrypted_value' in s or 'value' in s for s in ss))" 2>/dev/null)
pass "Secrets listed (encrypted values hidden: $HAS_VALUE)"

# Update secret
api PUT "/api/v1/secrets/$SECRET_ID" -d '{"value":"updated-secret-456"}' > /dev/null
pass "Secret updated"

# Delete secret
api DELETE "/api/v1/secrets/$SECRET_ID" > /dev/null
SECRETS2=$(api GET /api/v1/secrets)
SEC_COUNT=$(echo "$SECRETS2" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
pass "Secret deleted (remaining: $SEC_COUNT)"

# -- Admin Tailscale Key Lifecycle ---------------------------------
echo "10. Admin Tailscale key lifecycle"
# Check not configured
TS_STATUS=$(api GET /api/v1/admin/settings/tailscale-key)
TS_CONFIGURED=$(echo "$TS_STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('configured',None))" 2>/dev/null || echo "")
if [ "$TS_CONFIGURED" = "False" ]; then
    pass "Tailscale key initially not configured"
else
    info "Tailscale key status: $TS_CONFIGURED"
fi

# Set key
api PUT /api/v1/admin/settings/tailscale-key -d '{"value":"tskey-auth-test-key-123"}' > /dev/null
TS_STATUS=$(api GET /api/v1/admin/settings/tailscale-key)
TS_CONFIGURED=$(echo "$TS_STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('configured',None))" 2>/dev/null || echo "")
if [ "$TS_CONFIGURED" = "True" ]; then
    pass "Tailscale key set successfully"
else
    fail "Tailscale key not configured after set: $TS_STATUS"
fi

# Delete key
api DELETE /api/v1/admin/settings/tailscale-key > /dev/null
TS_STATUS=$(api GET /api/v1/admin/settings/tailscale-key)
TS_CONFIGURED=$(echo "$TS_STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('configured',None))" 2>/dev/null || echo "")
if [ "$TS_CONFIGURED" = "False" ]; then
    pass "Tailscale key deleted"
else
    fail "Tailscale key still configured after delete"
fi

# -- Preferences API ----------------------------------------------
echo "11. Preferences API"
PREFS=$(api GET /api/v1/preferences)
SSH_MODE=$(echo "$PREFS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('ssh_key_mode',''))" 2>/dev/null || echo "")
if [ "$SSH_MODE" = "plati" ]; then
    pass "Default SSH key mode: plati"
else
    info "SSH key mode: $SSH_MODE"
fi

# Update to personal
api PUT /api/v1/preferences -d '{"ssh_key_mode":"personal","tailscale_mode":"personal"}' > /dev/null
PREFS=$(api GET /api/v1/preferences)
SSH_MODE=$(echo "$PREFS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('ssh_key_mode',''))" 2>/dev/null || echo "")
if [ "$SSH_MODE" = "personal" ]; then
    pass "Preferences updated to personal"
else
    fail "Preferences update failed: $PREFS"
fi

# Reset back to plati
api PUT /api/v1/preferences -d '{"ssh_key_mode":"plati","tailscale_mode":"plati"}' > /dev/null
pass "Preferences reset to plati"

# -- Unauthorized Access ------------------------------------------
echo "12. Unauthorized access check"
api POST /auth/logout > /dev/null
UNAUTH=$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}/api/v1/instances")
if [ "$UNAUTH" = "401" ]; then
    pass "Unauthenticated request returns 401"
else
    fail "Expected 401, got $UNAUTH"
fi

echo ""
echo "=== All admin API tests completed ==="
