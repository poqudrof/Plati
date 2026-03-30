# Plati - Future Architectural Improvements

## Database

### Migrate from SQLite to PostgreSQL
SQLite's single-writer constraint limits throughput under concurrent load. Migrating to PostgreSQL would enable multiple concurrent writers, row-level locking, and better support for horizontal scaling. The `sqlx` layer makes this feasible — queries use `?` placeholders (rewrite to `$1`), and the schema avoids SQLite-specific syntax except for `AUTOINCREMENT`.

### Add connection pooling
Currently `MaxOpenConns=1` for SQLite safety. With PostgreSQL, a proper connection pool (pgxpool or sqlx pool with tuned limits) would improve concurrent request handling.

### Introduce a migration tool
The current embedded migration runner is minimal (version tracking + sequential application). Adopting `golang-migrate` or `goose` would add rollback support, migration status inspection, and CLI tooling for developers.

## Authentication & Authorization

### Replace cookie auth with token refresh mechanism
The current JWT in httpOnly cookies has a fixed expiry with no refresh flow. Adding short-lived access tokens with a refresh token rotation would improve security posture and allow session revocation without invalidating all tokens (which currently requires changing the JWT secret).

### Add RBAC beyond admin/user
The current model has two roles: `admin` and `user`. A more granular RBAC system (e.g., `org-admin`, `team-lead`, `developer`) with per-resource permissions would support multi-team deployments. This could be modeled with a `roles` table and a `user_roles` join table.

### Support multiple identity providers
Only Microsoft Entra ID is supported. Abstracting the OIDC layer behind a provider interface would allow adding GitHub, Google, or generic OIDC providers without modifying auth logic.

## Incus Integration

### Smarter server selection
The current first-fit algorithm iterates servers and picks the first with capacity. A weighted scoring strategy (factoring in current load, memory usage, network proximity, instance count) would distribute workloads more evenly.

### Async instance operations
Instance creation (image pull, cloud-init, volume attach, start) can take 30+ seconds. Currently this blocks the HTTP request. Moving to an async model — return immediately with status `provisioning`, update via background worker, notify frontend via SSE or polling — would improve UX and prevent HTTP timeouts.

### Health monitoring and auto-recovery
The server health check is only triggered via admin API. A periodic background health checker that marks servers offline and optionally migrates instances to healthy servers would improve reliability.

### Instance resource monitoring
No resource usage data (CPU, memory, disk) is collected from running instances. Adding an agent or polling Incus metrics would enable usage dashboards and capacity planning.

## Backend Architecture

### Introduce dependency injection
Services are wired manually in `main.go`. A DI container or at minimum constructor-based injection with interfaces would simplify testing and make the dependency graph explicit. The Incus client already uses an interface (`IncusClient`); extending this pattern to services and queries would enable full unit testing without a database.

### Add structured logging
The current logging uses `log.Printf` (unstructured). Adopting `slog` (stdlib, Go 1.21+) with JSON output would enable log aggregation, filtering by level/component, and correlation IDs per request.

### Request validation layer
Input validation is scattered across handlers. A declarative validation approach (struct tags with a validator like `go-playground/validator`, or a middleware-based schema validator) would centralize validation logic and produce consistent error responses.

### API versioning strategy
Routes are under `/api/v1/` but there is no versioning mechanism beyond the URL prefix. Defining a clear policy (URL-based, header-based, or content negotiation) before a second version is needed would avoid breaking changes.

### Rate limiting
No rate limiting exists. Adding per-user and per-IP rate limiting (e.g., `chi` middleware or `golang.org/x/time/rate`) would protect against abuse, especially on instance creation and auth endpoints.

## Frontend Architecture

### Server-side rendering for initial load
The SPA currently loads a blank shell, then fetches auth state client-side. Using SvelteKit's server-side load functions with a backend-for-frontend (BFF) pattern would eliminate the flash of loading state and improve perceived performance.

### State management
Global state uses Svelte writable stores (`currentUser`, `isLoading`). As the app grows, a more structured approach — either Svelte 5 fine-grained reactivity with context-based state, or a dedicated store library — would prevent prop-drilling and store sprawl.

### Error boundaries
No error boundaries exist. A component-level error boundary pattern would prevent a single failed API call from crashing the entire page, showing a localized error message instead.

### Offline-first capabilities
Instance cards could cache their last-known state in localStorage for instant rendering on revisit, with background refresh. Not critical, but improves perceived speed on slow connections.

## Operational

### Observability stack
No metrics, tracing, or structured logs are emitted. Adding OpenTelemetry instrumentation (traces for instance lifecycle, metrics for request latency and Incus operation duration) would support production monitoring.

### Configuration hot-reload
Configuration is loaded once at startup. Supporting hot-reload for non-critical settings (sleep timeout, templates directory) via file watching or SIGHUP would reduce restarts.

### Backup and restore
SQLite databases need periodic backup (`.backup` command or file copy during checkpoint). An automated backup mechanism, potentially tied to the sleep worker's tick, would prevent data loss.

### Container image registry
Templates reference images by name (e.g., `ubuntu:24.04`). A managed image registry or image preloading strategy would ensure consistent image availability across servers and reduce instance creation time.
