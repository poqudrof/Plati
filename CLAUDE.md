# CLAUDE.md

## Project

Plati — internal platform to manage Incus-based dev environments. Go backend + SvelteKit frontend.

## Dev Setup

```bash
cp .env.plati.example .env.plati                 # First time: infra values for config bootstrap
docker compose -f docker-compose.dev.yml up -d   # config-init → backend → frontend
```

Open `http://127.0.0.1:5300` and complete the `/setup` wizard, then **restart the stack**
(see "Config Bootstrap" below for why).

Ports: frontend `127.0.0.1:5300`, backend `127.0.0.1:8090` (published for debug and test
scripts; the frontend reaches it over the Docker network as `backend:8080`). Both are bound
to the loopback — nothing is exposed on the LAN.

Containers run on a Docker bridge, **not** `network_mode: host`: the backend's `:8080` stays
internal, so it does not collide with whatever already listens on the host. Incus runs on the
host and is reached via `host.docker.internal:8443`, mapped to the Docker gateway by
`extra_hosts`.

`./scripts/setup-dev.sh` + `make dev` / `mprocs` remain the host-native path (Go and Node
installed locally, frontend on `:5173`). The Docker stack above is self-contained.

After Go changes, restart the backend to apply them:

```bash
docker compose -f docker-compose.dev.yml restart backend
```

## Dev vs Prod

`docker-compose.dev.yml` and `docker-compose.prod.yml` share the same base: same images, same
bind-mounted sources, same volumes, same `config-init` bootstrap, same Incus access through
`host.docker.internal`. **They use the same `config/plati.yaml` and the same `.env.plati`** —
only `PLATI_FRONTEND_URL` differs in practice.

Prod adds, and is otherwise identical:

| | Dev | Prod |
|---|---|---|
| Frontend | `vite dev`, HMR | build ahead of time, served as static assets |
| Exposure | `127.0.0.1:5300` only | Tailscale node, HTTPS on the tailnet |
| Backend port | published on `127.0.0.1:8090` | not published |
| Networking | Docker bridge | containers share tailscaled's netns |

The prod stack publishes the frontend on the tailnet via a `tailscale` container running
`tailscaled` in userspace mode with Tailscale Serve (80/443). Because netstack only routes
inbound traffic to `127.0.0.1`, `backend` and `frontend` join that container's network
namespace with `network_mode: "service:tailscale"` — which is why no socat sidecar is needed
(unlike the host-app recipe in `tailscale-dev.md`). See the header of
`docker-compose.prod.yml`.

## Config Bootstrap

`config/plati.yaml` is gitignored and generated at launch by the `config-init` service
(`backend/cmd/plati-config`), **only if absent**. It writes infrastructure values read from
`.env.plati` and leaves `admin_password_hash`, `jwt_secret` and `secret_encryption_key`
**empty** — the existing `/setup` wizard fills those in on first visit.

An existing file is never rewritten. That is what protects `secret_encryption_key`: losing it
makes every stored user secret undecryptable.

Two gotchas in `handlers/setup.go`:

- **Restart the stack after completing the wizard.** `Complete()` writes `jwt_secret` and
  `secret_encryption_key` to disk but only refreshes `AdminPasswordHash` in memory. Until a
  restart, `NewUserService` is still running with a **zero encryption key** (its fallback when
  the configured key is empty), so any secret saved in between is encrypted with that key.
- **The wizard's Incus step overwrites `servers[]`.** If left blank, the `else` branch replaces
  the list with `[]config.IncusServer{}` and the generated endpoint is lost.

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
| `config/templates/*.yaml` | Instance template definitions |
| `frontend/src/routes/+layout.svelte` | Root layout, auth init |
| `frontend/src/lib/api/client.ts` | Typed HTTP client |
| `frontend/src/lib/stores/auth.ts` | Global auth state |

## Database

SQLite WAL mode, single writer (`MaxOpenConns=1`). Migrations in `backend/internal/database/migrations/`, embedded via `go:embed`, applied at startup.

Tables: `users`, `ssh_keys`, `secrets`, `servers`, `templates`, `volumes`, `instances`, `settings`, `user_preferences`, `instance_volumes`.

## Auth

