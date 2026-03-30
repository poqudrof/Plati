# Plati - Technologies

## Backend

| Technology | Role | Why |
|---|---|---|
| **Go 1.23** | Language | Fast compilation, static binary, strong concurrency primitives, native Incus SDK |
| **chi v5** | HTTP router | Lightweight, stdlib-compatible, middleware-friendly, route grouping |
| **sqlx** | Database access | Thin layer over `database/sql` — struct scanning without ORM overhead |
| **SQLite 3** (go-sqlite3) | Database | Zero-ops embedded database, WAL mode for concurrent reads, single-file backup |
| **viper** | Configuration | YAML config loading with environment variable overrides |
| **golang-jwt/v5** | Authentication | JWT token generation and validation |
| **bcrypt** (golang.org/x/crypto) | Password hashing | Adaptive cost factor for admin password |
| **go-oidc v3** (coreos) | OIDC client | Microsoft Entra ID authorization code flow |
| **oauth2** (golang.org/x/oauth2) | OAuth2 | Token exchange for Entra OIDC |
| **Incus Go SDK v6** (lxc/incus) | Container management | Native Go client for Incus instance/volume/image operations |
| **go:embed** | Migration embedding | SQL migrations compiled into the binary |
| **crypto/aes + cipher.GCM** | Secret encryption | AES-256-GCM encryption for user secrets at rest |

## Frontend

| Technology | Role | Why |
|---|---|---|
| **SvelteKit 2** | Framework | File-based routing, SSR support, Vite-powered builds |
| **Svelte 5** | UI library | Runes reactivity (`$state`, `$effect`, `$props`), compiled components |
| **TypeScript** | Language | Type safety for API types and component props |
| **Tailwind CSS 4** | Styling | Utility-first CSS, no custom CSS files needed |
| **Vite 6** | Build tool | Fast HMR in dev, optimized production builds |

## Testing

| Technology | Role | Why |
|---|---|---|
| **Selenium** (Python) | E2E browser tests | Headless Chromium, full page interaction and assertion |
| **curl** (bash scripts) | API integration tests | Lightweight HTTP lifecycle testing without dependencies |

## Infrastructure

| Technology | Role | Why |
|---|---|---|
| **Incus** | Container runtime | LXD fork — system containers with persistent volumes, TLS API, image management |
| **SQLite WAL** | Storage | Write-ahead logging for concurrent read access during writes |
| **TLS mutual auth** | Incus connectivity | Client certificates for secure server-to-server communication |
| **httpOnly cookies** | Session management | JWT stored in cookies — not accessible via JavaScript, mitigates XSS token theft |

## Key Dependencies (Go modules)

```
github.com/go-chi/chi/v5          v5.2.1
github.com/go-chi/cors             v1.2.1
github.com/jmoiron/sqlx            v1.4.0
github.com/mattn/go-sqlite3        v1.14.24
github.com/spf13/viper             v1.19.0
github.com/golang-jwt/jwt/v5       v5.2.1
github.com/coreos/go-oidc/v3       v3.12.0
github.com/lxc/incus/v6            v6.8.0
golang.org/x/crypto                v0.31.0
golang.org/x/oauth2                v0.25.0
```

## Key Dependencies (Node)

```
@sveltejs/kit                      ^2.16.0
svelte                             ^5.0.0
tailwindcss                        ^4.0.0
vite                               ^6.0.0
typescript                         ^5.0.0
```
