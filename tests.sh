#!/usr/bin/env bash
# tests.sh — run all Plati tests
#
# Usage:
#   ./tests.sh                        # unit + integration only (no server needed)
#   ./tests.sh --all                  # all suites (requires running stack)
#   ./tests.sh --url http://host:8080 --password <pw> [--tailscale-key <key>]
#
# Suites:
#   unit          Go unit tests (instances_test.go)
#   integration   Go integration tests (mock Incus, in-memory DB)
#   screenshots   Playwright screenshot tests (requires dev stack)
#   api-lifecycle Shell API lifecycle test (requires server + Incus)
#   api-admin     Shell API admin test (requires server)
#   selenium      Python Selenium E2E (requires dev stack + Chrome)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- defaults ---
RUN_UNIT=true
RUN_INTEGRATION=true
RUN_SCREENSHOTS=false
RUN_API_LIFECYCLE=false
RUN_API_ADMIN=false
RUN_SELENIUM=false
RUN_IMAGE=false
RUN_E2E_INSTANCE=false
SERVER_URL="http://localhost:8080"
# Docker compose runs the frontend dev server on port 5300 (not the default 5173)
FRONTEND_URL="http://localhost:5300"
ADMIN_PASSWORD="12345678"
TAILSCALE_AUTH_KEY="tskey-auth-kRqVWCq18Z11CNTRL-qZFfkoAyAcMT1NYmuVmbbMPZwnK3opj1"

# --- parse args ---
while [[ $# -gt 0 ]]; do
  case "$1" in
    --all)           RUN_SCREENSHOTS=true; RUN_API_LIFECYCLE=true; RUN_API_ADMIN=true; RUN_SELENIUM=true; RUN_IMAGE=true; RUN_E2E_INSTANCE=true; shift ;;
    --screenshots)   RUN_SCREENSHOTS=true; shift ;;
    --api-lifecycle) RUN_API_LIFECYCLE=true; shift ;;
    --api-admin)     RUN_API_ADMIN=true; shift ;;
    --selenium)      RUN_SELENIUM=true; shift ;;
    --image)         RUN_IMAGE=true; shift ;;
    --e2e-instance)  RUN_E2E_INSTANCE=true; shift ;;
    --url)            SERVER_URL="$2"; shift 2 ;;
    --frontend-url)   FRONTEND_URL="$2"; shift 2 ;;
    --password)       ADMIN_PASSWORD="$2"; shift 2 ;;
    --tailscale-key)  TAILSCALE_AUTH_KEY="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# --- helpers ---
PASS=0
FAIL=0

run_suite() {
  local name="$1"; shift
  echo ""
  echo "==> $name"
  if "$@"; then
    echo "    PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "    FAIL: $name"
    FAIL=$((FAIL + 1))
  fi
}

# --- suites ---

suite_unit() {
  cd "$ROOT/backend"
  go test -v ./internal/incus/...
}

suite_integration() {
  cd "$ROOT/backend"
  go test -v ./internal/integration/...
}

suite_screenshots() {
  cd "$ROOT/frontend"
  BASE_URL="$FRONTEND_URL" ADMIN_PASSWORD="$ADMIN_PASSWORD" npm run screenshots
}

suite_api_lifecycle() {
  if [[ -z "$ADMIN_PASSWORD" ]]; then
    echo "    SKIP: --password required for api-lifecycle"
    return 1
  fi
  TAILSCALE_AUTH_KEY="$TAILSCALE_AUTH_KEY" "$ROOT/scripts/test-api-lifecycle.sh" "$SERVER_URL" "$ADMIN_PASSWORD"
}

suite_api_admin() {
  if [[ -z "$ADMIN_PASSWORD" ]]; then
    echo "    SKIP: --password required for api-admin"
    return 1
  fi
  "$ROOT/scripts/test-api-admin.sh" "$SERVER_URL" "$ADMIN_PASSWORD"
}

suite_selenium() {
  python3 "$ROOT/tests/selenium_tests.py"
}

suite_image() {
  cd "$ROOT/backend"
  TAILSCALE_AUTH_KEY="$TAILSCALE_AUTH_KEY" \
    go test -v -timeout 30m -tags e2e ./internal/e2e/...
}

suite_e2e_instance() {
  if [[ -z "$ADMIN_PASSWORD" ]]; then
    echo "    SKIP: --password required for e2e-instance"
    return 1
  fi
  TAILSCALE_AUTH_KEY="$TAILSCALE_AUTH_KEY" \
    "$ROOT/scripts/test-instance-e2e.sh" "$SERVER_URL" "$ADMIN_PASSWORD"
}

# --- run ---

$RUN_UNIT        && run_suite "Go unit tests"        suite_unit
$RUN_INTEGRATION && run_suite "Go integration tests" suite_integration
$RUN_SCREENSHOTS && run_suite "Playwright screenshots" suite_screenshots
$RUN_API_LIFECYCLE && run_suite "Shell API lifecycle"  suite_api_lifecycle
$RUN_API_ADMIN   && run_suite "Shell API admin"       suite_api_admin
$RUN_SELENIUM    && run_suite "Python Selenium E2E"   suite_selenium
$RUN_IMAGE         && run_suite "Image (e2e) tests"        suite_image
$RUN_E2E_INSTANCE  && run_suite "Instance E2E tests"      suite_e2e_instance

# --- summary ---
echo ""
echo "Results: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
