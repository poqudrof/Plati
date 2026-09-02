# Plati - Architecture

## Overview

Plati is an internal platform for creating and managing persistent Incus-based development environments. It follows a classic two-tier web application architecture: a Go HTTP backend serving a REST API, and a SvelteKit single-page application as the frontend.

```
┌─────────────────────────────────────────────────────┐
│                     Browser                         │
│              SvelteKit SPA (Svelte 5)               │
└──────────────┬──────────────────────────────────────┘
               │ HTTP (JSON) + httpOnly JWT cookie
               ▼
┌─────────────────────────────────────────────────────┐
│                  Go Backend (chi)                    │
│  ┌──────────┐ ┌──────────┐ ┌─────────────────────┐  │
│  │  Router   │ │   Auth   │ │    Middleware        │  │
│  │  (chi)    │ │ JWT/Entra│ │ CORS/Logger/Recover │  │
│  └────┬─────┘ └──────────┘ └─────────────────────┘  │
│       ▼                                              │
│  ┌──────────┐ ┌──────────┐ ┌─────────────────────┐  │
│  │ Handlers │→│ Services │→│   Incus Client Pool  │  │
│  └──────────┘ └────┬─────┘ └──────────┬──────────┘  │
│                    │                   │              │
│                    ▼                   ▼              │
│             ┌──────────┐    ┌───────────────────┐    │
│             │  SQLite   │    │  Incus Server(s)  │    │
│             │  (sqlx)   │    │  (TLS mutual auth)│    │
│             └──────────┘    └───────────────────┘    │
└─────────────────────────────────────────────────────┘
```

## Backend Architecture

### Layered Structure

The backend follows a clean layered architecture with strict dependency direction:

```
handlers → services → queries → database
              ↓
          incus client
```

- **Handlers** (`internal/handlers/`): HTTP request/response handling. Parse input, call services, write JSON responses. No business logic.
- **Services** (`internal/services/`): Business logic layer. Orchestrates database queries and Incus operations. Owns lifecycle workflows (create, start, stop, rebuild, delete).
- **Queries** (`internal/database/queries/`): Pure SQL data access. One file per entity. Returns Go structs, no HTTP awareness.
- **Models** (`internal/models/`): Shared data structures with `db` and `json` struct tags.

### Entry Point

`cmd/plati-server/main.go` wires everything together:

1. Load YAML configuration (viper)
2. Open SQLite database and run embedded migrations
3. Connect to Incus servers (TLS), build connection pool
4. Initialize Entra OIDC provider (optional)
5. Create services, handlers, router
6. Sync servers and import default templates from disk
7. Start auto-sleep background worker
8. Listen on configured address with graceful shutdown (SIGINT/SIGTERM)

### Authentication

Two authentication methods, unified behind a single JWT middleware:

- **Admin password**: bcrypt-hashed password in config. `POST /auth/login` issues a JWT.
- **Microsoft Entra ID**: OIDC authorization code flow. `GET /auth/entra` redirects to Microsoft, `GET /auth/callback` exchanges code for user info and issues JWT.

JWTs are stored in `httpOnly` cookies (`plati_token`), not in localStorage. The `AuthMiddleware` reads the cookie, validates the JWT, and injects the user into the request context. `AdminMiddleware` checks the user's role.

### Incus Integration

The `internal/incus/` package abstracts Incus server communication:

- **Client** (`client.go`): Wraps the Incus Go SDK with TLS mutual authentication. Implements the `IncusClient` interface for testability.
- **Pool** (`pool.go`): Thread-safe map of named Incus clients. Supports multi-server deployments.
- **Instance operations** (`instances.go`): Build instance config from templates (resource limits). SSH key injection and setup commands via exec. Get instance IP addresses.
- **Volume operations** (`volumes.go`): Create, attach, detach, and delete the storage volumes that hold an instance's persistent data (the login user's home by default).

### Instance Lifecycle

