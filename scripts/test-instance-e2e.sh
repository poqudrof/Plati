#!/usr/bin/env bash
set -euo pipefail

# Plati Instance E2E Test
# Full end-to-end test: create instance from template, verify health checks,
# test Tailscale connectivity (HTTPS + SSH), test tailscale serve API, cleanup.
#
# Usage:
#   ./scripts/test-instance-e2e.sh [BASE_URL] [ADMIN_PASSWORD]
#   ./scripts/test-instance-e2e.sh -t simple-webserver -i
#   ./scripts/test-instance-e2e.sh --template config/templates/ubuntu.yaml --interactive
#
# Options:
#   -t, --template SLUG|FILE   Template slug or YAML file path (default: simple-webserver)
#   -i, --interactive          Pause after setup for manual testing
#   --url URL                  Backend URL (default: http://localhost:8080)
#   --password PASS            Admin password
#   --frontend-url URL         Plati frontend URL for monitoring link (default: http://localhost:5173)
#   --timeout SECS             Instance creation timeout (default: 300)
#
# Env vars:
#   TAILSCALE_AUTH_KEY   Required for Tailscale connectivity tests
#   ADMIN_PASSWORD       Admin password (can also use --password)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
[ -f "$SCRIPT_DIR/../.env" ] && source "$SCRIPT_DIR/../.env"

# ── Defaults ──────────────────────────────────────────────────────────────────

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
TAILSCALE_AUTH_KEY="${TAILSCALE_AUTH_KEY:-}"
TEMPLATE_SLUG="${TEMPLATE_SLUG:-simple-webserver}"
CREATION_TIMEOUT="${CREATION_TIMEOUT:-300}"
FRONTEND_URL="${FRONTEND_URL:-http://localhost:5173}"
INTERACTIVE=false

# ── Argument parsing ─────────────────────────────────────────────────────────

POSITIONAL=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        -t|--template)
            TEMPLATE_ARG="$2"
            # If it's a file path, extract slug from the YAML filename
            if [[ -f "$TEMPLATE_ARG" ]]; then
                TEMPLATE_SLUG=$(basename "$TEMPLATE_ARG" .yaml)
            elif [[ -f "$SCRIPT_DIR/../config/templates/$TEMPLATE_ARG" ]]; then
                TEMPLATE_SLUG=$(basename "$TEMPLATE_ARG" .yaml)
            else
                TEMPLATE_SLUG="$TEMPLATE_ARG"
            fi
            shift 2
            ;;
        -i|--interactive)
            INTERACTIVE=true
            shift
            ;;
        --url)
            BASE_URL="$2"
            shift 2
            ;;
        --password)
            ADMIN_PASSWORD="$2"
            shift 2
            ;;
        --frontend-url)
            FRONTEND_URL="$2"
            shift 2
            ;;
        --timeout)
            CREATION_TIMEOUT="$2"
            shift 2
            ;;
        --tailscale-key)
            TAILSCALE_AUTH_KEY="$2"
            shift 2
            ;;
        -*)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
        *)
            POSITIONAL+=("$1")
            shift
            ;;
    esac
done

