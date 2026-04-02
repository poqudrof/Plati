# Dev Log

**Last updated:** 2026-03-31

Snapshot of what is done & tested, what needs polish, and what is missing.

---

## ✓ Done & Tested

### Auth & Users

- Admin password login + Entra OIDC (JWT httpOnly cookie) — integration + shell tests
- Per-user password login (`password_hash` in DB)
- User CRUD (admin), role assignment (`admin`/`user`) — shell API tests
- Setup wizard (one-time: admin password → Entra → server → write config) — works in browser

### SSH Keys & Secrets

- User-provided public SSH keys (CRUD) — integration + shell tests
- Plati-generated Ed25519 keypairs (private key shown once) — integration tests
- Admin-managed shared keypairs (assign to users many-to-many)
- User secrets: AES-256-GCM encrypted, injected as `/etc/profile.d/plati-env.sh` — integration tests
- Instance-specific secret overrides — integration tests
- Secrets persist through rebuild — `TestSecretInjectionIntoInstance`

### Instance Lifecycle

- Create: cloud-init (phase 1) + exec setup (phase 2: SSH keys, secrets, post_create_commands)
- Start, stop, delete — integration + shell API lifecycle tests
- Rebuild: detach volumes → delete → recreate → reattach → restart — `TestRebuildKeepsWorkspace`
- Workspace volumes survive rebuild (persistent attach/detach)
- Auto-sleep: idle instances stopped after configurable timeout (sleep worker, 5-min check)

### Terminal

- WebSocket terminal via xterm.js (root + optional `terminal_user`)
- Cloud-init log streaming (WebSocket, journalctl → log file fallback) — admin debug panel

### Templates

- CRUD with YAML import/export
- Mixin system: `includes:` field resolved at import time, commands prepended
- Templates: `node-dev`, `python-dev`, `alpine-dev`, `alpine-minimal`, `docker-dev`, `site-ca`, `tailscale`, `sshx`, `transcription-catie`
- Mixins: `tailscale`, `sshx`
- E2E image tests (real cloud-init provisioned): node-dev ✓, python-dev ✓, docker-dev ✓, site-ca ✓, tailscale ✓, sshx ✓

### Admin & Config

- Server list with instance capacity bars
- Image browser (lists available Incus images per server)
- Platform Tailscale key (encrypted storage, never returned in plaintext)
- User preferences: `ssh_key_mode` (`plati`/`personal`), `tailscale_mode` (`plati`/`personal`)
- SSHX collaborative URL endpoint (`GET /api/v1/instances/{id}/sshx-url`)
- Duplicate instance (fresh clone from same template)

### Tests Passing (no server needed)

- **Go unit:** 11 tests — cloud-init building, SSH key injection, resource limits, setup commands
- **Go integration:** 9 scenarios — health, login, SSH keys, template import, full instance lifecycle, rebuild, admin creates for user, secrets, sshx URL

---

## ⚠ Nearly OK

1. **Instance creation is synchronous** — HTTP request blocks until phase 2 setup completes. Slow cloud-init or `apt-get` commands can hang the browser for minutes. No progress indicator.

2. **Phase 2 setup failures are silent** — If SSH key injection or `post_create_commands` fail, the instance is still marked `running`. User won't know setup failed without checking cloud-init logs manually.

3. **Settings page missing preference toggles** — `GET/PUT /api/v1/preferences` (`ssh_key_mode`, `tailscale_mode`) is fully implemented in the backend, but `/settings` has no UI for it. Users can't change their preference mode.

4. **Terminal resize not wired up** — xterm.js sends resize events via `FitAddon`, but the hardcoded `80x24` in `ExecInstance()` means initial size is wrong; resize may or may not be applied depending on timing.

5. **No image validation before create** — If a template's image doesn't exist on the target server, the error only surfaces after Incus rejects the create request. No pre-flight check.

6. **TLS `InsecureSkipVerify: true`** — The Incus client skips certificate verification. Fine for local dev, a risk for remote servers with self-signed certs you don't control.

7. **Duplicate instance doesn't clone secrets** — Instance-specific secrets are not copied to the duplicate. Likely intentional but not documented.

8. **Admin panel delete confirmations** — Uses `window.confirm()` (browser native). No loading state after confirmation; fast double-click could double-trigger.

9. **WebSocket error handling minimal** — Terminal and debug-log WebSockets show an error on disconnect but don't retry. Server restart mid-session kills the terminal.

10. **`sshx.yaml` persistence mode** — Template has `persistence: mode: ephemeral` but the E2E image test still checks for active services. Verify ephemeral containers can run systemd services reliably.

11. **Untested templates** — `alpine-dev`, `alpine-minimal`, `transcription-catie` have no E2E image tests. Provisioning is unverified against a real Incus server.

12. **No pagination** — All list endpoints (users, instances, templates, images) return full result sets. Will degrade on large datasets.

---

## ✗ Missing / Not Built

### Core UX Gaps

- Preferences UI (`ssh_key_mode` / `tailscale_mode` toggles in `/settings`)
- Instance filtering / sorting on dashboard (by status, template, date)
- Search (templates, users, instances)
- User profile / account page (edit own name, email, password)
- Password reset — admin must delete and recreate user; no self-service mechanism

### Instance Management

- Async instance creation with status polling — long creates block the HTTP response
- Per-user instance quota (`max_instances` is per-server, not per-user)
- Instance labels / tags / descriptions (only name + template today)
- Scheduled start/stop (cron-like) — sleep worker is idle-detection only
- Instance snapshots / backup
- Instance activity log (who did what when)

### Operations & Observability

- Audit log — no record of admin actions, instance deletions, key access
- Structured logging — uses `log.Printf`; no slog/zap, no log levels
- Resource usage graphs on instance detail page
- Metrics endpoint — nothing beyond basic DB ping in `/health`
- Rate limiting on API endpoints

### Multi-Server

- Server selection is round-robin by capacity; no affinity or manual server override
- No server health monitoring beyond one-shot ping at startup
- Multi-server client pool exists but is untested at scale

### Testing Gaps

- Negative tests (invalid inputs, permission denials, duplicate slugs, missing template IDs)
- Mixin composition integration test
- Concurrent instance creation test
- Alpine template E2E image tests
- Terminal WebSocket coverage in integration harness
- Setup wizard API coverage (currently only manual/browser tested)
- Secret update/delete end-to-end verification in shell lifecycle test