```
User selects template
        │
        ▼
  Validate template
        │
        ▼
  Select server (first-fit with capacity)
        │
        ▼
  Create persistent volume(s)
        │
        ▼
  Create Incus instance (image + profiles + limits)
        │
        ▼
  Attach volume at its mount path
        │
        ▼
  Start instance → return SSH connection info
```

**Rebuild** preserves the workspace volume: stop → detach volume → delete instance → recreate from template → reattach volume → start. This lets users reset their OS environment while keeping project files.

**Auto-sleep**: A background goroutine checks every 5 minutes for running instances whose `last_active_at` exceeds the configured timeout, and stops them to free resources.

### Database

SQLite in WAL mode with foreign keys enabled. Single-writer (`MaxOpenConns=1`).

Migrations are embedded in the binary via `//go:embed` and applied at startup. A `schema_migrations` table tracks applied versions.

Tables: `users`, `ssh_keys`, `secrets`, `servers`, `templates`, `volumes`, `instances`.

Template metadata (profiles, resources) is stored as JSON text columns, parsed at the service layer.

### Secret Encryption

User secrets (environment variables, API keys) are encrypted at rest using AES-256-GCM. The encryption key is configured in `plati.yaml`. The nonce is prepended to the ciphertext and stored as hex.

### Server Selection

When creating an instance, the backend iterates online servers and picks the first one where `current_instance_count < max_instances`. This is a simple first-fit strategy.

## Frontend Architecture

### SvelteKit + Svelte 5

The frontend is a SvelteKit 2 application using Svelte 5 runes syntax (`$state`, `$effect`, `$props`, `$bindable`). It runs as a client-side SPA with the Vite dev server proxying API calls to the Go backend.

### Route Structure

```
routes/
├── +layout.svelte          # Root: auth init, navbar, notifications
├── login/+page.svelte      # Entra + admin password login
├── callback/+page.svelte   # OAuth callback handler
└── (app)/                  # Protected route group
    ├── +layout.svelte      # Auth guard: redirect to /login if unauthenticated
    ├── dashboard/           # Instance list
    ├── instances/           # Create + detail views
    ├── settings/            # SSH keys + secrets management
    └── admin/               # Templates, servers, images, users
```

### Auth Flow

1. Root `+layout.svelte` calls `GET /auth/me` on mount to check session
2. Sets `currentUser` and `isLoading` stores
3. `(app)/+layout.svelte` watches these stores via `$effect` — redirects to `/login` if not authenticated, renders children if authenticated
4. Page components use `if (browser) { loadFn(); }` for data fetching (avoids SSR hydration issues with conditional rendering)

### API Client

`$lib/api/client.ts` provides a typed HTTP client with `credentials: 'include'` for cookie-based auth. Entity-specific functions are organized in `$lib/api/index.ts`. All API responses are typed via TypeScript interfaces in `$lib/api/types.ts`.

### Stores

- `auth.ts`: `currentUser` (writable) and `isLoading` (writable) — global authentication state
- `notifications.ts`: Toast notification queue with auto-dismiss

## Configuration

A single YAML file (`config/plati.yaml`) drives all runtime configuration:

- Server bind address and frontend URL (CORS)
- Entra ID OIDC credentials (client ID, secret, tenant)
- Admin password hash (bcrypt)
- JWT signing secret
- AES encryption key for user secrets
- SQLite database path
- Incus server endpoints with TLS client certificates
- Auto-sleep timeout
- Template import directory

## Deployment Topology

```
┌────────────┐     ┌──────────────┐     ┌──────────────────┐
│  Browser   │────▶│  Go Backend  │────▶│  Incus Server 1  │
│            │     │  :8080       │     │  (TLS)           │
└────────────┘     │              │     └──────────────────┘
                   │  SQLite DB   │     ┌──────────────────┐
                   │  plati.db    │────▶│  Incus Server 2  │
                   └──────────────┘     │  (TLS)           │
                                        └──────────────────┘
```

In development, the SvelteKit dev server (`:5173`) proxies API requests to the Go backend (`:8080`). In production, the Go backend serves the built SvelteKit static files or sits behind a reverse proxy.