# Backward compat: positional args [BASE_URL] [ADMIN_PASSWORD]
if [[ ${#POSITIONAL[@]} -ge 1 ]]; then
    BASE_URL="${POSITIONAL[0]}"
fi
if [[ ${#POSITIONAL[@]} -ge 2 ]]; then
    ADMIN_PASSWORD="${POSITIONAL[1]}"
fi

if [[ -z "$ADMIN_PASSWORD" ]]; then
    echo "Error: ADMIN_PASSWORD required (set in .env, use --password, or pass as 2nd arg)" >&2
    exit 1
fi

# ── State ─────────────────────────────────────────────────────────────────────

COOKIE_JAR=$(mktemp)
INST_ID=""
HC_PASSED=0
HC_TOTAL=0

cleanup() {
    if [ -n "$INST_ID" ]; then
        echo ""
        info "Cleaning up instance $INST_ID..."
        api DELETE "/api/v1/instances/$INST_ID" > /dev/null 2>&1 || true
    fi
    rm -f "$COOKIE_JAR"
}
trap cleanup EXIT

# ── Helpers ───────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

pass()    { echo -e "${GREEN}  PASS${NC}: $1"; }
fail()    { echo -e "${RED}  FAIL${NC}: $1"; exit 1; }
info()    { echo -e "${YELLOW}  INFO${NC}: $1"; }
section() { echo -e "\n${CYAN}--- $1 ---${NC}"; }

api() {
    local method="$1"
    local path="$2"
    shift 2
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
        -X "$method" \
        -H "Content-Type: application/json" \
        "${BASE_URL}${path}" "$@"
}

jq_field() {
    python3 -c "import sys,json; print(json.load(sys.stdin)$1)" 2>/dev/null
}

# ── Banner ────────────────────────────────────────────────────────────────────

echo ""
echo "========================================="
echo "  Plati Instance E2E Test"
echo "========================================="
echo "Backend:     $BASE_URL"
echo "Frontend:    $FRONTEND_URL"
echo "Template:    $TEMPLATE_SLUG"
echo "Interactive: $INTERACTIVE"
echo "Tailscale:   ${TAILSCALE_AUTH_KEY:+configured}${TAILSCALE_AUTH_KEY:-not set (skipping network tests)}"
echo ""

# ============================================================
section "1. Health Check & Login"
# ============================================================

HEALTH=$(api GET /health)
STATUS=$(echo "$HEALTH" | jq_field "['status']" || echo "error")
[ "$STATUS" = "ok" ] && pass "Backend healthy" || fail "Backend unhealthy: $HEALTH"

LOGIN=$(api POST /auth/login -d "{\"password\":\"$ADMIN_PASSWORD\"}")
MSG=$(echo "$LOGIN" | jq_field ".get('message','')" || echo "")
[ "$MSG" = "ok" ] && pass "Admin login" || fail "Login failed: $LOGIN"

# ============================================================
section "2. Seed Tailscale Key (if provided)"
# ============================================================

if [ -n "$TAILSCALE_AUTH_KEY" ]; then
    api PUT /api/v1/admin/settings/tailscale-key \
        -d "{\"key\":\"$TAILSCALE_AUTH_KEY\"}" > /dev/null
    pass "Platform Tailscale key configured"
else
    info "No TAILSCALE_AUTH_KEY — Tailscale tests will be skipped"
fi

# ============================================================
section "3. Find Template"
# ============================================================

TEMPLATES=$(api GET /api/v1/templates)
TEMPLATE_ID=$(echo "$TEMPLATES" | python3 -c "
import sys, json
templates = json.load(sys.stdin)
for t in templates:
    if t['slug'] == '$TEMPLATE_SLUG':
        print(t['id'])
        break
else:
    print('')
" 2>/dev/null || echo "")

if [ -z "$TEMPLATE_ID" ]; then
    fail "Template '$TEMPLATE_SLUG' not found. Available: $(echo "$TEMPLATES" | python3 -c "import sys,json; print(', '.join(t['slug'] for t in json.load(sys.stdin)))" 2>/dev/null)"
fi
pass "Found template: $TEMPLATE_SLUG (id=$TEMPLATE_ID)"

# Get health checks from template detail
TEMPLATE_DETAIL=$(api GET "/api/v1/templates/$TEMPLATE_ID")
HEALTH_CHECKS=$(echo "$TEMPLATE_DETAIL" | python3 -c "import sys,json; print(json.load(sys.stdin).get('health_checks','[]'))" 2>/dev/null || echo "[]")
TERMINAL_USER=$(echo "$TEMPLATE_DETAIL" | python3 -c "import sys,json; print(json.load(sys.stdin).get('terminal_user','root'))" 2>/dev/null || echo "root")
info "Health checks: $HEALTH_CHECKS"

# ============================================================
section "4. Create Instance"
# ============================================================

INSTANCE_NAME="e2e-test-$(date +%s)"
CREATE_RESULT=$(api POST /api/v1/instances -d "{\"name\":\"$INSTANCE_NAME\",\"template_id\":$TEMPLATE_ID}")
INST_ID=$(echo "$CREATE_RESULT" | jq_field ".get('id','')" || echo "")
INCUS_NAME=$(echo "$CREATE_RESULT" | jq_field ".get('incus_name','')" || echo "")

if [ -z "$INST_ID" ]; then
    ERROR=$(echo "$CREATE_RESULT" | jq_field ".get('error','unknown')" || echo "unknown")
    fail "Instance creation failed: $ERROR"
fi
pass "Instance created: id=$INST_ID incus=$INCUS_NAME"

# ============================================================
section "5. Wait for Instance Ready"
# ============================================================

ELAPSED=0
POLL_INTERVAL=5
CONTAINER_IP=""
while [ "$ELAPSED" -lt "$CREATION_TIMEOUT" ]; do
    DETAIL=$(api GET "/api/v1/instances/$INST_ID")
    INST_STATUS=$(echo "$DETAIL" | jq_field "['status']" || echo "unknown")

    case "$INST_STATUS" in
        running)
            CONTAINER_IP=$(echo "$DETAIL" | python3 -c "import sys,json; d=json.load(sys.stdin); ip=d.get('ip_address'); print(ip if ip else '')" 2>/dev/null || echo "")
            pass "Instance running after ${ELAPSED}s"
            break
            ;;
        error)
            LOG=$(echo "$DETAIL" | jq_field "['creation_log']" || echo "no log")
            fail "Instance creation error after ${ELAPSED}s. Log:\n$LOG"
            ;;
        creating)
            printf "  ... creating (%ds / %ds)\r" "$ELAPSED" "$CREATION_TIMEOUT"
            sleep "$POLL_INTERVAL"
            ELAPSED=$((ELAPSED + POLL_INTERVAL))
            ;;
        *)
            info "Unexpected status: $INST_STATUS"
            sleep "$POLL_INTERVAL"
            ELAPSED=$((ELAPSED + POLL_INTERVAL))
            ;;
    esac
done
echo "" # clear progress line

if [ "$ELAPSED" -ge "$CREATION_TIMEOUT" ]; then
    fail "Instance creation timed out after ${CREATION_TIMEOUT}s"
fi

# Give services a few more seconds to stabilize
info "Waiting 10s for services to stabilize..."
sleep 10

# ============================================================
section "6. Health Checks (in-container)"
# ============================================================

HC_COUNT=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(len(hc))" 2>/dev/null || echo "0")
HC_TOTAL=$HC_COUNT

if [ "$HC_COUNT" -eq 0 ]; then
    info "No health checks defined in template — skipping"
else
    for i in $(seq 0 $((HC_COUNT - 1))); do
        HC_PORT=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[$i]['port'])" 2>/dev/null)
        HC_PATH=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[$i]['path'])" 2>/dev/null)
        HC_STATUS=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[$i]['expected_status'])" 2>/dev/null)
        HC_DESC=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[$i].get('description',''))" 2>/dev/null)
        HC_TIMEOUT=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[$i].get('timeout', 60))" 2>/dev/null)

        info "Checking: $HC_DESC (localhost:$HC_PORT$HC_PATH -> $HC_STATUS)"

        HC_ELAPSED=0
        HC_OK=false
        while [ "$HC_ELAPSED" -lt "$HC_TIMEOUT" ]; do
            EXEC_RESULT=$(api POST "/api/v1/admin/instances/$INST_ID/exec" \
                -d "{\"command\":\"curl -s -o /dev/null -w '%{http_code}' http://localhost:${HC_PORT}${HC_PATH}\"}")
            ACTUAL_STATUS=$(echo "$EXEC_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('output','').strip())" 2>/dev/null || echo "")

            if [ "$ACTUAL_STATUS" = "$HC_STATUS" ]; then
                HC_OK=true
                break
            fi
            sleep 3
            HC_ELAPSED=$((HC_ELAPSED + 3))
        done

        if $HC_OK; then
            pass "Health check: $HC_DESC (status=$ACTUAL_STATUS after ${HC_ELAPSED}s)"
            HC_PASSED=$((HC_PASSED + 1))
        else
            fail "Health check failed: $HC_DESC — expected $HC_STATUS, got $ACTUAL_STATUS after ${HC_TIMEOUT}s"
        fi

        # Also verify response body
        BODY_RESULT=$(api POST "/api/v1/admin/instances/$INST_ID/exec" \
            -d "{\"command\":\"curl -s http://localhost:${HC_PORT}${HC_PATH}\"}")
        BODY=$(echo "$BODY_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('output','').strip())" 2>/dev/null || echo "")
        info "Response body: $BODY"
    done
