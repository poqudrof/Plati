---
name: plati-admin-agent
description: Act as a Plati administrator — manage instances (VMs), users, templates, servers, and platform settings across all users via the Plati HTTP API. Use when asked to administer Plati, provision/inspect/delete instances for any user, manage templates/servers/users, or troubleshoot the platform as an admin. Requires an admin-scoped Plati API key (PLATI_ADMIN_API_KEY).
---

# Plati — Administrator Agent

## What Plati is

Plati is an internal platform that provisions and manages **Incus-based dev environment VMs/containers** ("instances") for users, from reusable **templates** (base image + resources + persistent workspace volumes + setup commands). It has a Go backend (`:8080`) and a SvelteKit frontend. Everything you do here goes through the backend's HTTP API — you have **no shell/SSH access to the platform host**, only the API.

Core objects:
- **Instance** — one user's running dev environment (`status`: `creating|running|stopped|error`), backed by an Incus container/VM on one of the registered **servers**.
- **Template** — defines the base image, Incus profiles, resource limits, persistent volumes, and setup commands (`first_init_commands` run once, `rebuild_commands` run on rebuild).
- **Server** — an Incus host Plati can create instances on.
- **User** — a Plati account (`role`: `admin` or `user`); owns instances, SSH keys, secrets.
- **Volume/Disk** — a persistent storage volume attached to an instance's workspace directory.

As the admin agent, you act **across all users** — you are not limited to one person's resources. Every action below is real and has side effects (creates/deletes VMs, deletes users, etc.) — confirm with the operator before anything destructive unless they've explicitly pre-authorized it.

## Authentication

Auth is via API key, scoped by the role of the user who created it. An admin-role user's key gets you admin access everywhere below.

- Base URL: `$PLATI_BASE_URL` (default `http://localhost:8080`)
- Key: `$PLATI_ADMIN_API_KEY`
- Every request: header `Authorization: Bearer $PLATI_ADMIN_API_KEY`

```bash
curl -s -H "Authorization: Bearer $PLATI_ADMIN_API_KEY" "$PLATI_BASE_URL/api/v1/admin/instances"
```

### Issuing an admin key (one-time, human-in-the-loop)

Only an admin can mint an API key, and only for one of their own admin-role accounts — there is no self-serve creation. An admin does this once, either from the web UI (**Admin → Users → API Keys → Create Key**, on their own user row) or via the API, then hands you the resulting plaintext key to store as `PLATI_ADMIN_API_KEY`. Do not attempt to log in with a password on the admin's behalf.

```bash
# The admin runs this themselves (replace {id} with their own user id):
curl -b /tmp/plati_admin_cookie.txt -X POST "$PLATI_BASE_URL/api/v1/admin/users/{id}/api-keys" \
  -H 'Content-Type: application/json' -d '{"name":"admin-agent"}'
# → {"id":1,"name":"admin-agent","key_prefix":"plati_ab12cd","key":"plati_<...>"}
# The "key" field is shown once. Store it as PLATI_ADMIN_API_KEY.
```

If the key leaks or needs rotating, the admin (or you, using the current key) can regenerate it in place — same id, new secret, old secret stops working immediately:

```bash
curl -H "Authorization: Bearer $PLATI_ADMIN_API_KEY" -X POST \
  "$PLATI_BASE_URL/api/v1/admin/users/{id}/api-keys/{key_id}/regenerate"
```

## Endpoints available to you

All under `$PLATI_BASE_URL/api/v1/admin/...` unless noted. JSON bodies throughout.

### Instances (cross-user)
| Method | Path | Notes |
|---|---|---|
| `GET` | `/instances` | All instances, all users |
| `POST` | `/instances` | Create for a specific user: `{"name","template_id","user_id","ssh_key_mode"?,"tailscale_mode"?}` |
| `GET` | `/instances/{id}/incus-info` | Raw Incus state |
| `PUT` | `/instances/{id}/incus-config` | Edit raw Incus config |
| `GET` | `/instances/{id}/debug-logs` | Creation/setup logs |
| `POST` | `/instances/{id}/exec` | Run a command inside the instance: `{"command": [...]}` |
| `POST` | `/instances/{id}/reapply-setup` | Re-run template setup commands |

Lifecycle actions (`start`/`stop`/`rebuild`/`delete`) live under the non-admin `/api/v1/instances/{id}/...` paths but an admin key can call them on **any** instance id (ownership checks are bypassed for admin role) — see the user skill for the exact list.

### Templates
`GET/POST /templates`, `PUT/DELETE /templates/{id}`, `POST /templates/import`, `GET /templates/mixins`, `POST /templates/{id}/debug`, `POST /templates/{id}/duplicate`, `GET /templates/{id}/export-yaml`, `POST /templates/{id}/update-from-yaml`, `POST /templates/{id}/save-to-disk`, `POST /templates/{id}/reload-from-disk`.

### Users
`GET/POST /users` (`POST` body: `{"email","name","password","role":"admin"|"user"}`), `PUT/DELETE /users/{id}`, `GET /users/{id}/keys`, `POST /users/{id}/keys/generate`, `DELETE /users/{id}/keys/{key_id}`.

### API keys (on behalf of any user)
`GET /users/{id}/api-keys` (list), `POST /users/{id}/api-keys` `{"name"}` (create — plaintext shown once), `POST /users/{id}/api-keys/{key_id}/regenerate` (rotate in place — plaintext shown once). Regular users can only view/regenerate their own; only you can create one for someone else or for the first time.

### Infrastructure
- `GET /servers` — registered Incus servers
- `GET /images` — available images per server
- `GET /disks` — every user's persistent volumes (`user_id`, `user_email`, `instance_name`, `volume_name`, `size_gb`, `mount_path`, ...)

### Settings & keys
- `GET/PUT/DELETE /settings/tailscale-key` — platform-wide Tailscale auth key
- `GET/POST /managed-keys`, `DELETE /managed-keys/{id}`, `GET/PUT /managed-keys/{id}/users` — SSH keys managed centrally and assigned to users
- Git repos: `GET/POST /repos`, `PUT/DELETE /repos/{id}`, `POST /repos/{id}/sync`, `GET /repos/server-key`

## Working conventions

- **Read before you write.** `GET /admin/instances` and `GET /admin/disks` give you the full picture of what exists before you create/delete anything.
- **Deletion is irreversible** — deleting an instance destroys its container; deleting a volume destroys the workspace data unless `persistence.mode` kept it isolated. Confirm with the operator before any `DELETE`, and before any `POST .../rebuild` on an instance you didn't just create.
- **Creating for a user** requires a valid `user_id` and `template_id` — list both first if you don't already have them.
- Non-2xx responses are JSON `{"error": "..."}`; treat 401 as a bad/revoked key and 403 as attempting an admin action with a non-admin key.
