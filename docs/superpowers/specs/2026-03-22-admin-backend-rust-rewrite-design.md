# Admin Dashboard Backend: Go to Rust Rewrite

**Date:** 2026-03-22
**Status:** Draft
**Scope:** `admin-dashboard/backend/` complete replacement

## 1. Overview

Replace the admin dashboard Go backend with Rust (axum). Full replacement — no gradual migration, no co-existence period. The Go backend directory is removed after the Rust backend is complete.

### Goals

- Complete functional parity (minus removed features)
- Fix known bugs from Go implementation
- Performance optimizations leveraging Rust ecosystem
- Single binary deployment with embedded frontend (unchanged)

### Non-Goals

- OpenTelemetry support (removed)
- Log viewer feature (removed from both backend and frontend)
- Changing the frontend (React) — only backend replacement
- Changing the deployment topology (single-host Docker Compose)

## 2. Removed Features

| Feature | Go Location | Reason |
|---------|-------------|--------|
| OpenTelemetry | `bootstrap/`, `server.go`, `status.go`, `proxy.go` | Scope reduction |
| Log viewer API | `handlers_logs.go`, `logs/logs.go` | Removal target (frontend included) |
| Log viewer routes | `/admin/api/logs/*` | Removal target |

## 3. Bug Fixes (from Go review)

| Bug | Go Location | Fix |
|-----|-------------|-----|
| ETag double-write | `middleware/etag.go:21-28` | Buffer response body, conditionally write only if no 304 |
| SSR fetch unbounded body | `ssr/ssr.go:232` | `Content-Length` check + body read limit (2 MB max) |
| Login failure returns 200 | `handlers_auth.go:101` | Return 401 for authentication failures |
| CSP Report-Only duplicate | `auth.go:326-346` | Remove Report-Only header (identical to enforced CSP) |
| heartbeat/logout auth duplication | `routes.go:33-38` | Move to authenticated route group, remove inline auth checks |

## 4. Tech Stack

| Role | Crate |
|------|-------|
| HTTP framework | `axum` |
| Runtime | `tokio` |
| Middleware | `tower`, `tower-http` |
| Session store | `redis` (Valkey compatible) + `deadpool-redis` |
| Docker API | `bollard` |
| WebSocket | `axum` built-in (extract::ws) |
| Reverse proxy (H2C) | `hyper` + `hyper-util` |
| HMAC / bcrypt | `hmac`, `sha2`, `bcrypt` |
| JSON | `serde` + `serde_json` |
| Environment vars | `dotenvy` + manual parsing |
| Static file embed | `rust-embed` |
| System stats | `sysinfo` |
| OpenAPI docs | `utoipa` + `utoipa-swagger-ui` |
| Logging | `tracing` + `tracing-subscriber` |
| CORS | `tower-http::CorsLayer` |
| Error types | `thiserror` |
| ETag hashing | `xxhash-rust` (non-crypto, fast) |

## 5. Project Structure

```
admin-dashboard/backend-rs/
├── Cargo.toml
├── Makefile
├── src/
│   ├── main.rs                     # entrypoint, graceful shutdown
│   ├── config.rs                   # Config, SecurityConfig, SessionConfig
│   ├── error.rs                    # AppError (AuthError, DockerError, ProxyError)
│   ├── state.rs                    # AppState (Arc-shared)
│   ├── auth/
│   │   ├── mod.rs
│   │   ├── session.rs              # ValkeySessionStore, SessionProvider trait
│   │   ├── hmac.rs                 # session ID sign/verify
│   │   ├── middleware.rs           # AuthMiddleware, SecurityHeaders
│   │   └── rate_limiter.rs         # LoginRateLimiter (tokio + CancellationToken)
│   ├── middleware/
│   │   ├── mod.rs
│   │   ├── csrf.rs                 # CSRF Double Submit Cookie (3-state mode)
│   │   └── etag.rs                 # ETag (buffered, fixed)
│   ├── handlers/
│   │   ├── mod.rs
│   │   ├── auth.rs                 # login, logout, heartbeat
│   │   ├── docker.rs               # containers CRUD, log stream WebSocket
│   │   └── status.rs               # aggregated status, system stats WebSocket
│   ├── docker/
│   │   ├── mod.rs
│   │   └── service.rs              # bollard-based, DockerProvider trait
│   ├── proxy/
│   │   ├── mod.rs
│   │   └── bot_proxy.rs            # hyper H2C + WS routing
│   ├── ssr/
│   │   ├── mod.rs
│   │   └── injector.rs             # SSR data injection (size-limited)
│   ├── status/
│   │   ├── mod.rs
│   │   ├── collector.rs            # multi-service health aggregation
│   │   └── system_stats.rs         # sysinfo + broadcast channel
│   └── static_files.rs             # rust-embed serving
├── static/
│   └── dist/                       # frontend build output (copied at Docker build)
```

