# Testing

## Overview

| Suite | Type | Dependencies | Command |
|-------|------|--------------|---------|
| Go unit tests | Unit | None | `cd backend && go test ./internal/incus/...` |
| Go integration tests | Integration | None (mock Incus, in-memory DB) | `cd backend && go test ./internal/integration/...` |
| Image (e2e) tests | E2E | Live Incus server + internet access | `./tests.sh --image` |
| Playwright screenshots | Visual/E2E | Running dev stack | `./tests.sh --screenshots` |
| Shell API lifecycle | API | Running server + Incus | `./tests.sh --api-lifecycle` |
| Shell API admin | API | Running server | `./tests.sh --api-admin` |
| Python Selenium E2E | E2E | Running dev stack + Chrome | `python3 tests/selenium_tests.py` |

### Running all tests

```bash
# Unit + integration only (no server needed):
./tests.sh

# All suites against docker dev stack:
./tests.sh --all

# All suites with custom URL / password:
./tests.sh --all --url http://localhost:8080 --frontend-url http://localhost:5300 --password <pw>
```

### Docker dev stack ports

When using `docker compose -f docker-compose.dev.yml up`:

| Service | Port |
|---------|------|
| Backend | `8080` (host network) |
| Frontend dev server | `5300` |

`tests.sh` defaults match these ports (`--url http://localhost:8080`, `--frontend-url http://localhost:5300`).

---

## Go Unit Tests

**File:** `backend/internal/incus/instances_test.go`

Tests `buildInstanceConfig` in isolation (no Incus daemon required).

| Test | Description |
|------|-------------|
| `TestBuildInstanceConfig_Resources` | CPU and memory resource limits are set |
| `TestBuildSetupCommands_RootSSHAlways` | SSH authorized_keys pushed for root |
| `TestBuildSetupCommands_TerminalUser` | SSH setup for terminal_user with user wait |
| `TestBuildSetupCommands_Secrets` | Secrets written to env file |
| `TestBuildSetupCommands_PostCreateCmds` | Post-create commands executed |

```bash
cd backend && go test ./internal/incus/...
```

---

## Image (e2e) Tests

**File:** `backend/internal/e2e/image_test.go`

Provisions real Incus containers using the actual template definitions, waits for
readiness, then runs targeted assertions inside the container. Each test creates
a uniquely-named container and deletes it via `t.Cleanup` even on failure.

**Requirements:**
- Incus server accessible (default `https://127.0.0.1:8443`)
- Container internet access (packages come from the distro mirrors — apt for the Ubuntu templates, pacman for `arch`)
- TLS client cert/key at `config/plati-client.crt` / `config/plati-client.key` (or via env vars)

**Skip conditions:** Tests skip automatically when the cert files are missing or the Incus server is unreachable. A test whose template names an Incus profile the server does not have skips too (`provision` detects the "doesn't exist" error).

| Test | Template | Checks |
|------|----------|--------|
| `TestImage_SimpleWebServer` | `simple-webserver.yaml` | `webserver` service active; `/health` returns `{"status":"ok"}`; root path serves HTML |
| `TestImage_DockerInDocker` | `ubuntu.yaml` + `docker` mixin | `docker` service active; nginx:alpine serves HTTP on port 80 (host network) |
| `TestImage_Arch` | `arch.yaml` + all its mixins | Runs Plati's **real** setup pipeline (`incus.BuildSetupSteps` + `TemplateService.MixinSetupSteps`), not a hand-copied command list: mixin binaries pushed via `PushFile`, SSH keys and secrets written as production writes them, `security.nesting` taken from the mixin's own `incus_config`. Asserts: `arch` user and home ownership; `authorized_keys` for root and the terminal user; `plati-env.sh`; the first-init sentinel; `git` from pacman; `sshd` active with the Plati drop-in; Docker on the **fuse-overlayfs** driver running nginx; terminal user in the `docker` group; `sshx`, `openvscode-server` and `tailscaled` active |
| `TestImage_QcmPocFormation` | `qcm-poc-formation.yaml` | `git` available; repo clone at the template's dest; `rebuild_commands` (git pull) fetch a new commit; SSH key injection |
| `TestImage_SiteIAGen` | `site-ia-gen.yaml` | `git` available; repo clone; `rebuild_commands` fetch a new commit |

Note the `e2e` build tag: without `-tags e2e` the package compiles to nothing and `go test` reports `ok` while running none of it.

```bash
# Via top-level runner (also sets TAILSCALE_AUTH_KEY if already exported):
./tests.sh --image

# Directly with env vars:
TAILSCALE_AUTH_KEY=tskey-... \
IMAGE_TIMEOUT_SEC=600 \
  cd backend && go test -v -tags e2e -timeout 30m ./internal/e2e/...

# Single test:
cd backend && go test -v -tags e2e -run TestImage_Arch -timeout 30m ./internal/e2e/...
```

**Env vars:**