fi

# ============================================================
section "7. Tailscale Status"
# ============================================================

TS_CONNECTED="False"
TS_DNS=""

TS_STATUS=$(api GET "/api/v1/instances/$INST_ID/tailscale-status")
TS_CONNECTED=$(echo "$TS_STATUS" | jq_field "['connected']" || echo "False")
TS_DNS=$(echo "$TS_STATUS" | jq_field ".get('dns_name','')" || echo "")

if [ "$TS_CONNECTED" = "True" ] && [ -n "$TS_DNS" ]; then
    pass "Tailscale connected: $TS_DNS"
else
    if [ -n "$TAILSCALE_AUTH_KEY" ]; then
        info "Tailscale not connected yet (connected=$TS_CONNECTED, dns=$TS_DNS)"
        for attempt in $(seq 1 6); do
            sleep 10
            TS_STATUS=$(api GET "/api/v1/instances/$INST_ID/tailscale-status")
            TS_CONNECTED=$(echo "$TS_STATUS" | jq_field "['connected']" || echo "False")
            TS_DNS=$(echo "$TS_STATUS" | jq_field ".get('dns_name','')" || echo "")
            if [ "$TS_CONNECTED" = "True" ] && [ -n "$TS_DNS" ]; then
                pass "Tailscale connected (attempt $attempt): $TS_DNS"
                break
            fi
        done
    else
        info "Tailscale not configured — skipping connectivity tests"
    fi