## 6. Architecture

### Route Map

```
GET  /health                                → 200 OK (no auth)
GET  /                                      → static_files + SSR injection
GET  /assets/*                              → static_files (immutable cache)
GET  /favicon.svg                           → static_files

POST /admin/api/auth/login                  → handlers::auth (rate limiter, NO auth middleware)

  [AuthMiddleware group — all routes below require valid session cookie]
  POST /admin/api/auth/logout               → CSRF middleware + handlers::auth
  POST /admin/api/auth/heartbeat            → CSRF middleware + handlers::auth

  GET  /admin/api/docker/health             → handlers::docker
  GET  /admin/api/docker/containers         → handlers::docker (cached 5s)
  POST /admin/api/docker/containers/:name/restart → CSRF + handlers::docker
  POST /admin/api/docker/containers/:name/stop    → CSRF + handlers::docker
  POST /admin/api/docker/containers/:name/start   → CSRF + handlers::docker
  GET  /admin/api/docker/containers/:name/logs/stream → WebSocket (StreamLimiter)

  GET  /admin/api/status                    → handlers::status
  GET  /admin/api/ws/system-stats           → WebSocket (broadcast)

  ANY  /admin/api/holo/*                    → proxy::bot_proxy (H2C / WS)

NoRoute (SPA fallback)                      → static_files + SSR injection
                                               EXCEPT: /admin/api/* → 404 JSON
```

### Changes from Go

- `logout` and `heartbeat` moved into authenticated route group (removes inline auth duplication)
- Login failure returns 401 (was 200)
- CSP `Report-Only` header removed

### AppState

```rust
struct AppState {
    config: Config,
    security_config: SecurityConfig,
    sessions: ValkeySessionStore,       // deadpool-redis pool
    rate_limiter: LoginRateLimiter,      // Arc<Mutex<HashMap>> + CancellationToken
    docker_svc: Option<DockerService>,   // bollard client
    bot_proxy: Option<BotProxy>,         // hyper H2C + WS clients
    status_collector: StatusCollector,   // reqwest::Client shared
    ssr_injector: SsrInjector,           // cached HTML + reqwest::Client
    stream_limiter: StreamLimiter,       // Semaphore-based
    stats_tx: broadcast::Sender<SystemStats>,  // shared system stats
}
```

## 7. Behavior Contracts

### 7.1 Session & Cookie Contract

**Valkey key schema:** `session:admin:<session_id>` — JSON payload, TTL-managed.

**Session JSON format:**
```json
{
  "id": "<hex string, 64 chars>",
  "created_at": "<RFC3339>",
  "expires_at": "<RFC3339>",
  "absolute_expires_at": "<RFC3339>",
  "last_rotated_at": "<RFC3339>"
}
```

**Session constants:**
| Parameter | Value |
|-----------|-------|
| Sliding TTL | 30 minutes |
| Absolute timeout | 8 hours |
| Idle session TTL | 10 seconds |
| Grace period (old session after rotation) | 30 seconds |
| Rotation interval | 15 minutes |

**Cookie contract:**
| Cookie | HttpOnly | SameSite | Secure | Purpose |
|--------|----------|----------|--------|---------|
| `admin_session` | true | Strict | TLS or `FORCE_HTTPS` | HMAC-signed session ID (`<id>.<base64url_sig>`). Path=`/`, session cookie (Max-Age=0, expires on browser close). Delete: set Max-Age=-1 with same attributes. |
| `csrf_token` | false (JS-readable) | Strict | TLS or `FORCE_HTTPS` | CSRF token (`<nonce_hex>.<base64url_sig>`). Path=`/`, session cookie. Delete: set Max-Age=-1 with same attributes. |

**Cutover:** Full session invalidation on deploy. All users must re-login. (Valkey key format is identical, but new binary = new deployment = acceptable session reset.)

### 7.2 CSRF Contract