| Var | Default | Description |
|-----|---------|-------------|
| `INCUS_ENDPOINT` | `https://127.0.0.1:8443` | Incus API URL |
| `INCUS_TLS_CERT` | `config/plati-client.crt` | Path to TLS client cert |
| `INCUS_TLS_KEY` | `config/plati-client.key` | Path to TLS client key |
| `TAILSCALE_AUTH_KEY` | _(unset)_ | Auth key for tailscale join check |
| `IMAGE_TIMEOUT_SEC` | `300` | Seconds to wait for container readiness |

---

## Go Integration Tests

**File:** `backend/internal/integration/workflow_test.go`

Full HTTP API tests using a mock `IncusClient` and in-memory SQLite. No real Incus daemon or database file needed.

| Test | Description |
|------|-------------|
| `TestHealthCheck` | `GET /health` returns ok |
| `TestAdminLogin` | Admin password auth + `/auth/me` |
| `TestSSHKeyCRUD` | Add, list, delete SSH keys |
| `TestTemplateImportAndList` | Import template via API, list templates |
| `TestInstanceLifecycleWithMockIncus` | Create → verify SSH key push → start → stop → delete |
| `TestRebuildKeepsWorkspace` | Rebuild preserves workspace volume |
| `TestAdminCanCreateInstanceForUser` | Admin creates instance on behalf of user |
| `TestSecretInjectionIntoInstance` | Secrets injected as env vars and persist through rebuild |
| `TestSshxURLEndpoint` | SSHX collaborative terminal URL endpoint |

```bash
cd backend && go test ./internal/integration/...
```

---

## Playwright Screenshot Tests

**File:** `frontend/tests/screenshot.test.ts`

Visual regression / documentation screenshots. Requires the full dev stack running (backend + frontend).

| Page | Cases |
|------|-------|
| Login | default state, error state (wrong password) |
| Dashboard | instances list |
| New instance | blank form, template selected |
| Instance detail | stopped, running |
| Terminal | connected with command output |
| Settings | SSH keys + secrets |
| Admin | templates tab, servers tab, users tab, edit template modal |
| Docs | `/docs`, `/docs/instances`, `/docs/templates`, `/docs/settings`, `/docs/admin` |
| SSHX | sshx instance url display |
| Notifications | success toast |

```bash
# Start dev stack first: docker compose -f docker-compose.dev.yml up
./tests.sh --screenshots
# or directly:
cd frontend && BASE_URL=http://localhost:5300 ADMIN_PASSWORD=<pw> npm run screenshots
```

**Note:** `BASE_URL` must point to the running frontend (`http://localhost:5300` with docker).
`ADMIN_PASSWORD` is required for the auth setup step.

---

## Shell API Tests

### Lifecycle Test

**File:** `scripts/test-api-lifecycle.sh`

End-to-end instance lifecycle against a live server.

```
1.  Health check
2.  Admin login
3.  Get current user
4.  List templates
5.  Seed TAILSCALE_AUTH_KEY secret
6.  List instances (before create)
7.  Create instance
8.  Get instance detail
9.  Verify in Incus (if incus CLI available)
10. Stop instance
11. Start instance
12. Rebuild instance
13. Verify instance after rebuild
14. Delete instance
15. Verify cleanup
16. Cleanup TAILSCALE_AUTH_KEY secret
17. Logout
```

```bash
./tests.sh --api-lifecycle
# or directly:
./scripts/test-api-lifecycle.sh http://localhost:8080 <password>
```

### Admin Test

**File:** `scripts/test-api-admin.sh`

Admin API operations. Automatically cleans up stale test data from previous runs before starting.

```
0.  Pre-cleanup (remove leftover test-go-dev template if any)
1.  Template import
2.  List templates
3.  Template export
4.  Template update
5.  Template delete
6.  Verify deletion
7.  List servers
8.  List users
9.  SSH key CRUD (create, list, delete)
10. Secret CRUD (create, list, update, delete)
11. Unauthorized access check (expect 401)
```

```bash
./tests.sh --api-admin
# or directly:
./scripts/test-api-admin.sh http://localhost:8080 <password>
```

---

## Python Selenium E2E Tests

**File:** `tests/selenium_tests.py`

Browser-level E2E tests using Selenium + Chrome. Requires running dev stack and Chrome installed.

| Test | Description |
|------|-------------|
| `test_01_login_page_renders` | Login page UI elements present |
| `test_02_admin_login` | Admin password authentication |
| `test_03_dashboard_renders` | Dashboard loads with "My Workspaces" heading |
| `test_04_navigation_links` | Navbar navigation works |
| `test_05_new_instance_page` | Template selector and creation form |
| `test_06_settings_page` | SSH key manager and secrets sections |
| `test_07_admin_page` | Admin tabs visible |
| `test_08_logout` | Logout clears session |
| `test_09_protected_routes_redirect` | Unauthenticated access redirects to login |
| `test_10_health_endpoint` | `GET /health` returns ok |
| `test_11_ssh_key_crud` | Add/remove SSH keys via UI |
| `test_12_secret_crud` | Add/remove secrets via UI |
| `test_13_instance_lifecycle` | Create → terminal → command → delete |
| `test_14_login_failure` | Wrong password shows error |

```bash
python3 tests/selenium_tests.py
```