fi

# ============================================================
section "8. Tailscale Serve API"
# ============================================================

SERVE_URL=""

SERVE_STATUS=$(api GET "/api/v1/instances/$INST_ID/tailscale-serve")
SERVE_STATE=$(echo "$SERVE_STATUS" | jq_field ".get('status','')" || echo "")
SERVE_URL=$(echo "$SERVE_STATUS" | jq_field ".get('url','')" || echo "")

if [ "$SERVE_STATE" = "active" ]; then
    pass "Tailscale Serve active (auto-configured): $SERVE_URL"
else
    info "Tailscale Serve not auto-active, testing manual start..."
    SERVE_START=$(api POST "/api/v1/instances/$INST_ID/tailscale-serve" -d '{"port":3000}')
    SERVE_STATE=$(echo "$SERVE_START" | jq_field ".get('status','')" || echo "")
    SERVE_URL=$(echo "$SERVE_START" | jq_field ".get('url','')" || echo "")

    if [ "$SERVE_STATE" = "active" ]; then
        pass "Tailscale Serve started: $SERVE_URL"
    else
        info "Tailscale Serve start result: $SERVE_START"
    fi
fi

SERVE_CHECK=$(api GET "/api/v1/instances/$INST_ID/tailscale-serve")
SERVE_CHECK_STATE=$(echo "$SERVE_CHECK" | jq_field ".get('status','')" || echo "")
if [ "$SERVE_CHECK_STATE" = "active" ]; then
    pass "Tailscale Serve status confirmed active"
fi

# ============================================================
section "9. HTTPS via Tailscale (from host)"
# ============================================================

