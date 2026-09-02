---
name: plati-user-agent
description: Act on behalf of a Plati user — list, create, start, stop, rebuild, and delete their own instances (VMs), browse/download their own workspace storage, and manage their own preferences/SSH keys/secrets via the Plati HTTP API. Use when asked to manage "my" Plati instance(s) as a regular (non-admin) user. Requires a user-scoped Plati API key (PLATI_USER_API_KEY).
---

# Plati — User Agent

## What Plati is

Plati is an internal platform that provisions and manages **Incus-based dev environment VMs/containers** ("instances") from reusable **templates** (base image + resources + a persistent volume on the login user's home + setup commands). Backend on `:8080`; you talk to it purely over its HTTP API — there is no shell/SSH access to the platform host itself (though you can act *inside* your own instance via its own SSH/terminal, which is separate from this API).

Core objects relevant to you:
- **Instance** — your dev environment (`status`: `creating|running|stopped|error`).
- **Template** — what an instance is created from; you can only read templates, not create/edit them.
- **Volume** — your instance's persistent workspace storage; survives `rebuild`.

Everything below is scoped to **you** (the user who owns the API key) — the API enforces this server-side regardless of what ids you pass, so you cannot see or affect other users' instances with a user key.

## Authentication

- Base URL: `$PLATI_BASE_URL` (default `http://localhost:8080`)
- Key: `$PLATI_USER_API_KEY`
- Every request: header `Authorization: Bearer $PLATI_USER_API_KEY`

```bash
curl -s -H "Authorization: Bearer $PLATI_USER_API_KEY" "$PLATI_BASE_URL/api/v1/instances"
```

### Getting your key (admin-issued, self-regenerated)

You cannot create your own API key — an admin creates the first one for you (web UI: **Admin → Users → API Keys**, or they call `POST /api/v1/admin/users/{your_id}/api-keys`) and hands you the plaintext once. Store it as `PLATI_USER_API_KEY`.

If it leaks, or you just want a fresh one, you can regenerate it yourself at any time — same key id, new secret, old secret stops working immediately:

```bash
curl -H "Authorization: Bearer $PLATI_USER_API_KEY" -X POST \
  "$PLATI_BASE_URL/api/v1/api-keys/{id}/regenerate"
# → {"id":1,"name":"user-agent","key_prefix":"plati_ab12cd","key":"plati_<...>"}
# Update PLATI_USER_API_KEY with the new "key" value — it is shown only once.
```

List your own key(s) any time: `GET /api/v1/api-keys` (also visible under **Settings → API Keys** in the web UI, where you can regenerate with one click).

## Endpoints available to you

All under `$PLATI_BASE_URL/api/v1/...`. JSON bodies throughout.

### Instances (yours only)
| Method | Path | Notes |
|---|---|---|
| `GET` | `/instances` | List your instances |
| `POST` | `/instances` | Create: `{"name","template_id","ssh_key_mode"?,"tailscale_mode"?}` |
| `GET` | `/instances/{id}` | Details |
| `DELETE` | `/instances/{id}` | Destroy (irreversible) |
| `POST` | `/instances/{id}/start` \| `/stop` \| `/rebuild` \| `/duplicate` | Lifecycle actions |
| `GET` | `/instances/{id}/stats` | Resource usage |
| `GET` | `/instances/{id}/volumes` | Attached volumes |
| `GET`/`PUT` | `/instances/{id}/sleep` | Auto-stop policy: `{"disabled"?,"timeout_minutes"?}` (0 = platform default) |
| `POST` | `/instances/{id}/sleep/reset` | Buy another full timeout before the auto-stop |
| `GET` | `/instances/{id}/sshx-url` | Web terminal share link |
| `GET`/`POST`/`DELETE` | `/instances/{id}/tailscale-serve` | Tailscale Serve exposure |
| `GET` | `/instances/{id}/tailscale-status` | Tailscale status |
| `GET` | `/instances/{id}/terminal` | WebSocket terminal (not plain HTTP) |
| `GET` | `/instances/{id}/creation-stream` | SSE stream of creation progress |

### Instance secrets (env vars injected into one instance)
`GET/POST /instances/{id}/secrets`, `PUT/DELETE /instances/{id}/secrets/{secret_id}`.

### Storage (your instance's workspace)
| Method | Path | Notes |
|---|---|---|
| `GET` | `/instances/{id}/storage/browse?path=/home/ubuntu` | Directory listing |
| `GET` | `/instances/{id}/storage/download?path=...` | Download one file |
| `GET` | `/instances/{id}/storage/download-dir?path=...` | Download a directory as `.tar` |
| `GET`/`POST` | `/instances/{id}/storage/volumes/{vol_id}/snapshots` | List / create snapshot |
| `DELETE` | `/instances/{id}/storage/volumes/{vol_id}/snapshots/{name}` | Delete snapshot |
| `POST` | `/instances/{id}/storage/volumes/{vol_id}/snapshots/{name}/restore` | Restore (instance must be stopped) |

### Templates (read-only)
`GET /templates`, `GET /templates/{id}`, `GET /templates/{id}/export` — to pick a `template_id` before creating an instance.

### Your account
- `GET /disks` — your own volumes across all instances
- `GET/PUT /preferences` — `ssh_key_mode` (`plati`|`personal`), `tailscale_mode` (`plati`|`personal`)
- `GET/POST /ssh-keys`, `DELETE /ssh-keys/{id}` — your personal public SSH keys
- `GET /user-keys`, `POST /user-keys/generate`, `DELETE /user-keys/{id}` — Plati-generated keypairs
- `GET/POST /secrets`, `PUT/DELETE /secrets/{id}` — reusable secrets (available to inject into instances)

## Working conventions

- `GET /templates` and `GET /instances` first — you need a `template_id` to create, and an `instance` `id` for every lifecycle/storage call.
- **Deletion and rebuild are destructive** to instance-local state (`rebuild` re-runs setup but keeps the persistent volume; `delete` destroys the instance and, unless it's an `ephemeral`-mode template, detaches its volume). Confirm with the user before either unless explicitly pre-authorized.
- Snapshot `restore` requires the instance to be **stopped** first (`POST /instances/{id}/stop`).
- Non-2xx responses are JSON `{"error": "..."}`; a 404 on an instance/volume id usually means it belongs to someone else, not that it doesn't exist.