Two methods unified behind JWT middleware:
- **Admin password**: bcrypt hash in config → `POST /auth/login` → JWT in httpOnly cookie
- **Entra OIDC**: optional, OIDC code flow → `GET /auth/entra` → `GET /auth/callback`

JWT stored in `plati_token` httpOnly cookie. `AuthMiddleware` validates it. `AdminMiddleware` checks role.

## Incus Integration

`internal/incus/`:
- `client.go` — wraps Incus SDK, implements `IncusClient` interface
- `pool.go` — thread-safe map of named clients
- `instances.go` — build config from templates (resource limits); `SetupConfig` drives SSH key injection, first-init vs rebuild command split
- `volumes.go` — create/attach/detach/delete workspace volumes (parameterized by device name)

## SSH Key Modes

Controlled per-user via `user_preferences.ssh_key_mode` (overridable per-instance at create time):

- **`plati`** (default): The admin-managed key assigned to the user is injected into the instance. Git/SSH ops work automatically (e.g. access to org repos).
- **`personal`**: The user's own personal keypair (from `user_ssh_keys`) is injected. For users who manage their own GitHub keys.

## Tailscale Key Modes

Controlled per-user via `user_preferences.tailscale_mode`:

- **`plati`** (default): The platform-level Tailscale auth key (set by admin at `PUT /api/v1/admin/settings/tailscale-key`) is injected as `TAILSCALE_AUTH_KEY`.
- **`personal`**: The user's own `TAILSCALE_AUTH_KEY` secret is used.

## Workspace Persistence Modes

Configured per-template via `persistence.mode` in the template YAML:

- **`normal`** (default): One or more persistent volumes are created and attached at first `Create()`. On `Rebuild()`, volumes are detached/reattached. A sentinel file `{first_dir}/.plati-initialized` distinguishes first init from rebuild — `first_init_commands` run on first init, `rebuild_commands` on subsequent rebuilds.
- **`ephemeral`**: No volumes created. All storage is instance-local and wiped on rebuild/delete. `first_init_commands` always run (no sentinel).

### Template YAML persistence fields

```yaml
persistence:
  mode: normal          # "normal" | "ephemeral"
  directories:
    - path: /workspace
      size: 20GB
      pool: default     # optional, uses Incus default if omitted

first_init_commands:    # run only on first Create()
  - git clone ... /workspace/repo

rebuild_commands:       # run on Rebuild() when sentinel exists
  - cd /workspace && git pull || true

# Legacy: post_create_commands treated as first_init_commands if first_init_commands absent
```

Backward compat: templates without a `persistence` block get a single `/workspace` volume sized from `resources.disk`.

## Storage Management

Users can browse, download, and snapshot their persistent volumes from the **Storage** tab on the instance detail page.

### Backend

`internal/incus/client.go` exposes 7 storage methods on `IncusClient`:
- `ListDirectory` — `find -printf` inside the instance (returns SVAR-compatible `FileEntry`)
- `GetFile` / `StreamDirectory` — download single file (SDK `GetInstanceFile`) or directory (tar stream)
- `ListVolumeSnapshots` / `CreateVolumeSnapshot` / `DeleteVolumeSnapshot` / `RestoreVolumeSnapshot` — Incus SDK volume snapshot operations

`internal/services/storage_service.go` — dedicated service (not in `InstanceService`). Every method validates ownership via `GetInstanceByUser` and checks the requested path is within a mounted volume (traversal protection). Snapshot restore requires instance stopped.

`internal/handlers/storage.go` — HTTP handlers. Download endpoints stream binary with `Content-Disposition: attachment`.

### API Endpoints

All under `/api/v1/instances/{id}/storage/`, JWT-authed:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `browse?path=/workspace` | Directory listing (lazy load for SVAR file manager) |
| `GET` | `download?path=/workspace/file.txt` | Stream single file download |
| `GET` | `download-dir?path=/workspace/dir` | Stream directory as .tar |
| `GET` | `volumes/{vol_id}/snapshots` | List volume snapshots |
| `POST` | `volumes/{vol_id}/snapshots` | Create snapshot `{"name":"..."}` |
| `DELETE` | `volumes/{vol_id}/snapshots/{name}` | Delete snapshot |
| `POST` | `volumes/{vol_id}/snapshots/{name}/restore` | Restore snapshot (instance must be stopped) |

