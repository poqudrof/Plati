# CLAUDE.md

## Project

Plati — internal platform to manage Incus-based dev environments. Go backend + SvelteKit frontend.

## Dev Setup

```bash
cp .env.plati.example .env.plati                 # First time: infra values for config bootstrap
cp .env.tailscale.example .env.tailscale         # First time: TS_AUTHKEY, TS_HOSTNAME, APP_PORT
docker compose --env-file .env.tailscale -f docker-compose.dev.yml up -d
```

The dev stack is published on the tailnet, exactly like prod — so you can work on the UI with
HMR and still reach it remotely. `--env-file .env.tailscale` is **required**: the compose file
declares `TS_AUTHKEY` mandatory, and `backend`/`frontend` have no network of their own (they
join the `tailscale` container's netns), so a failed `tailscale up` leaves them with no
network at all — that is what a `npm install` dying on `ENETUNREACH` means.

Open `https://<TS_HOSTNAME>.<tailnet>.ts.net` (or `http://127.0.0.1:5300`) and complete the
`/setup` wizard, then **restart the stack** (see "Config Bootstrap" below for why).

Ports, both bound to the loopback — nothing on the LAN: frontend `127.0.0.1:5300`, backend
`127.0.0.1:8090` (for debug and test scripts). Both are published **on the `tailscale`
service**, since it owns the shared netns; declaring them on `backend`/`frontend` is an error.
Inside that netns the frontend reaches the backend as `127.0.0.1:8080`.

**HMR only works through the tailnet URL.** The Vite client builds its WebSocket URL as
`hostname:${hmrPort || page port}`; behind Tailscale Serve the page is https with no explicit
port, which would yield the invalid `wss://<host>:`. So `VITE_HMR_CLIENT_PORT=443` is set in
the compose file and read by `vite.config.ts`. The trade-off: `127.0.0.1:5300` still serves the
app but gets no hot reload. Set `VITE_HMR_CLIENT_PORT=5300` to flip it.

Incus runs on the host and is reached at **`172.31.240.1:8444`** — the gateway of the pinned
`plati` subnet, not `host.docker.internal`. That is not a style choice: Docker rejects
`extra_hosts` together with `network_mode: service:*` (*"conflicting options: custom host-to-IP
mapping and the network mode"*), so `host.docker.internal` cannot be mapped and would not
resolve. Both compose files pin the same subnet precisely so this IP is stable and can live in
the shared `plati.yaml`.

Beware: this failure is **silent**. `NewClient` passes `SkipGetServer: true`, so `ConnectIncus`
never touches the network and the startup log prints `connected to Incus server local` even
when the endpoint is unreachable. Check it for real instead:

```bash
docker exec plati-backend curl -sk -o /dev/null -w '%{http_code}\n' https://172.31.240.1:8444/1.0
```

`./scripts/setup-dev.sh` + `make dev` / `mprocs` remain the host-native path (Go and Node
installed locally, frontend on `:5173`). The Docker stack above is self-contained.

After Go changes, restart the backend to apply them:

```bash
docker compose --env-file .env.tailscale -f docker-compose.dev.yml restart backend
```

## Dev vs Prod

`docker-compose.dev.yml` and `docker-compose.prod.yml` share the same base: same images, same
bind-mounted sources, same volumes, same `config-init` bootstrap, same Incus access through
`host.docker.internal`. **They use the same `config/plati.yaml` and the same `.env.plati`** —
only `PLATI_FRONTEND_URL` differs in practice.

**They also share the same database.** Both files declare the `plati-data` volume and both
resolve to the same Docker volume (`plati_plati-data`), because the Compose project name comes
from the directory, not from the compose file name. `database.path` is
`/workspace/data/plati.db` in the single shared `plati.yaml`. So dev is for working on the UI
and on migrations; prod serves the very same data, built.

They also share the same **Tailscale node**: same `tailscale-state` volume, same hostname, so
the same URL — and switching between them needs no new auth key.

Corollary: **never run both stacks at once.** They share `container_name`s, so Compose refuses
to — which is fortunate, since SQLite here runs single-writer (`MaxOpenConns=1`) and two
processes on one file would corrupt it. Switch explicitly:

```bash
docker compose --env-file .env.tailscale -f docker-compose.prod.yml down
docker compose --env-file .env.tailscale -f docker-compose.dev.yml up -d
```

The **only** difference left is how the frontend is served:

| | Dev | Prod |
|---|---|---|
| Frontend | `vite dev`, HMR | `vite build` (adapter-node) served by `frontend/server.js` |
| Backend proxy | `server.proxy` in `vite.config.ts` | `frontend/server.js` (dev-only config, hence the duplicate) |
| Backend port | published on `127.0.0.1:8090` | not published |

Everything else — images, bind mounts, volumes, `config-init`, DB, `plati.yaml`, Tailscale
node, Serve config, pinned subnet, Incus access, `network_mode: service:tailscale` — is
identical.

The prod stack publishes the frontend on the tailnet via a `tailscale` container running
`tailscaled` in userspace mode with Tailscale Serve (80/443). Because netstack only routes
inbound traffic to `127.0.0.1`, `backend` and `frontend` join that container's network
namespace with `network_mode: "service:tailscale"` — which is why no socat sidecar is needed
(unlike the host-app recipe in `tailscale-dev.md`). See the header of
`docker-compose.prod.yml`.

### The prod frontend proxy

`vite.config.ts`'s `server.proxy` only exists in dev, so the built frontend needs its own way
to reach the backend. `frontend/server.js` wraps adapter-node's `handler` and forwards `/api`,
`/auth` and `/health` to `BACKEND_HOST:BACKEND_PORT`.

It has to be a real HTTP server rather than a SvelteKit `handle` hook: `/api` carries
WebSockets (terminal, creation-stream, debug-logs) and a hook has no access to the HTTP
upgrade. Keep its `PROXIED` prefix list in sync with `vite.config.ts`.

Two gotchas when editing the prod frontend service:

- `npm install --include=dev` is required — `vite`, `@sveltejs/kit` and the adapter are
  devDependencies, so `NODE_ENV=production` at install time would skip them and break the
  build. `NODE_ENV` is only set on the final `node server.js`.
- `${APP_PORT}` appears twice: the `PORT` env var here, and hardcoded in
  `deploy/tailscale/serve.json` (Tailscale Serve does not interpolate env vars).

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

Three methods unified behind JWT middleware:
- **Admin password**: bcrypt hash in config → `POST /auth/login` with no email → JWT in httpOnly cookie
- **User password**: bcrypt hash in `users.password_hash` → `POST /auth/login` with `{email, password}`
- **Entra OIDC**: optional, OIDC code flow → `GET /auth/entra` → `GET /auth/callback`

JWT stored in `plati_token` httpOnly cookie. `AuthMiddleware` validates it. `AdminMiddleware` checks role.

Admins set a user's password at creation (`POST /api/v1/admin/users`) and can replace it later
by passing a `password` field to `PUT /api/v1/admin/users/{id}` — an empty or absent field
leaves the current one untouched, so the same form saves name, role and password together. The
minimum is `services.MinPasswordLength` (8), the same floor the setup wizard enforces. There is
no "confirm the old password" step: the admin never needs to know it.

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

### User public keys (remote access)

Separate from the modes above: `ssh_keys` holds the user's *own* public keys, always written
to `authorized_keys` (root + terminal user) on create/rebuild so they can SSH in from their
machine. Admins manage them in **Admin → Users → Keys**; keys are validated with
`ssh.ParseAuthorizedKey` and stored normalized.

A running instance does not need a rebuild: the instance page's **SSH Access** tab installs a
key immediately via `POST /instances/{id}/authorized-keys` (idempotent shell append, see
`services/instance_authorized_keys.go`).

## Tailscale Key Modes

Controlled per-user via `user_preferences.tailscale_mode`:

- **`plati`** (default): The platform-level Tailscale auth key (set by admin at `PUT /api/v1/admin/settings/tailscale-key`) is injected as `TAILSCALE_AUTH_KEY`.
- **`personal`**: The user's own `TAILSCALE_AUTH_KEY` secret is used.

**The platform key must be reusable.** Every instance logs in with the same key, so a
single-use key works exactly once and every machine after the first — a duplicate above all —
silently fails to join the tailnet.

That failure used to be invisible: the mixin's login command ends in `|| true` so a rejected
key never fails a create, and `InstallTailscale` passed `logFn = nil`, discarding the output.
So when the machine is still logged out after an install, `InstallTailscale` now re-runs
`tailscale up` with the output captured and returns it as `login_output` — the instance page
renders it under "What Tailscale said". The key is read from `/etc/profile.d/plati-env.sh`
inside the instance, never placed on the command line, and `redactTailscaleKeys` scrubs any
`tskey-…` from the output before it reaches the UI or the log.

The machine's tailnet hostname is `sanitizeName(instance.name)`, recomputed at each install —
so a renamed instance (`incus_name` deliberately never follows a rename) comes up under its
current name, not the one it was created with.

## Persistence Modes

Configured per-template via `persistence.mode` in the template YAML:

- **`normal`** (default): One or more persistent volumes are created and attached at first `Create()`. On `Rebuild()`, volumes are detached/reattached. A sentinel file `{first_dir}/.plati-initialized` distinguishes first init from rebuild — `first_init_commands` run on first init, `rebuild_commands` on subsequent rebuilds.
- **`ephemeral`**: No volumes created. All storage is instance-local and wiped on rebuild/delete. `first_init_commands` always run (no sentinel).

### Template YAML persistence fields

```yaml
persistence:
  mode: normal          # "normal" | "ephemeral"
  directories:
    - path: /home/ubuntu
      size: 20GB
      pool: default     # optional, uses Incus default if omitted

first_init_commands:    # run only on first Create()
  - git clone ... /home/ubuntu/repo

rebuild_commands:       # run on Rebuild() when sentinel exists
  - cd /home/ubuntu/repo && git pull || true

# Legacy: post_create_commands treated as first_init_commands if first_init_commands absent
```

Every template persists the login user's home (`/home/ubuntu` in practice): that is where
repos, dotfiles and tool caches live, so a rebuild keeps them. A template without a
`persistence` block gets exactly that implicitly — one volume on `/home/{terminal_user}`
(or `/root` when the template declares no `terminal_user`), sized from `resources.disk`
(`defaultPersistenceDirs` in `services/instance_service.go`).

## Auto-Stop (Sleep)

Incus stops nothing on its own — there is no idle timeout in Incus. Every automatic stop
comes from Plati's sleep worker (`services/sleep_service.go`), which sweeps every
**5 minutes** (`sleepCheckInterval`) and stops the running instances whose deadline has
passed. The deadline is therefore accurate to within one sweep.

The policy is per instance, stored on the `instances` row (migration 018):

| Column | Meaning |
|---|---|
| `sleep_disabled` | 1 takes the instance out of the sweep entirely — it runs until stopped by hand |
| `sleep_timeout_minutes` | Per-instance timeout; **0 means "use the platform default"** (`sleep_timeout` in `plati.yaml`, 4h) |

`ListSleepableInstances` resolves both in SQL, so the worker stays a single query.

**The timer counts from the last start, not from actual use.** `last_active_at` is only
written by `Create`, `CreateAsync` and `Start` — SSH, the web terminal and OpenVSCode never
touch it. So an instance is stopped ~4h after it was started even while someone is working in
it. `POST /instances/{id}/sleep/reset` is the escape hatch: it re-stamps `last_active_at` and
buys another full timeout. Users who never want the interruption turn auto-stop off.

The **Status** tab on the instance page (`StatusTab.svelte`, first tab, before Storage) is the
UI for all of it: state summary, the on/off switch, the timeout picker, the countdown to the
next stop, and the reset button.

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
| `GET` | `browse?path=/home/ubuntu` | Directory listing (lazy load for SVAR file manager) |
| `GET` | `download?path=/home/ubuntu/file.txt` | Stream single file download |
| `GET` | `download-dir?path=/home/ubuntu/dir` | Stream directory as .tar |
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
| `PUT` | `/api/v1/instances/{id}` | Rename an instance (also updates its Tailscale hostname); `rename_container` renames the Incus container, `rename_system_hostname` the hostname inside Ubuntu |
| `GET` | `/api/v1/instances/{id}/authorized-keys` | Owner's public keys + whether each is installed in the instance |
| `POST` | `/api/v1/instances/{id}/authorized-keys` | Install one of them into the running instance (`{"key_id": N}`) |
| `GET` | `/api/v1/admin/users/{id}/public-keys` | List a user's own public keys (authorized_keys entries) |
| `POST` | `/api/v1/admin/users/{id}/public-keys` | Add a public key for a user (`{"name","public_key"}`) |
| `DELETE` | `/api/v1/admin/users/{id}/public-keys/{key_id}` | Remove one |
| `GET` | `/api/v1/instances/{id}/sleep` | Resolved auto-stop policy: override, platform default, and the deadline |
| `PUT` | `/api/v1/instances/{id}/sleep` | Set `{"disabled": bool}` and/or `{"timeout_minutes": N}` (0 = platform default); both optional, omitted fields keep their value |
| `POST` | `/api/v1/instances/{id}/sleep/reset` | Re-stamp `last_active_at` — buys another full timeout |
| `GET` | `/api/v1/preferences` | Get current user's preferences |
| `PUT` | `/api/v1/preferences` | Update SSH key mode / Tailscale mode |
| `GET` | `/api/v1/admin/settings/tailscale-key` | Check if platform key configured (`{"configured": bool}`) |
| `PUT` | `/api/v1/admin/settings/tailscale-key` | Set platform Tailscale auth key |
| `DELETE` | `/api/v1/admin/settings/tailscale-key` | Remove platform Tailscale auth key |
| `GET` | `/api/v1/admin/instances` | Every user's instances, with `user_email` / `user_name` / `template_name` resolved |
| `POST` | `/api/v1/admin/instances/{id}/duplicate` | Copy any instance and assign it to `{"user_id": N}` (omitted = source's owner) |

`instances.create` body accepts optional `ssh_key_mode` and `tailscale_mode` fields to override user preferences per-instance.

## Renaming an Instance

`PUT /api/v1/instances/{id}` changes the display name and, with it, the tailnet hostname
(`sanitizeName(name)`) — through `environment.PLATI_TAILSCALE_HOSTNAME` for the next rebuild
and a live `tailscale set --hostname` when the instance is running.

There are three names, and each is opt-in past the first — `services.RenameOptions` carries the
flags:

| Name | Flag | Cost |
|------|------|------|
| Display name + tailnet hostname | always | none |
| Hostname inside Ubuntu (`/etc/hostname`, `/etc/hosts`, `hostnamectl`) | `rename_system_hostname` | needs the instance running |
| Incus container (`incus_name`) | `rename_container` | stops and restarts the instance |

The UI checks `rename_system_hostname` by default (it costs nothing) and leaves
`rename_container` unchecked (it restarts). The API defaults both to false — the Go zero value —
so an existing caller sending only `name` keeps the old behaviour.

`systemHostnameScript` writes all three places for a reason: `/etc/hostname` is what survives a
restart, the `127.0.1.1` line in `/etc/hosts` is what stops sudo warning *"unable to resolve
host"*, and `hostnamectl` (falling back to plain `hostname`) applies it live without a reboot.
The response carries back `system_hostname`, read from `hostname` inside the instance, so the UI
reports what actually took effect rather than what was asked.

`incus_name` does **not** follow unless the request carries `rename_container: true`, because
Incus refuses to rename a running instance. With the flag, `renameContainer` stops the
instance, renames it to `plati-{user_id}-{hostname}`, and starts it again if it was running —
so the option is opt-in in the UI and confirmed before saving. Three things to know:

- The DB row is updated **after** the Incus operation returns. If that write fails the rename
  is rolled back in Incus: a stale `incus_name` would make every later call address a container
  that no longer exists.
- Storage volumes keep their original names. They are attached by device name, so this is
  cosmetic; renaming them would mean detach/rename/reattach and put the data at risk.
- Right after the restart the container is up but not yet accepting exec, so the live
  `tailscale set --hostname` step retries (5 × 2s) when a restart happened.

`RenameResult` embeds `*models.Instance` and therefore needs its own `MarshalJSON`, for the
same reason `AdminInstance` does — see the note under "Admin View of All Workspaces".

## Admin View of All Workspaces

The dashboard shows a **Mine / All users** tab bar to admins. "All users" is a table
(`GET /api/v1/admin/instances`) rather than the usual `InstanceCard` grid, because every
`/api/v1/instances/{id}/…` route is scoped to the caller via `GetInstanceByUser` — an admin
cannot start, stop, or open someone else's instance through them. Duplicate is the exception:
it has an admin route of its own.

`DuplicateInstanceButton.svelte` carries both paths. Given a non-empty `users` prop (admins
only) it shows an "assign to" picker and posts to the admin route; otherwise it posts to
`/api/v1/instances/{id}/duplicate`, which copies one of your own to yourself.

Two things to know about the model side:

- `models.AdminInstance` embeds `Instance`, which defines its own `MarshalJSON`. Embedding
  **promotes** that method, so `AdminInstance` needs the override in `models/json.go` — without
  it the owner and template columns are silently dropped from the response.
- `incus_name` is `plati-{user_id}-{name}` and `(incus_name, server_id)` is unique, so a second
  copy into the same account would collide. `freeInstanceName` picks `…-copy`, `…-copy-2`, …

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
