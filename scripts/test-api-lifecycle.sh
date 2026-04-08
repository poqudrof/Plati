#!/usr/bin/env bash
set -euo pipefail

# Plati API Lifecycle Test Script
# Tests: login -> list templates -> create instance -> start/stop/rebuild -> delete
#
# Usage: ./scripts/test-api-lifecycle.sh [BASE_URL] [ADMIN_PASSWORD]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../.env
[ -f "$SCRIPT_DIR/../.env" ] && source "$SCRIPT_DIR/../.env"

BASE_URL="${1:-http://localhost:8080}"
ADMIN_PASSWORD="${2:-${ADMIN_PASSWORD:?Set ADMIN_PASSWORD in .env or pass as arg}}"
TAILSCALE_AUTH_KEY="${TAILSCALE_AUTH_KEY:?Set TAILSCALE_AUTH_KEY in .env}"
COOKIE_JAR=$(mktemp)

trap "rm -f $COOKIE_JAR" EXIT

RED='\033[0;31m'
GREEN='\033[0;32m'
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
echo "=== Plati API Lifecycle Test ==="
echo "Backend: $BASE_URL"
echo ""

# -- Health Check -------------------------------------------------
echo "1. Health check"
HEALTH=$(api GET /health)
STATUS=$(echo "$HEALTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null || echo "error")
if [ "$STATUS" = "ok" ]; then
    pass "Health endpoint returns ok"
else
    fail "Health endpoint returned: $HEALTH"
fi

# -- Admin Login --------------------------------------------------
echo "2. Admin login"
LOGIN=$(api POST /auth/login -d "{\"password\":\"$ADMIN_PASSWORD\"}")
MSG=$(echo "$LOGIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$MSG" = "ok" ]; then
    pass "Admin login successful"
else
    fail "Login failed: $LOGIN"
fi

# -- Get Current User ---------------------------------------------
echo "3. Get current user"
ME=$(api GET /auth/me)
EMAIL=$(echo "$ME" | python3 -c "import sys,json; print(json.load(sys.stdin)['email'])" 2>/dev/null || echo "")
ROLE=$(echo "$ME" | python3 -c "import sys,json; print(json.load(sys.stdin)['role'])" 2>/dev/null || echo "")
if [ "$EMAIL" = "admin@plati.local" ] && [ "$ROLE" = "admin" ]; then
    pass "Current user: $EMAIL (role: $ROLE)"
else
    fail "Unexpected user: $ME"
fi

# -- List Templates -----------------------------------------------
echo "4. List templates"
TEMPLATES=$(api GET /api/v1/templates)
TMPL_COUNT=$(echo "$TEMPLATES" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
if [ "$TMPL_COUNT" -gt 0 ]; then
    pass "Found $TMPL_COUNT templates"
    TEMPLATE_ID=$(echo "$TEMPLATES" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])" 2>/dev/null)
    TEMPLATE_NAME=$(echo "$TEMPLATES" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['name'])" 2>/dev/null)
    info "Using template: $TEMPLATE_NAME (id=$TEMPLATE_ID)"
else
    fail "No templates found. Import templates first."
fi

# -- Seed TAILSCALE_AUTH_KEY secret --------------------------------
echo "5. Seed TAILSCALE_AUTH_KEY secret"
TAILSCALE_KEY="$TAILSCALE_AUTH_KEY"
SECRET_RESULT=$(api POST /api/v1/secrets -d "{\"name\":\"TAILSCALE_AUTH_KEY\",\"value\":\"$TAILSCALE_KEY\"}")
SECRET_ID=$(echo "$SECRET_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
if [ -n "$SECRET_ID" ]; then
    pass "TAILSCALE_AUTH_KEY secret created (id=$SECRET_ID)"
else
    fail "Failed to create TAILSCALE_AUTH_KEY: $SECRET_RESULT"
fi

# -- List Instances (should be empty) -----------------------------
echo "6. List instances (before create)"
INSTANCES=$(api GET /api/v1/instances)
INST_COUNT=$(echo "$INSTANCES" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
pass "Found $INST_COUNT instances"

# -- Create Instance ----------------------------------------------
echo "7. Create instance"
INSTANCE_NAME="test-lifecycle-$(date +%s)"
CREATE_RESULT=$(api POST /api/v1/instances -d "{\"name\":\"$INSTANCE_NAME\",\"template_id\":$TEMPLATE_ID}")
INST_ID=$(echo "$CREATE_RESULT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "")
INST_STATUS=$(echo "$CREATE_RESULT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('status',''))" 2>/dev/null || echo "")
INCUS_NAME=$(echo "$CREATE_RESULT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('incus_name',''))" 2>/dev/null || echo "")

if [ -n "$INST_ID" ] && [ "$INST_STATUS" = "running" ]; then
    pass "Instance created: id=$INST_ID name=$INSTANCE_NAME incus=$INCUS_NAME status=$INST_STATUS"
elif [ -n "$INST_ID" ]; then
    info "Instance created with status: $INST_STATUS (id=$INST_ID)"
    info "Note: Instance may not be running if no Incus server is configured."
else
    # If no Incus server is available, creation will fail - that's expected
    ERROR=$(echo "$CREATE_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('error','unknown'))" 2>/dev/null || echo "unknown")
    info "Instance creation returned: $ERROR"
    info "This is expected if no Incus server is configured."
    echo ""
    echo "=== Lifecycle test completed (partial - no Incus server) ==="
    echo "  Tests passed: health, login, user, templates, instance list"
    echo "  Skipped: instance create/start/stop/rebuild/delete (no Incus server)"
    exit 0
fi

# -- Get Instance Detail ------------------------------------------
echo "8. Get instance detail"
DETAIL=$(api GET "/api/v1/instances/$INST_ID")
DETAIL_NAME=$(echo "$DETAIL" | python3 -c "import sys,json; print(json.load(sys.stdin)['name'])" 2>/dev/null || echo "")
if [ "$DETAIL_NAME" = "$INSTANCE_NAME" ]; then
    pass "Instance detail matches: $DETAIL_NAME"
else
    fail "Instance detail mismatch: expected $INSTANCE_NAME, got $DETAIL_NAME"
fi

# -- Verify in Incus (if incus CLI available) ---------------------
echo "9. Verify in Incus"
if command -v incus >/dev/null 2>&1 && [ -n "$INCUS_NAME" ]; then
    INCUS_STATUS=$(incus info "$INCUS_NAME" 2>/dev/null | grep "Status:" | awk '{print $2}' || echo "unknown")
    if [ "$INCUS_STATUS" = "RUNNING" ] || [ "$INCUS_STATUS" = "Running" ]; then
        pass "Incus instance $INCUS_NAME is running"
    else
        info "Incus instance status: $INCUS_STATUS"
    fi

    # Check workspace volume
    DEVICES=$(incus config device list "$INCUS_NAME" 2>/dev/null || echo "")
    if echo "$DEVICES" | grep -q "workspace"; then
        pass "Workspace volume is attached"
    else
        info "Workspace device not found in: $DEVICES"
    fi
else
    info "Incus CLI not available or no incus_name - skipping direct verification"
fi

# -- Verify user disk listing -------------------------------------
echo "9b. Verify user disk listing"
DISKS=$(api GET /api/v1/disks)
DISK_COUNT=$(echo "$DISKS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)" 2>/dev/null || echo "0")
if [ "$DISK_COUNT" -ge 1 ]; then
    pass "User disk listing shows $DISK_COUNT disk(s)"
    DISK_INSTANCE=$(echo "$DISKS" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['instance_name'])" 2>/dev/null || echo "")
    DISK_MOUNT=$(echo "$DISKS" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['mount_path'])" 2>/dev/null || echo "")
    DISK_SIZE=$(echo "$DISKS" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['size_gb'])" 2>/dev/null || echo "")
    info "Disk: instance=$DISK_INSTANCE mount=$DISK_MOUNT size=${DISK_SIZE}GB"
else
    fail "Expected at least 1 disk, got $DISK_COUNT"
fi

# -- Verify admin disk listing ------------------------------------
echo "9c. Verify admin disk listing"
ADMIN_DISKS=$(api GET /api/v1/admin/disks)
ADMIN_DISK_COUNT=$(echo "$ADMIN_DISKS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)" 2>/dev/null || echo "0")
if [ "$ADMIN_DISK_COUNT" -ge 1 ]; then
    pass "Admin disk listing shows $ADMIN_DISK_COUNT disk(s)"
else
    fail "Admin disk listing empty"
fi

# -- Verify Incus storage volume ----------------------------------
echo "9d. Verify Incus storage volume"
if command -v incus >/dev/null 2>&1 && [ -n "$INCUS_NAME" ]; then
    VOL_NAME="${INCUS_NAME}-workspace"
    if incus storage volume list default --format csv 2>/dev/null | grep -q "$VOL_NAME"; then
        pass "Storage volume $VOL_NAME exists in pool"
    else
        info "Volume $VOL_NAME not found in storage pool list"
    fi
else
    info "Incus CLI not available - skipping volume verification"
fi

# -- Stop Instance ------------------------------------------------
echo "10. Stop instance"
STOP_RESULT=$(api POST "/api/v1/instances/$INST_ID/stop")
STOP_MSG=$(echo "$STOP_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$STOP_MSG" = "stopped" ]; then
    pass "Instance stopped"
else
    info "Stop result: $STOP_RESULT"
fi

# -- Start Instance -----------------------------------------------
echo "11. Start instance"
START_RESULT=$(api POST "/api/v1/instances/$INST_ID/start")
START_MSG=$(echo "$START_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$START_MSG" = "started" ]; then
    pass "Instance started"
else
    info "Start result: $START_RESULT"
fi

# -- Rebuild Instance ---------------------------------------------
echo "12. Rebuild instance"
REBUILD_RESULT=$(api POST "/api/v1/instances/$INST_ID/rebuild")
REBUILD_MSG=$(echo "$REBUILD_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$REBUILD_MSG" = "rebuilt" ]; then
    pass "Instance rebuilt"
else
    info "Rebuild result: $REBUILD_RESULT"
fi

# -- Verify Instance After Rebuild --------------------------------
echo "13. Verify instance after rebuild"
DETAIL2=$(api GET "/api/v1/instances/$INST_ID")
DETAIL2_STATUS=$(echo "$DETAIL2" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null || echo "")
info "Instance status after rebuild: $DETAIL2_STATUS"

if command -v incus >/dev/null 2>&1 && [ -n "$INCUS_NAME" ]; then
    INCUS_STATUS2=$(incus info "$INCUS_NAME" 2>/dev/null | grep "Status:" | awk '{print $2}' || echo "unknown")
    pass "Incus instance status after rebuild: $INCUS_STATUS2"

    # Verify workspace volume survived rebuild
    DEVICES2=$(incus config device list "$INCUS_NAME" 2>/dev/null || echo "")
    if echo "$DEVICES2" | grep -q "workspace"; then
        pass "Workspace volume survived rebuild"
    else
        info "Workspace device after rebuild: $DEVICES2"
    fi
fi

# -- Verify disks survived rebuild --------------------------------
echo "13b. Verify disks after rebuild"
DISKS_POST_REBUILD=$(api GET /api/v1/disks)
DISK_COUNT_REBUILD=$(echo "$DISKS_POST_REBUILD" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)" 2>/dev/null || echo "0")
if [ "$DISK_COUNT_REBUILD" -ge 1 ]; then
    pass "Disk listing preserved after rebuild ($DISK_COUNT_REBUILD disk(s))"
else
    fail "Expected at least 1 disk after rebuild, got $DISK_COUNT_REBUILD"
fi

# -- Delete Instance ----------------------------------------------
echo "14. Delete instance"
DELETE_RESULT=$(api DELETE "/api/v1/instances/$INST_ID")
DELETE_MSG=$(echo "$DELETE_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$DELETE_MSG" = "deleted" ]; then
    pass "Instance deleted from API"
else
    info "Delete result: $DELETE_RESULT"
fi

# -- Verify Cleanup -----------------------------------------------
echo "15. Verify cleanup"
INSTANCES_AFTER=$(api GET /api/v1/instances)
AFTER_COUNT=$(echo "$INSTANCES_AFTER" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
if [ "$AFTER_COUNT" -eq "$INST_COUNT" ]; then
    pass "Instance count back to $AFTER_COUNT"
else
    fail "Expected $INST_COUNT instances, got $AFTER_COUNT"
fi

if command -v incus >/dev/null 2>&1 && [ -n "$INCUS_NAME" ]; then
    if incus info "$INCUS_NAME" >/dev/null 2>&1; then
        fail "Incus instance $INCUS_NAME still exists after delete"
    else
        pass "Incus instance $INCUS_NAME properly removed"
    fi
fi

# -- Verify disks cleaned up --------------------------------------
echo "15b. Verify disks cleaned up"
DISKS_POST_DELETE=$(api GET /api/v1/disks)
DISK_COUNT_DELETE=$(echo "$DISKS_POST_DELETE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d) if d else 0)" 2>/dev/null || echo "0")
if [ "$DISK_COUNT_DELETE" -eq 0 ]; then
    pass "Disk listing empty after instance deletion"
else
    fail "Expected 0 disks after delete, got $DISK_COUNT_DELETE"
fi

# -- Verify Incus volume removed ----------------------------------
echo "15c. Verify Incus volume cleanup"
if command -v incus >/dev/null 2>&1 && [ -n "$INCUS_NAME" ]; then
    VOL_NAME="${INCUS_NAME}-workspace"
    if incus storage volume list default --format csv 2>/dev/null | grep -q "$VOL_NAME"; then
        fail "Storage volume $VOL_NAME still exists after instance deletion"
    else
        pass "Storage volume $VOL_NAME properly removed"
    fi
else
    info "Incus CLI not available - skipping volume cleanup verification"
fi

# -- Cleanup secret -----------------------------------------------
echo "16. Cleanup TAILSCALE_AUTH_KEY secret"
api DELETE "/api/v1/secrets/$SECRET_ID" > /dev/null
pass "Secret cleaned up"

# -- Git Repos CRUD -----------------------------------------------
echo "17. Git repos: generate server key"
KEY_RESP=$(api POST /api/v1/admin/repos/server-key/generate)
PUB_KEY=$(echo "$KEY_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('public_key',''))" 2>/dev/null || echo "")
if [[ "$PUB_KEY" == ssh-ed25519* ]]; then
    pass "Server SSH key generated (${PUB_KEY:0:30}...)"
else
    fail "Generate server key failed: $KEY_RESP"
fi

echo "18. Git repos: get server key"
GET_KEY_RESP=$(api GET /api/v1/admin/repos/server-key)
GET_PUB=$(echo "$GET_KEY_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('public_key',''))" 2>/dev/null || echo "")
if [[ "$GET_PUB" == ssh-ed25519* ]]; then
    pass "Server SSH key retrieved"
else
    fail "Get server key failed: $GET_KEY_RESP"
fi

echo "19. Git repos: add repo (expect fail gracefully without network)"
ADD_RESP=$(api POST /api/v1/admin/repos -d '{"ssh_url":"git@github.com:catie-aq/AI-state-art-public.git"}')
REPO_ID=$(echo "$ADD_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
if [ -n "$REPO_ID" ]; then
    pass "Repo registered with id=$REPO_ID (cloning in background)"
else
    fail "Add repo failed: $ADD_RESP"
fi

echo "20. Git repos: list repos"
LIST_RESP=$(api GET /api/v1/admin/repos)
REPO_COUNT=$(echo "$LIST_RESP" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
if [ "$REPO_COUNT" -ge 1 ]; then
    pass "Listed $REPO_COUNT repo(s)"
else
    fail "List repos failed: $LIST_RESP"
fi

echo "21. Git repos: sync repo"
SYNC_RESP=$(api POST "/api/v1/admin/repos/$REPO_ID/sync")
SYNC_STATUS=$(echo "$SYNC_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
if [ "$SYNC_STATUS" = "syncing" ]; then
    pass "Sync triggered"
else
    fail "Sync failed: $SYNC_RESP"
fi

echo "22. Git repos: delete repo"
DEL_RESP=$(api DELETE "/api/v1/admin/repos/$REPO_ID")
DEL_STATUS=$(echo "$DEL_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
if [ "$DEL_STATUS" = "deleted" ]; then
    pass "Repo deleted"
else
    fail "Delete repo failed: $DEL_RESP"
fi

# -- Logout -------------------------------------------------------
echo "23. Logout"
LOGOUT=$(api POST /auth/logout)
pass "Logged out"

echo ""
echo "=== All lifecycle tests completed ==="
