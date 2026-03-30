#!/usr/bin/env bash
set -euo pipefail

echo "=== Plati Dev Setup ==="

# Check dependencies
command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed"; exit 1; }
command -v node >/dev/null 2>&1 || { echo "Error: Node.js is not installed"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "Error: npm is not installed"; exit 1; }

# Config
if [ ! -f config/plati.yaml ]; then
    cp config/plati.yaml.example config/plati.yaml
    echo "Created config/plati.yaml - please edit it with your settings"
else
    echo "config/plati.yaml already exists"
fi

# Backend dependencies
echo "Installing Go dependencies..."
cd backend
go mod tidy
cd ..

# Frontend dependencies
echo "Installing frontend dependencies..."
cd frontend
npm install
cd ..

# Generate JWT secret if not set
if grep -q 'jwt_secret: ""' config/plati.yaml 2>/dev/null; then
    JWT_SECRET=$(openssl rand -hex 32)
    sed -i "s/jwt_secret: \"\"/jwt_secret: \"$JWT_SECRET\"/" config/plati.yaml
    echo "Generated JWT secret"
fi

# Generate encryption key if not set
if grep -q 'secret_encryption_key: ""' config/plati.yaml 2>/dev/null; then
    ENC_KEY=$(openssl rand -hex 32)
    sed -i "s/secret_encryption_key: \"\"/secret_encryption_key: \"$ENC_KEY\"/" config/plati.yaml
    echo "Generated encryption key"
fi

echo ""
echo "=== Setup complete ==="
echo "Edit config/plati.yaml with your Entra ID credentials and Incus server details."
echo "Then run: make dev"