Volume listing uses the existing `GET /api/v1/instances/{id}/volumes` endpoint (returns `InstanceStorageInfo` with `VolumeDetail[]` including `volume_id`).

### Frontend

- **SVAR File Manager** (`@svar-ui/svelte-filemanager`) — third-party Svelte 5 component for the file browser
- `StorageTab.svelte` — component with two sections: file browser (SVAR with lazy `request-data` → `provide-data`) and per-volume snapshot management (create/list/restore/delete)
- Integrated as the default tab on the instance detail page
- Downloads use `window.open()` to the streaming endpoints (cookie auth works automatically)
- Snapshot restore is disabled when instance is running (button grayed out + service-level check)

## Frontend Patterns

- Svelte 5 runes: `$state`, `$effect`, `$props`, `$bindable`
- API calls wrapped in `if (browser) { ... }` to avoid SSR hydration issues
- All API calls use `credentials: 'include'` for cookie auth
- Route group `(app)/` is protected — redirects to `/login` if not authenticated

## New API Endpoints (added in platform improvements)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/preferences` | Get current user's preferences |
| `PUT` | `/api/v1/preferences` | Update SSH key mode / Tailscale mode |
| `GET` | `/api/v1/admin/settings/tailscale-key` | Check if platform key configured (`{"configured": bool}`) |
| `PUT` | `/api/v1/admin/settings/tailscale-key` | Set platform Tailscale auth key |
| `DELETE` | `/api/v1/admin/settings/tailscale-key` | Remove platform Tailscale auth key |

`instances.create` body accepts optional `ssh_key_mode` and `tailscale_mode` fields to override user preferences per-instance.

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
./tests.sh                # unit + integration (no server needed)
./tests.sh --all          # all suites (requires docker dev stack running)
./tests.sh --screenshots  # Playwright screenshots only
./tests.sh --api-admin    # shell admin API tests only
./tests.sh --image        # image e2e tests (requires Incus + internet)
```

Docker dev stack runs frontend on `:5300`, backend on `:8090`. `tests.sh` still defaults to
`http://localhost:8080` for the backend, so pass `--url http://localhost:8090` (its
`FRONTEND_URL` default of `:5300` is still correct).

See `testing.md` for the full suite reference.

### Writing tests

**Go unit test** (`backend/internal/incus/instances_test.go`): call `BuildInstanceConfig(...)` directly and assert on the returned `map[string]string`. No harness needed.

**Go integration test** (`backend/internal/integration/workflow_test.go`):
1. Call `newHarness(t)` — spins up an in-memory SQLite DB, mock Incus client, and `httptest.Server` with the real router.
2. Use `h.do(method, path, body)` for HTTP calls; assert on `resp.StatusCode`.
3. Inspect `h.mock.instances` / `h.mock.volumes` to verify Incus side-effects.
4. Call `h.teardown()` via `defer`.

New handler routes are automatically exercised once registered in `router.go` — no harness changes needed unless you add a new handler dependency to `router.Deps`.

**Shell API tests** (`scripts/test-api-admin.sh`, `scripts/test-api-lifecycle.sh`):
- Add a new numbered section using the `api METHOD /path` helper.
- If your test creates durable state (e.g. a named template), add a pre-cleanup block at the top so re-runs don't fail with `UNIQUE constraint` errors.
- Use `pass/fail/info` helpers for output; `fail` exits immediately.

**Playwright screenshots** (`frontend/tests/screenshot.test.ts`):
- Add a new `test(...)` inside an existing `test.describe` block, or create a new `test.describe`.
- Use `shot(page, 'NN-descriptive-name')` to save a screenshot.
- Skip gracefully when data doesn't exist: `if (!thing) { test.skip(); return; }`.

### Deleting tests

- **Go**: remove the `Test*` function; the file stays unless it becomes empty.
- **Shell**: delete the numbered section from the script. Remove the corresponding pre-cleanup block if you added one.
- **Playwright**: delete the `test(...)` block. Remove the `test.describe` wrapper if it becomes empty.