Format: `<nonce_hex(64)>.<hmac_base64url>` where HMAC input = `"csrf:" + session_id + ":" + nonce_hex`.

Validation flow (POST/PUT/DELETE only):
1. Read `X-CSRF-Token` header — reject if missing
2. Read `csrf_token` cookie — reject if missing
3. Compare header == cookie — reject if mismatch
4. Extract session ID from `admin_session` cookie, verify HMAC signature
5. Verify CSRF token signature against session ID

3-state mode (`CSRF_MODE`):
- `enforce`: reject on failure (403)
- `monitor`: log warning, allow request
- `off`: skip validation

### 7.3 SSR / SPA Serving Contract

Request flow for `GET /` and SPA fallback (NoRoute):
1. Check `admin_session` cookie → validate HMAC → validate session in Valkey
2. If **not authenticated**: serve cached HTML as-is (no SSR data injection)
3. If **authenticated**: call `ssr_injector.inject_for_path(path, session_cookie)`
   - `/dashboard/members` → fetch `GET <HOLO_BOT_URL>/api/holo/members` (with session cookie forwarded)
   - `/dashboard/settings` → fetch settings from holo-bot + Docker health/containers from local
   - Other paths → no SSR data
4. Inject `<script>window.__SSR_DATA__=<json>;</script>` before `</head>`
5. XSS defense: `serde_json` auto-escapes `</` — no manual replacement needed
6. **SPA fallback boundary**: `/admin/api/*` paths that don't match any route return 404 JSON — never fall through to SPA HTML
7. **Fallback chain** (non-API paths only): SSR injection failure → cached HTML → embedded index.html
7. **Body size limit**: upstream SSR fetch responses capped at 2 MB (`take()` on body stream)

Cache headers:
- HTML: `no-store, no-cache, must-revalidate`
- `/assets/*`: `public, max-age=31536000, immutable`
- `/favicon.svg`: `public, max-age=86400`

### 7.4 WebSocket Origin Verification

Independent from CORS — applies to WS upgrade requests only.

3-state mode (`WS_ORIGIN_MODE`):
- `enforce`: reject if Origin missing or not in allowed list
- `monitor`: log warning, allow all
- `off`: skip Origin check

Origin allowed list: same as `ALLOWED_ORIGINS` env var (parsed from `SecurityConfig`).
- Production: localhost origins filtered out (unless `ALLOW_LOCALHOST_IN_PROD=true`)
- Fallback origins (if env not set): `["https://admin.capu.blog", "http://localhost:5173"]`

### 7.5 Proxy Contract (`/admin/api/holo/*`)

Path rewrite: `/admin/api/holo/<path>` → `/api/holo/<path>`

Header manipulation:
- **Inject**: `X-API-Key: <HOLO_BOT_API_KEY>` (if configured)
- **Remove**: `Origin` header — prevents upstream bot CORS/Origin guard rejection

Transport routing:
- **WebSocket** (detected by `Upgrade: websocket` header) → HTTP/1.1 transport (H2C does not support `Connection: Upgrade`)
- **All other requests** → H2C transport (HTTP/2 cleartext, connection-pooled)

Error handling:
- Upstream unreachable (HTTP) → 502 `{"error": "Service unavailable"}`
- Upstream unreachable (WebSocket) → 502 `{"error": "WebSocket service unavailable"}`
- Upstream returns 404 → log warning (route mismatch indicator), forward response as-is

### 7.6 Stream Limiter Contract

Dual limiting for Docker log streams and WebSocket connections:
- **Global limit**: `GLOBAL_STREAM_LIMIT` (default: 10) — total concurrent streams
- **Per-session limit**: `PER_SESSION_STREAM_LIMIT` (default: 2) — per session ID

3-state mode (`STREAM_LIMIT_MODE`):
- `enforce`: reject with 429 if limit exceeded
- `monitor`: log, allow
- `off`: no limiting

Implementation: `tokio::sync::Semaphore` for global limit + `Arc<Mutex<HashMap<String, usize>>>` for per-session tracking.

Docker log stream timeout: 10 minutes max per connection.

### 7.7 Heartbeat Response Contract

```json
// Normal refresh
{"status": "ok"}

// With token rotation
{"status": "ok", "rotated": true, "absolute_expires_at": 1704067200, "csrf_token": "<new_token>"}

// Absolute timeout exceeded
HTTP 401: {"error": "Session expired", "absolute_expired": true}

// Idle tab (idle=true in request body)
{"status": "idle", "idle_rejected": true}

// Session not found / expired
HTTP 401: {"error": "Session expired"}
```