if [ "$TS_CONNECTED" = "True" ] && [ -n "$SERVE_URL" ]; then
    info "Testing HTTPS: $SERVE_URL/health"
    sleep 5

    HTTPS_OK=false
    for attempt in $(seq 1 6); do
        HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 --max-time 10 "$SERVE_URL/health" 2>/dev/null || echo "000")
        if [ "$HTTP_CODE" = "200" ]; then
            HTTPS_OK=true
            break
        fi
        info "Attempt $attempt: HTTP $HTTP_CODE (retrying...)"
        sleep 5
    done

    if $HTTPS_OK; then
        HTTPS_BODY=$(curl -s --max-time 10 "$SERVE_URL/health" 2>/dev/null || echo "")
        pass "HTTPS reachable via Tailscale: $SERVE_URL/health -> $HTTPS_BODY"
    else
        info "HTTPS not reachable from host (may need Tailscale on this machine)"
    fi
else
    info "Skipping HTTPS test (no Tailscale Serve URL)"
fi

# ============================================================
section "10. SSH via Tailscale"
# ============================================================

TS_HOSTNAME=""
if [ "$TS_CONNECTED" = "True" ] && [ -n "$TS_DNS" ]; then
    TS_HOSTNAME=$(echo "$TS_DNS" | sed 's/\.$//')

    info "Testing SSH to $TS_HOSTNAME..."
    SSH_USER="${TERMINAL_USER:-ubuntu}"
    SSH_RESULT=$(ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
        -o BatchMode=yes "${SSH_USER}@${TS_HOSTNAME}" "echo plati-ssh-ok" 2>/dev/null || echo "ssh-failed")

    if [ "$SSH_RESULT" = "plati-ssh-ok" ]; then
        pass "SSH via Tailscale: ${SSH_USER}@${TS_HOSTNAME}"
    else
        info "SSH test result: $SSH_RESULT (may need SSH keys configured)"
    fi
else
    info "Skipping SSH test (no Tailscale connectivity)"
fi

# ============================================================
section "Instance Access Info"
# ============================================================

SSH_USER="${TERMINAL_USER:-ubuntu}"

echo ""
echo -e "${BOLD}=========================================${NC}"
echo -e "${BOLD}  Instance Ready${NC}"
echo -e "${BOLD}=========================================${NC}"
echo ""
echo -e "  ${CYAN}Instance ID${NC}:    $INST_ID"
echo -e "  ${CYAN}Incus Name${NC}:     $INCUS_NAME"
echo -e "  ${CYAN}Template${NC}:       $TEMPLATE_SLUG"
echo -e "  ${CYAN}Plati UI${NC}:       ${FRONTEND_URL}/instances/${INST_ID}"
echo ""
if [ -n "$CONTAINER_IP" ]; then
    echo -e "  ${CYAN}Container IP${NC}:   $CONTAINER_IP"
fi
if [ "$TS_CONNECTED" = "True" ] && [ -n "$TS_HOSTNAME" ]; then
    echo -e "  ${CYAN}Tailscale DNS${NC}:  $TS_HOSTNAME"
    if [ -n "$SERVE_URL" ] && [ "$SERVE_CHECK_STATE" = "active" ]; then
        echo -e "  ${CYAN}HTTPS URL${NC}:      ${SERVE_URL}/"
    fi
    echo -e "  ${CYAN}SSH${NC}:            ssh ${SSH_USER}@${TS_HOSTNAME}"
fi
echo ""
if [ "$HC_TOTAL" -gt 0 ]; then
    echo -e "  ${CYAN}Health Checks${NC}:  ${HC_PASSED}/${HC_TOTAL} passed"
else
    echo -e "  ${CYAN}Health Checks${NC}:  none defined"
fi
echo ""
echo -e "${BOLD}=========================================${NC}"

# ── Interactive pause ─────────────────────────────────────────────────────────

