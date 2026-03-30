# CLAUDE.md

## Project

Plati — internal platform to manage Incus-based dev environments. Go backend + SvelteKit frontend.

## Dev Setup

```bash
./scripts/setup-dev.sh    # First time: copies config, generates JWT secret + AES key
make migrate              # Apply DB migrations
make dev                  # or: mprocs
```

Backend runs on `:8080`, frontend dev server on `:5173` (proxies `/api`, `/auth`, `/health` to backend).

## Architecture

```
handlers → services → queries → database (SQLite)
                ↓
           incus client (TLS)
```

- **handlers** (`internal/handlers/`): HTTP only, no business logic
- **services** (`internal/services/`): orchestrates DB + Incus operations
- **queries** (`internal/database/queries/`): pure SQL, one file per entity
- **models** (`internal/models/`): shared structs with `db`/`json` tags

## Key Files

| File | Purpose |
|------|---------|
| `backend/cmd/plati-server/main.go` | App bootstrap (config → DB → Incus → services → router → listen) |
| `backend/internal/router/router.go` | All route definitions |
| `config/plati.yaml` | Runtime config (gitignored) |
| `config/plati.yaml.example` | Config template |
| `config/templates/*.json` | Instance template definitions |
| `frontend/src/routes/+layout.svelte` | Root layout, auth init |
| `frontend/src/lib/api/client.ts` | Typed HTTP client |
| `frontend/src/lib/stores/auth.ts` | Global auth state |

## Database

SQLite WAL mode, single writer (`MaxOpenConns=1`). Migrations in `backend/internal/database/migrations/`, embedded via `go:embed`, applied at startup.

Tables: `users`, `ssh_keys`, `secrets`, `servers`, `templates`, `volumes`, `instances`, `settings`.

## Auth

Two methods unified behind JWT middleware:
- **Admin password**: bcrypt hash in config → `POST /auth/login` → JWT in httpOnly cookie
- **Entra OIDC**: optional, OIDC code flow → `GET /auth/entra` → `GET /auth/callback`

JWT stored in `plati_token` httpOnly cookie. `AuthMiddleware` validates it. `AdminMiddleware` checks role.

## Incus Integration

`internal/incus/`:
- `client.go` — wraps Incus SDK, implements `IncusClient` interface
- `pool.go` — thread-safe map of named clients
- `instances.go` — build config from templates (cloud-init, SSH keys, resource limits)
- `volumes.go` — create/attach/detach/delete workspace volumes

## Frontend Patterns

- Svelte 5 runes: `$state`, `$effect`, `$props`, `$bindable`
- API calls wrapped in `if (browser) { ... }` to avoid SSR hydration issues
- All API calls use `credentials: 'include'` for cookie auth
- Route group `(app)/` is protected — redirects to `/login` if not authenticated

## Adding a New API Endpoint

1. Add model fields in `internal/models/models.go` if needed
2. Add DB query in `internal/database/queries/<entity>.go`
3. Add service method in `internal/services/<entity>_service.go`
4. Add handler in `internal/handlers/<entity>.go`
5. Register route in `internal/router/router.go`
6. Add typed API function in `frontend/src/lib/api/index.ts`

## Config Fields Reference

```yaml
server:
  host: 0.0.0.0
  port: 8080
  frontend_url: http://localhost:5173   # CORS origin

auth:
  jwt_secret: <openssl rand -hex 32>
  jwt_lifetime_hours: 24
  admin_password_hash: <bcrypt hash>
  entra_client_id: ""                   # Optional
  entra_client_secret: ""
  entra_tenant_id: ""

database:
  path: plati.db

secret_encryption_key: <openssl rand -hex 32>   # AES-256-GCM
sleep_timeout: 4h
templates_dir: templates                          # Relative to config dir

servers:
  - name: local
    endpoint: https://127.0.0.1:8443
    tls_client_cert: ""
    tls_client_key: ""
    max_instances: 50
```

## Make Targets

```bash
make dev            # backend + frontend in parallel
make build          # compile both
make migrate        # apply DB migrations
make seed           # bcrypt hash helper
make test           # go test + npm test
make clean          # remove artifacts
```

## Tests

```bash
# API integration
./scripts/test-api-lifecycle.sh http://localhost:8080 <password>
./scripts/test-api-admin.sh http://localhost:8080 <password>

# E2E (requires Chrome + selenium)
python3 tests/selenium_tests.py

# Go unit tests
cd backend && go test ./...
```
