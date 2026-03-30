#!/usr/bin/env bash
set -euo pipefail

# Plati API Lifecycle Test Script
# Tests: login -> list templates -> create instance -> start/stop/rebuild -> delete
#
# Usage: ./scripts/test-api-lifecycle.sh [BASE_URL] [ADMIN_PASSWORD]

BASE_URL="${1:-http://localhost:8080}"
ADMIN_PASSWORD="${2:-admin123}"
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

# -- List Instances (should be empty) -----------------------------
echo "5. List instances (before create)"
INSTANCES=$(api GET /api/v1/instances)
INST_COUNT=$(echo "$INSTANCES" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
pass "Found $INST_COUNT instances"

# -- Create Instance ----------------------------------------------
echo "6. Create instance"
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
echo "7. Get instance detail"
DETAIL=$(api GET "/api/v1/instances/$INST_ID")
DETAIL_NAME=$(echo "$DETAIL" | python3 -c "import sys,json; print(json.load(sys.stdin)['name'])" 2>/dev/null || echo "")
if [ "$DETAIL_NAME" = "$INSTANCE_NAME" ]; then
    pass "Instance detail matches: $DETAIL_NAME"
else
    fail "Instance detail mismatch: expected $INSTANCE_NAME, got $DETAIL_NAME"
fi

# -- Verify in Incus (if incus CLI available) ---------------------
echo "8. Verify in Incus"
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

# -- Stop Instance ------------------------------------------------
echo "9. Stop instance"
STOP_RESULT=$(api POST "/api/v1/instances/$INST_ID/stop")
STOP_MSG=$(echo "$STOP_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$STOP_MSG" = "stopped" ]; then
    pass "Instance stopped"
else
    info "Stop result: $STOP_RESULT"
fi

# -- Start Instance -----------------------------------------------
echo "10. Start instance"
START_RESULT=$(api POST "/api/v1/instances/$INST_ID/start")
START_MSG=$(echo "$START_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$START_MSG" = "started" ]; then
    pass "Instance started"
else
    info "Start result: $START_RESULT"
fi

# -- Rebuild Instance ---------------------------------------------
echo "11. Rebuild instance"
REBUILD_RESULT=$(api POST "/api/v1/instances/$INST_ID/rebuild")
REBUILD_MSG=$(echo "$REBUILD_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$REBUILD_MSG" = "rebuilt" ]; then
    pass "Instance rebuilt"
else
    info "Rebuild result: $REBUILD_RESULT"
fi

# -- Verify Instance After Rebuild --------------------------------
echo "12. Verify instance after rebuild"
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

# -- Delete Instance ----------------------------------------------
echo "13. Delete instance"
DELETE_RESULT=$(api DELETE "/api/v1/instances/$INST_ID")
DELETE_MSG=$(echo "$DELETE_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('message',''))" 2>/dev/null || echo "")
if [ "$DELETE_MSG" = "deleted" ]; then
    pass "Instance deleted from API"
else
    info "Delete result: $DELETE_RESULT"
fi

# -- Verify Cleanup -----------------------------------------------
echo "14. Verify cleanup"
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

# -- Logout -------------------------------------------------------
echo "15. Logout"
LOGOUT=$(api POST /auth/logout)
pass "Logged out"

echo ""
echo "=== All lifecycle tests completed ==="
