# Plati

**Open AI coding agents to your whole team, in sandboxes your administrators control.**

Plati is a self-hosted platform that gives each person a micro-VM (an [Incus](https://linuxcontainers.org/incus/)
container) with an AI agent harness — Claude Code, Codex CLI, OpenCode — already installed
next to the project's repositories. Everyone can take part in a project through the agent:
the website, a feature on the web app, a dedicated AI tool with its own skills. Administrators
decide who gets a machine, which Git repositories it can reach and which secrets it carries.

## Who it is for

| | What Plati gives them |
|---|---|
| **Contributors** (developers or not) | A ready machine with the agent and the team's skills; access from a laptop or a phone; a shared terminal to work together |
| **Developers** | A persistent workspace that survives rebuilds; agents left running in the background; demos on the VPN instead of the internet |
| **Platform developers and admins** | Micro-VMs created, cloned and transferred in a few clicks; colleagues' SSH keys installed for them; Git access per skill level; CPU/memory limits and auto-stop |
| **Managers** | Data stays on company servers; the AI provider for the whole company is a template change; no personal cloud machines to keep track of |

## What a micro-VM comes with

- **An AI agent harness.** Templates are YAML with reusable mixins. Claude Code ships as a
  mixin (`config/templates/mixins/claude-code.yaml`); Codex CLI or OpenCode are one install
  line in a template. Swapping provider for everyone means editing the template and rebuilding.
- **Remote access from anywhere.** Each machine joins your [Tailscale](https://tailscale.com)
  network, so it is reachable over SSH (and Claude Code's remote access) from a laptop or a
  phone — on your hardware, not on a vendor's cloud machines.
- **Private demos.** An app running in the machine is served on its tailnet name
  (`https://<name>.<tailnet>.ts.net`): colleagues see the progress, the internet does not.
- **Collaborative terminal.** [SSHX](https://github.com/catie-aq/sshx) (our fork) runs as a
  service; share the link and work in the same shell. OpenVSCode Server is available too.
- **Persistence.** The home directory lives on a volume that survives rebuilds; snapshots and
  downloads are in the **Storage** tab.

## What administrators control

- **Accounts:** password or Microsoft Entra ID login, admin/user roles.
- **SSH keys:** users' public keys are installed in their machines, and can be added to a
  running one without a rebuild.
- **Git access by level:** a managed SSH key is assigned per user and injected into their
  machines, so the key — and therefore which repositories it opens — follows each person's level.
- **Secrets:** stored encrypted (AES-256-GCM), injected as environment variables.
- **Machines:** create, clone (to anyone), transfer to their future owner, set CPU/memory
  limits, and auto-stop idle ones (default 4h).

## For platform developers and admins: rolling it out

The path from a bare server to a team working with agents, in four steps.

### 1. Install the platform and Incus

Incus runs on the host and hosts the micro-VMs; Plati itself runs in Docker next to it
(backend, frontend and a Tailscale node, see [Quick Start](#quick-start)). The machines can
run Docker in turn: the Docker mixin (`config/templates/mixins/docker.yaml`) sets
`security.nesting` and uses `fuse-overlayfs`, which unprivileged Incus containers need.

- Incus install and TLS client certificates: [doc/quickstart.md](doc/quickstart.md),
  [doc/incus-config.md](doc/incus-config.md)
- Incus profiles: `scripts/setup-docker-profile.sh`, `scripts/setup-tailscale-profile.sh`,
  `scripts/setup-gpu-profile.sh`

### 2. Add users and their SSH keys

In **Admin → Users**, create each account (password, or Microsoft Entra ID), then under
**Keys** paste the public keys of the machines they will connect from. Those keys are written
to `authorized_keys` of every machine the user owns; on a running one, the instance's
**SSH Access** tab installs a key immediately. Then, among the managed SSH keys on the admin page,
assign each user to the key that matches their level: that key is injected into their
machines and decides which Git repositories they can reach.

### 3. Prepare the machines and onboard users

Describe each project once as a template in `config/templates/` — image, repositories to
clone, AI agent, skills, mixins (Tailscale, SSHX, OpenVSCode, Docker), resources. Create a
machine from it, check it works, then give each user a short tutorial: how to reach their
machine over the tailnet (SSH, OpenVSCode, SSHX link), start the agent, and show their work
to colleagues on the machine's tailnet URL.

### 4. Manage it the simple way

- **Copy** a machine that works instead of setting up a new one — to yourself or straight
  into another user's account (dashboard → **All users** → **Duplicate**).
- **Transfer** a machine you prepared to the person it is for: same container and data,
  their keys in, yours out.
- **Roll out a new configuration** by editing the template (or a mixin shared by several),
  then **Rebuild**: the container is recreated from the template, the home volume is kept.
  This is also how the whole company switches AI provider.
- Adjust **CPU / memory** per machine and the **auto-stop** delay from the instance page.

## Quickstart for AI agents

Plati can itself be driven by an agent — Claude Code or any tool that can call an HTTP API.
The repository ships what it needs in `.claude/`, picked up automatically when Claude Code
runs in this directory:

| | Scope | Key |
|---|---|---|
| `.claude/skills/plati-user-agent` | One user's own machines: create, start, stop, rebuild, storage, secrets, preferences | `PLATI_USER_API_KEY` |
| `.claude/skills/plati-admin-agent` | Every user's machines, users, templates, servers, settings | `PLATI_ADMIN_API_KEY` |
| `.claude/agents/template-creator.md` | Writes and validates templates in `config/templates/` | none (works on files) |

**1. Get an API key.** Only an admin can issue the first one: **Admin → Users → API Keys →
Create Key** on the user's row, or through the API. The key (`plati_…`) is shown once; its
owner can regenerate it later. It carries its owner's role, so give an agent the key of the
account it should act as — a user key for the user skill, an admin's key for the admin skill.

```bash
# As an admin, logged in (cookie) — create a key for user {id}
curl -b cookie.txt -X POST "$PLATI_BASE_URL/api/v1/admin/users/{id}/api-keys" \
  -H 'Content-Type: application/json' -d '{"name":"claude-agent"}'
```

**2. Point the agent at Plati.**

```bash
export PLATI_BASE_URL=http://127.0.0.1:8090     # Docker dev stack; the tailnet URL works too
export PLATI_USER_API_KEY=plati_...              # or PLATI_ADMIN_API_KEY
curl -s -H "Authorization: Bearer $PLATI_USER_API_KEY" "$PLATI_BASE_URL/api/v1/instances"
```

**3. Ask in plain language.** From Claude Code in this repository:

```text
/plati-user-agent  create a machine from the "site-web" template and tell me its tailnet URL
/plati-admin-agent copy alice's "site-web" machine to bob and install his SSH keys
```

The skills list every endpoint and the conventions to follow — the admin one asks before
anything destructive (deleting a machine or a user, a transfer).

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

The Docker stack is self-contained and published on your tailnet:

```bash
cp .env.plati.example .env.plati           # infrastructure values
cp .env.tailscale.example .env.tailscale   # TS_AUTHKEY, TS_HOSTNAME, APP_PORT
docker compose --env-file .env.tailscale -f docker-compose.dev.yml up -d
```

Open `https://<TS_HOSTNAME>.<tailnet>.ts.net` (or `http://127.0.0.1:5300`), complete the
`/setup` wizard, then restart the stack. See [CLAUDE.md](CLAUDE.md) for the details and the prod
compose file.

Host-native alternative (Go 1.23+ and Node 20+ installed locally):

```bash
./scripts/setup-dev.sh   # generate config with secrets
make migrate             # apply database migrations
make dev                 # backend :8080 + frontend :5173 (or: mprocs)
```

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
  → Create persistent volume(s) (/home/ubuntu by default)
  → Create Incus instance (image + profiles + limits)
  → Attach volume
  → Start instance → SSH connection info
```

**Rebuild** preserves the persistent volume: stop → detach volume → delete → recreate → reattach → start.

**Auto-sleep**: a background worker stops instances whose timer has run out — `sleep_timeout`
(default: 4h), overridable or switched off per instance from the **Status** tab. The timer
counts from the last start, not from actual use.

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


## Tailscale service 

tailscale serve  --service=svc:plati --https=443 127.0.0.1:5300
(not required !) tailscale serve  --service=svc:plati-server 127.0.0.1:8
−> Allow in admin 

#To remove config for the service, run: tailscale serve clear svc:plati