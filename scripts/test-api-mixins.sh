#!/usr/bin/env bash
set -euo pipefail

# Plati Mixin API Test Script
#
# Verifies that all non-NVIDIA mixins (tailscale, sshx, docker, openvscode-server,
# claude-code) are correctly loaded, listed, and applied to the Ubuntu template.
# Requires a running Plati backend with templates synced from config/templates/.
#
# Usage: ./scripts/test-api-mixins.sh [BASE_URL] [ADMIN_PASSWORD]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
[ -f "$SCRIPT_DIR/../.env" ] && source "$SCRIPT_DIR/../.env"

BASE_URL="${1:-http://localhost:8080}"
ADMIN_PASSWORD="${2:-${ADMIN_PASSWORD:?Set ADMIN_PASSWORD in .env or pass as arg}}"
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

py() { python3 -c "$@"; }

echo ""
echo "=== Plati Mixin API Test ==="
echo "Backend: $BASE_URL"
echo ""

# -- Health Check -------------------------------------------------
echo "1. Health check"
HEALTH=$(api GET /health)
STATUS=$(py "import sys,json; print(json.loads('$HEALTH')['status'])" 2>/dev/null || echo "error")
if [ "$STATUS" = "ok" ]; then
    pass "Health endpoint returns ok"
else
    fail "Health endpoint returned: $HEALTH"
fi

# -- Admin Login --------------------------------------------------
echo "2. Admin login"
LOGIN=$(api POST /auth/login -d "{\"password\":\"$ADMIN_PASSWORD\"}")
MSG=$(py "import sys,json; print(json.loads('''$LOGIN''').get('message',''))" 2>/dev/null || echo "")
if [ "$MSG" = "ok" ]; then
    pass "Admin login successful"
else
    fail "Login failed: $LOGIN"
fi

# -- List Mixins --------------------------------------------------
echo "3. List mixins via GET /api/v1/admin/templates/mixins"
MIXINS=$(api GET /api/v1/admin/templates/mixins)
MIXIN_COUNT=$(py "import sys,json; print(len(json.loads('''$MIXINS''')))" 2>/dev/null || echo "0")
if [ "$MIXIN_COUNT" -gt 0 ]; then
    pass "Found $MIXIN_COUNT mixin(s)"
else
    fail "No mixins returned — is the backend running with config/templates synced?"
fi

# -- Verify each non-NVIDIA mixin is present ----------------------
echo "4. Verify non-NVIDIA mixins are present"
NON_NVIDIA_MIXINS="tailscale sshx docker openvscode-server claude-code"
MIXIN_NAMES=$(py "import sys,json; print('\n'.join(m['name'] for m in json.loads('''$MIXINS''')))" 2>/dev/null || echo "")

for MIXIN in $NON_NVIDIA_MIXINS; do
    if echo "$MIXIN_NAMES" | grep -qi "$MIXIN"; then
        pass "Mixin '$MIXIN' present"
    else
        fail "Mixin '$MIXIN' not found in mixin list: $MIXIN_NAMES"
    fi
done

# -- Verify NVIDIA mixin is present (but not used in ubuntu) ------
echo "5. Verify NVIDIA mixin exists"
if echo "$MIXIN_NAMES" | grep -qi "nvidia"; then
    pass "Nvidia mixin present (expected — used by ubuntu-gpu)"
else
    info "Nvidia mixin not found (ok if this machine has no GPU template loaded)"
fi