### 7.8 Status Collector Contract

Endpoints polled:
| Service | Health URL | Stats URL | Timeout |
|---------|-----------|-----------|---------|
| hololive-bot | `<HOLO_BOT_URL>/health` | `<HOLO_BOT_URL>/api/holo/stats` | 3 seconds |

Partial degradation: if a service health check fails, it is reported as `available: false` with zero values. Other services are unaffected.

Admin dashboard itself is always reported as available with current goroutine (tokio task) count.

### 7.9 TLS Contract

When `TLS_ENABLED=true`:
- axum server binds with `axum_server::tls_rustls` using `TLS_CERT_PATH` and `TLS_KEY_PATH`
- Graceful shutdown via `axum_server::Handle`

When `TLS_ENABLED=false` (default):
- Plain HTTP via `axum::serve`
- Graceful shutdown via `tokio::signal` + `with_graceful_shutdown`

## 8. Optimizations

### Performance

| Item | Go (current) | Rust (improved) |
|------|-------------|-----------------|
| System stats sharing | Per-WS-connection polling | Single collector task, `tokio::sync::broadcast` to all subscribers |
| ETag hashing | SHA256 per request | `xxhash` (non-crypto, ~10x faster) |
| Static file compression | Delegated to Cloudflare | Build-time brotli/gzip via `rust-embed`, `Accept-Encoding` routing |
| Valkey connections | Single client | `deadpool-redis` connection pool |
| Security headers | Per-request `c.Header()` calls | Pre-built `HeaderValue`, zero-alloc tower layer |
| H2C proxy connections | Potentially new per request | `hyper-util` connection pooling |
| HTTP client for health checks | Per-request client possible | Single `reqwest::Client` instance (internal connection pool) |

### Structural

| Item | Go (current) | Rust (improved) |
|------|-------------|-----------------|
| Rate limiter cleanup | Goroutine without context (leak possible) | `tokio::spawn` + `CancellationToken` for graceful shutdown |
| Session rotation | GET + SET + EXPIRE (3 ops, race condition) | Valkey Lua script (atomic) |
| Docker container list | API call per request | 5-second TTL cache (`tokio::sync::RwLock` + timestamp) |
| SSR XSS defense | Manual `</script>` replacement (3 passes) | `serde_json` escapes `</` by default — no extra work |
| Stream limiter | Manual counter with Mutex | `tokio::sync::Semaphore` (global) + per-session map |
| `findSubstring` reimplementation | Manual loop | Removed — use `str::find` |

### Worker/Resource Recycling

| Resource | Implementation |
|----------|----------------|
| Valkey connection pool | `deadpool-redis` — idle connection recycling, max idle time, stale eviction |
| HTTP client pool | `reqwest::Client` — single shared instance, internal hyper connection pool with recycling |
| H2C proxy pool | `hyper-util::client::legacy::Client` — upstream connection reuse |
| Docker client | `bollard` — single instance, internal connection reuse |
| Stream worker pool | `tokio::sync::Semaphore` — replaces manual counter; global + per-session dual limiting |

All pooled resources are `Arc`-shared via `AppState` and drained on graceful shutdown.

## 9. Error Handling

Domain-specific error types via `thiserror`:

```rust
enum AppError {
    Auth(AuthError),        // 401, 403, 429
    Docker(DockerError),    // 404 (unmanaged), 500, 503
    Proxy(ProxyError),      // 502
    Internal(anyhow::Error) // 500
}
```

Per-handler status code mapping:
| Scenario | Status |
|----------|--------|
| No/invalid session cookie | 401 |
| Login failure (bad credentials) | 401 |
| Rate limited | 429 (+ `Retry-After` header) |
| CSRF violation (enforce mode) | 403 |
| Container not managed | 404 |
| Docker/Valkey unavailable | 503 |
| Upstream proxy unreachable | 502 |
| Session creation failure | 503 |
| Internal error | 500 (generic message to client) |

- `impl IntoResponse for AppError` — auto-converts to HTTP response
- Internal error details logged via `tracing`, client receives generic message

## 10. Testing Strategy

