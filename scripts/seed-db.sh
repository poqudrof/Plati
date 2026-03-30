#!/usr/bin/env bash
set -euo pipefail

DB_PATH="${1:-plati.db}"

echo "=== Seeding Plati Database ==="

# Generate admin password hash
if [ -z "${ADMIN_PASSWORD:-}" ]; then
    echo "Set ADMIN_PASSWORD env var or enter admin password:"
    read -s -p "Password: " ADMIN_PASSWORD
    echo
fi

# Use the Go backend to hash the password
HASH=$(cd backend && go run -tags seed -exec "echo" ./cmd/plati-server 2>/dev/null || true)

# Fallback: use htpasswd or python
if command -v python3 >/dev/null 2>&1; then
    HASH=$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'${ADMIN_PASSWORD}', bcrypt.gensalt()).decode())" 2>/dev/null || true)
fi

if [ -n "$HASH" ]; then
    echo "Admin password hash: $HASH"
    echo "Add this to config/plati.yaml as admin_password_hash"
else
    echo "Install python3-bcrypt to generate password hash:"
    echo "  pip install bcrypt"
    echo "  python3 -c \"import bcrypt; print(bcrypt.hashpw(b'yourpassword', bcrypt.gensalt()).decode())\""
fi

echo ""
echo "=== Seed complete ==="