if $INTERACTIVE; then
    echo ""
    echo -e "  ${YELLOW}Interactive mode — instance is running.${NC}"
    echo -e "  ${YELLOW}Test manually, then press ENTER to delete and exit.${NC}"
    echo ""
    read -r -p "  Press ENTER to continue to cleanup... "
    echo ""

    # In interactive mode, skip lifecycle tests — user already tested manually.
    # Go straight to cleanup.
    section "Cleanup"

    # Turn off tailscale serve if active
    if [ "$SERVE_CHECK_STATE" = "active" ]; then
        api DELETE "/api/v1/instances/$INST_ID/tailscale-serve" > /dev/null 2>&1 || true
        info "Tailscale Serve stopped"
    fi

    DELETE_RESULT=$(api DELETE "/api/v1/instances/$INST_ID")
    DELETE_MSG=$(echo "$DELETE_RESULT" | jq_field ".get('message','')" || echo "")
    if [ "$DELETE_MSG" = "deleted" ]; then
        pass "Instance deleted"
        INST_ID=""
    else
        info "Delete result: $DELETE_RESULT"
    fi

    echo ""
    echo "========================================="
    echo "  E2E Instance Test Complete (interactive)"
    echo "========================================="
    exit 0
fi

# ── Automated continuation ────────────────────────────────────────────────────

# ============================================================
section "11. Stop Tailscale Serve"
# ============================================================

SERVE_OFF=$(api DELETE "/api/v1/instances/$INST_ID/tailscale-serve")
SERVE_OFF_CHECK=$(api GET "/api/v1/instances/$INST_ID/tailscale-serve")
SERVE_OFF_STATE=$(echo "$SERVE_OFF_CHECK" | jq_field ".get('status','')" || echo "")
if [ "$SERVE_OFF_STATE" = "off" ]; then
    pass "Tailscale Serve stopped"
else
    info "Tailscale Serve off result: $SERVE_OFF_STATE"
fi

# ============================================================
section "12. Instance Lifecycle"
# ============================================================

# Stop
STOP_RESULT=$(api POST "/api/v1/instances/$INST_ID/stop")
STOP_MSG=$(echo "$STOP_RESULT" | jq_field ".get('message','')" || echo "")
[ "$STOP_MSG" = "stopped" ] && pass "Instance stopped" || info "Stop: $STOP_RESULT"

# Start
START_RESULT=$(api POST "/api/v1/instances/$INST_ID/start")
START_MSG=$(echo "$START_RESULT" | jq_field ".get('message','')" || echo "")
[ "$START_MSG" = "started" ] && pass "Instance started" || info "Start: $START_RESULT"

# Wait for it to be running again
sleep 10

# Re-check health after restart
if [ "$HC_COUNT" -gt 0 ]; then
    HC_PORT=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[0]['port'])" 2>/dev/null)
    HC_PATH=$(echo "$HEALTH_CHECKS" | python3 -c "import sys,json; hc=json.loads(sys.stdin.read()); print(hc[0]['path'])" 2>/dev/null)

    RESTART_HC_OK=false
    for attempt in $(seq 1 10); do
        EXEC_RESULT=$(api POST "/api/v1/admin/instances/$INST_ID/exec" \
            -d "{\"command\":\"curl -s -o /dev/null -w '%{http_code}' http://localhost:${HC_PORT}${HC_PATH}\"}" 2>/dev/null)
        ACTUAL=$(echo "$EXEC_RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('output','').strip())" 2>/dev/null || echo "")
        if [ "$ACTUAL" = "200" ]; then
            RESTART_HC_OK=true
            break
        fi
        sleep 3
    done
    $RESTART_HC_OK && pass "Health check passes after restart" || info "Health check not yet passing after restart"
fi

# ============================================================
section "13. Delete Instance"
# ============================================================

DELETE_RESULT=$(api DELETE "/api/v1/instances/$INST_ID")
DELETE_MSG=$(echo "$DELETE_RESULT" | jq_field ".get('message','')" || echo "")
if [ "$DELETE_MSG" = "deleted" ]; then
    pass "Instance deleted"
    INST_ID="" # prevent double-delete in trap
else
    info "Delete result: $DELETE_RESULT"
fi

# Verify gone
DETAIL_AFTER=$(api GET "/api/v1/instances/$INST_ID" 2>/dev/null || echo "")
if echo "$DETAIL_AFTER" | grep -q "not found" 2>/dev/null; then
    pass "Instance confirmed gone"
fi

# ============================================================
echo ""
echo "========================================="
echo "  E2E Instance Test Complete"
echo "========================================="