# -- Verify mixin commands are non-empty --------------------------
echo "6. Verify mixin commands are populated"
for MIXIN in $NON_NVIDIA_MIXINS; do
    CMD_COUNT=$(py "
import sys, json
data = json.loads('''$MIXINS''')
for m in data:
    if m['name'].lower() == '$MIXIN' or '$MIXIN' in m['name'].lower():
        print(len(m['commands']))
        break
else:
    print(0)
" 2>/dev/null || echo "0")
    if [ "$CMD_COUNT" -gt 0 ]; then
        pass "Mixin '$MIXIN' has $CMD_COUNT command(s)"
    else
        fail "Mixin '$MIXIN' has no commands"
    fi
done

# -- Verify expected keywords in mixin commands -------------------
echo "7. Verify mixin command keywords"

check_mixin_keyword() {
    local MIXIN="$1"
    local KEYWORD="$2"
    local FOUND
    FOUND=$(py "
import sys, json
data = json.loads('''$MIXINS''')
for m in data:
    if '$MIXIN' in m['name'].lower():
        for cmd in m['commands']:
            if '$KEYWORD' in cmd:
                print('yes')
                break
        break
" 2>/dev/null || echo "")
    if [ "$FOUND" = "yes" ]; then
        pass "Mixin '$MIXIN' contains '$KEYWORD'"
    else
        fail "Mixin '$MIXIN': keyword '$KEYWORD' not found in commands"
    fi
}

check_mixin_keyword "tailscale"         "tailscaled"
check_mixin_keyword "sshx"              "sshx"
check_mixin_keyword "docker"            "docker-ce"
check_mixin_keyword "openvscode-server" "openvscode-server"
check_mixin_keyword "claude-code"       "claude.ai/install.sh"

# -- Find Ubuntu template -----------------------------------------
echo "8. Find Ubuntu template"
TEMPLATES=$(api GET /api/v1/templates)
UBUNTU_ID=$(py "
import sys, json
for t in json.loads('''$TEMPLATES'''):
    if t['slug'] == 'ubuntu':
        print(t['id'])
        break
" 2>/dev/null || echo "")

if [ -z "$UBUNTU_ID" ]; then
    fail "Ubuntu template not found — sync templates from config/templates/ first"
fi
pass "Ubuntu template found (id=$UBUNTU_ID)"

# -- Fetch Ubuntu template detail ---------------------------------
echo "9. Verify Ubuntu template includes all non-NVIDIA mixins"
UBUNTU=$(api GET "/api/v1/templates/$UBUNTU_ID")
UBUNTU_INCLUDES=$(py "
import sys, json
t = json.loads('''$UBUNTU''')
includes = json.loads(t.get('includes', '[]'))
print('\n'.join(includes))
" 2>/dev/null || echo "")

for MIXIN in $NON_NVIDIA_MIXINS; do
    if echo "$UBUNTU_INCLUDES" | grep -q "^${MIXIN}$"; then
        pass "Ubuntu template includes '$MIXIN'"
    else
        fail "Ubuntu template missing mixin '$MIXIN' (includes: $(echo "$UBUNTU_INCLUDES" | tr '\n' ','))"
    fi
done

# -- Verify NVIDIA is NOT in Ubuntu includes ----------------------
echo "10. Verify NVIDIA is NOT in Ubuntu includes"
if echo "$UBUNTU_INCLUDES" | grep -q "^nvidia$"; then
    fail "Ubuntu template should NOT include nvidia mixin"
else
    pass "Ubuntu template does not include nvidia"
fi

# -- Verify mixin commands appear in first_init_commands ----------
echo "11. Verify mixin commands appear in Ubuntu first_init_commands"
UBUNTU_FIRST_INIT=$(py "
import sys, json
t = json.loads('''$UBUNTU''')
cmds = json.loads(t.get('first_init_commands', '[]'))
print('\n'.join(cmds))
" 2>/dev/null || echo "")

FIRST_INIT_KEYWORDS=(
    "tailscale-install.sh:tailscale"
    "tailscaled:tailscale"
    "sshx:sshx"
    "docker-ce:docker"
    "openvscode-server:openvscode-server"
    "claude.ai/install.sh:claude-code"
    "openssh-server:ubuntu-init"
)

for ENTRY in "${FIRST_INIT_KEYWORDS[@]}"; do
    KEYWORD="${ENTRY%%:*}"
    MIXIN="${ENTRY##*:}"
    if echo "$UBUNTU_FIRST_INIT" | grep -q "$KEYWORD"; then
        pass "Ubuntu first_init_commands contains '$KEYWORD' ($MIXIN)"
    else
        fail "Ubuntu first_init_commands missing '$KEYWORD' (from $MIXIN mixin)"
    fi
done

# -- Verify mixin commands precede template's own init commands ---
echo "12. Verify mixin commands precede template-specific init commands"
MIXIN_LINE=$(py "
import sys, json
t = json.loads('''$UBUNTU''')
cmds = json.loads(t.get('first_init_commands', '[]'))
for i, cmd in enumerate(cmds):
    if 'tailscale-install' in cmd:
        print(i)
        break
else:
    print(-1)
" 2>/dev/null || echo "-1")

INIT_LINE=$(py "
import sys, json
t = json.loads('''$UBUNTU''')
cmds = json.loads(t.get('first_init_commands', '[]'))
for i, cmd in enumerate(cmds):
    if 'openssh-server' in cmd:
        print(i)
        break
else:
    print(-1)
" 2>/dev/null || echo "-1")

if [ "$MIXIN_LINE" = "-1" ]; then
    fail "tailscale-install command not found in first_init_commands"
elif [ "$INIT_LINE" = "-1" ]; then
    fail "openssh-server command not found in first_init_commands"
elif [ "$MIXIN_LINE" -lt "$INIT_LINE" ]; then
    pass "Mixin commands (line $MIXIN_LINE) precede template init (line $INIT_LINE)"
else
    fail "Mixin commands at line $MIXIN_LINE should precede template init at line $INIT_LINE"
fi

# -- Verify Ubuntu resources (sanity check) -----------------------
echo "13. Verify Ubuntu template resources"
UBUNTU_CPU=$(py "
import sys, json
t = json.loads('''$UBUNTU''')
r = json.loads(t.get('resources', '{}'))
print(r.get('cpu', ''))
" 2>/dev/null || echo "")
if [ -n "$UBUNTU_CPU" ]; then
    pass "Ubuntu template has CPU resource: $UBUNTU_CPU"
else
    fail "Ubuntu template resources missing or malformed"
fi

# -- Logout -------------------------------------------------------
echo "14. Logout"
api POST /auth/logout > /dev/null
pass "Logged out"

echo ""
echo "=== All mixin tests passed ==="
echo "  Verified: 5 non-NVIDIA mixins (tailscale, sshx, docker, openvscode-server, claude-code)"
echo "  Verified: Ubuntu template includes all non-NVIDIA mixins"
echo "  Verified: Mixin commands precede template init commands in first_init_commands"
