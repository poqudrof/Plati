# Plati

Internal platform for creating and managing persistent Incus-based development environments. Users get ready-to-code containers in one click, with persistent workspaces accessible via SSH.

## Architecture

```
Browser (SvelteKit :5173)
    │ HTTP/JSON + httpOnly JWT cookie
    ▼
Go Backend (chi :8080)
    │                    │
    ▼                    ▼
SQLite DB          Incus Server(s)
(plati.db)         (TLS mutual auth)
```

**Stack**: Go 1.23 · chi · sqlx · SQLite · SvelteKit 2 · Svelte 5 · Tailwind CSS 4 · Vite 6

## Prerequisites

- Go 1.23+
- Node.js 20+
- An Incus server (optional for UI development)

## Quick Start

```bash
# 1. Generate config with secrets
./scripts/setup-dev.sh

# 2. Apply database migrations
make migrate

# 3. Start backend + frontend
make dev
# or
mprocs
```

- Backend: http://localhost:8080
- Frontend: http://localhost:5173
- Default login: `admin` / password set during setup

## Configuration

All runtime config lives in `config/plati.yaml`. Copy the example and fill in your values:

```bash
cp config/plati.yaml.example config/plati.yaml
```

Key fields:

| Field | Description |
|-------|-------------|
| `server.port` | Backend HTTP port (default: 8080) |
| `server.frontend_url` | CORS origin for frontend |
| `auth.jwt_secret` | JWT signing key — `openssl rand -hex 32` |
| `auth.admin_password_hash` | bcrypt hash — `htpasswd -nbBC 10 "" pass \| tr -d ':\n'` |
| `secret_encryption_key` | AES-256 key — `openssl rand -hex 32` |
| `database.path` | SQLite file path |
| `servers[].endpoint` | Incus server URL (e.g. `https://host:8443`) |
| `servers[].tls_client_cert/key` | TLS mutual auth certs |
| `auth.entra_*` | Microsoft Entra ID (optional) |

## Makefile Targets

```bash
make dev            # Run backend + frontend in parallel
make build          # Compile production binaries
make migrate        # Apply database migrations
make seed           # Generate bcrypt hash helper
make test           # Run all tests
make clean          # Remove build artifacts
```

## Project Structure

```
backend/
├── cmd/plati-server/main.go    # Entry point
└── internal/
    ├── auth/                   # JWT, bcrypt, Entra OIDC
    ├── config/                 # YAML config loader
    ├── database/
    │   ├── migrations/         # Embedded SQL migrations
    │   └── queries/            # Data access layer (per-entity)
    ├── handlers/               # HTTP request handlers
    ├── incus/                  # Incus SDK wrapper + client pool
    ├── middleware/             # CORS, logging, recovery
    ├── models/                 # Shared structs
    ├── router/                 # chi route definitions
    └── services/               # Business logic + auto-sleep worker

frontend/
└── src/
    ├── routes/
    │   ├── login/              # Admin + Entra login
    │   ├── callback/           # OAuth callback
    │   └── (app)/              # Protected route group
    │       ├── dashboard/      # Instance list
    │       ├── instances/      # Create + detail
    │       ├── settings/       # SSH keys + secrets
    │       └── admin/          # Templates, servers, users
    └── lib/
        ├── api/                # Typed HTTP client
        ├── components/         # Reusable UI components
        └── stores/             # auth, notifications

config/
├── plati.yaml                  # Active config (gitignored)
├── plati.yaml.example          # Config template
└── templates/                  # Instance template definitions (JSON)
```

## Instance Lifecycle

```
Select template → Validate → Select server (first-fit)
  → Create storage volume (/workspace)
  → Create Incus instance (image + cloud-init)
  → Attach volume
  → Start instance → SSH connection info
```

**Rebuild** preserves workspace: stop → detach volume → delete → recreate → reattach → start.

**Auto-sleep**: background worker stops instances idle for more than `sleep_timeout` (default: 4h).

## API Overview

| Endpoint | Description |
|----------|-------------|
| `POST /auth/login` | Admin password login |
| `GET /auth/entra` | Microsoft Entra OAuth redirect |
| `GET /health` | Health check |
| `GET /api/v1/instances` | List user instances |
| `POST /api/v1/instances` | Create instance |
| `POST /api/v1/instances/{id}/start\|stop\|rebuild` | Lifecycle actions |
| `GET /api/v1/templates` | List available templates |
| `GET/POST /api/v1/ssh-keys` | SSH key management |
| `GET/POST /api/v1/secrets` | Encrypted secret management |
| `GET /api/v1/admin/*` | Admin endpoints (admin role required) |

## Testing

```bash
# API integration tests
./scripts/test-api-lifecycle.sh http://localhost:8080 yourpassword
./scripts/test-api-admin.sh http://localhost:8080 yourpassword

# E2E browser tests (requires Chrome + selenium)
python3 tests/selenium_tests.py

# Go unit tests
make test-backend
```

## Deployment

See [docker-plan.md](docker-plan.md) for the Docker deployment plan (nginx + backend + frontend containers).

## Docs

- [Architecture](doc/architecture.md)
- [Technologies](doc/technologies.md)
- [Future improvements](doc/future-improvements.md)
