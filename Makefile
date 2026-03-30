.PHONY: all backend frontend dev dev-backend dev-frontend build clean test test-e2e test-integration migrate seed

all: build

# Development
dev:
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd backend && go run ./cmd/plati-server -config ../config/plati.yaml

dev-frontend:
	cd frontend && npm run dev

# Build
build: build-backend build-frontend

build-backend:
	cd backend && go build -o plati-server ./cmd/plati-server

build-frontend:
	cd frontend && npm run build

# Test
test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test

test-e2e:
	python3 tests/selenium_tests.py

# Test d'intégration système complet (nécessite un serveur Incus réel)
# Variables : PLATI_URL, ADMIN_PASSWORD, REPO_TO_CLONE, SSH_KEY_PATH, APP_PORT
test-integration:
	python3 tests/integration_system.py

# Database
migrate:
	cd backend && go run ./cmd/plati-server -config ../config/plati.yaml -migrate

seed:
	./scripts/seed-db.sh

# Screenshots — capture UI screenshots of every page for design review
# Requires: make dev running in another terminal
# Required env: ADMIN_PASSWORD=<your admin password>
# Optional env: INSTANCE_ID=<id of a running instance> (for terminal screenshots)
screenshots:
	cd frontend && ADMIN_PASSWORD=$(ADMIN_PASSWORD) npm run screenshots

# Clean
clean:
	rm -f backend/plati-server
	rm -rf frontend/build frontend/.svelte-kit