| Layer | Approach | Key Scenarios |
|-------|----------|---------------|
| Unit | In-module `#[cfg(test)]` | HMAC sign/verify, CSRF token gen/validate, rate limiter window/lockout, config parsing, security mode 3-state parsing |
| Session | Table-driven tests | Sliding TTL refresh, absolute timeout expiry, rotation interval check, grace period overlap, idle session TTL |
| Stream limiter | Table-driven tests | Global limit hit, per-session limit hit, enforce vs monitor vs off mode, acquire/release symmetry |
| WS Origin | Table-driven tests | enforce reject, monitor allow+log, off skip, missing Origin, localhost filtering in prod |
| Integration | `axum::test::TestServer` + mock Valkey | Login → session → heartbeat → rotation → logout full flow |
| Docker handlers | `mockall` mock `DockerProvider` trait | List, restart, stop, start, managed check |
| Proxy | `axum::test` mock upstream | Path rewrite, X-API-Key injection, WS vs H2C routing, upstream 502 |
| SSR | Unit + integration | Auth/unauth path, body size limit, fallback chain, XSS escaping |
| E2E | Existing frontend | API contract compatibility verification |

## 11. Build & Deployment

### Dockerfile (multi-stage)

```
Stage 1: node:22-alpine       → frontend build (unchanged)
Stage 2: rust:1.87-alpine     → cargo build --release (copy dist → static/)
Stage 3: alpine:3.23          → single binary + ca-certificates + tini
```

### Makefile

```makefile
build:    cargo build --release
test:     cargo test
lint:     cargo clippy -- -D warnings
fmt:      cargo fmt --check
docker:   docker build ...
```

### Migration Steps

1. Create `admin-dashboard/backend-rs/` with full Rust implementation
2. Write new `admin-dashboard/Dockerfile` targeting Rust build
3. Test with existing frontend (API contract unchanged)
4. Remove `admin-dashboard/backend/` (Go)
5. Rename `backend-rs/` to `backend/`
6. Remove `admin-dashboard/backend` from `go.work`
7. Update `admin-dashboard/AGENTS.md` for Rust conventions

## 12. Environment Variables

### Removed (OTel)
- `OTEL_ENABLED`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_INSECURE`, `OTEL_SAMPLE_RATE`

### Removed (Log viewer — file logging config no longer needed, Rust uses stdout-only via tracing)
- `LOG_DIR`, `LOG_FILE_MAX_SIZE_MB`, `LOG_FILE_MAX_BACKUPS`, `LOG_FILE_MAX_AGE_DAYS`, `LOG_FILE_COMPRESS`

### Retained

| Env Var | Default | Description |
|---------|---------|-------------|
| `PORT` | `30190` | Listen port |
| `ENV` | `production` | Environment (`production` / `development`) |
| `FORCE_HTTPS` | `true` | Force Secure cookie flag |
| `LOG_LEVEL` | `info` | tracing log level |
| `ADMIN_USER` | `admin` | Admin username |
| `ADMIN_PASS_HASH` / `ADMIN_PASS_BCRYPT` | (required) | bcrypt hash, first non-empty wins |
| `SESSION_SECRET` / `ADMIN_SECRET_KEY` | (required) | HMAC secret, first non-empty wins |
| `SESSION_TOKEN_ROTATION` | `true` | Enable heartbeat session rotation |
| `VALKEY_URL` | `valkey-cache:6379` | Valkey connection address |
| `DOCKER_HOST` | `tcp://docker-proxy:2375` | Docker daemon TCP endpoint |
| `HOLO_BOT_URL` | `http://hololive-kakao-bot-go:30001` | Upstream bot URL |
| `HOLO_BOT_API_KEY` | (empty) | X-API-Key for bot proxy |
| `ALLOWED_ORIGINS` | fallback list | Comma-separated allowed origins |
| `ALLOW_LOCALHOST_IN_PROD` | `false` | Allow localhost origins in production |
| `CSRF_MODE` | `enforce` | CSRF 3-state mode |
| `WS_ORIGIN_MODE` | `enforce` | WebSocket Origin 3-state mode |
| `STREAM_LIMIT_MODE` | `enforce` | Stream limiter 3-state mode |
| `GLOBAL_STREAM_LIMIT` | `10` | Max concurrent streams |
| `PER_SESSION_STREAM_LIMIT` | `2` | Max streams per session |
| `TLS_ENABLED` | `false` | Enable TLS termination |
| `TLS_CERT_PATH` | `/certs/localhost.crt` | TLS certificate path |
| `TLS_KEY_PATH` | `/certs/localhost.key` | TLS private key path |
