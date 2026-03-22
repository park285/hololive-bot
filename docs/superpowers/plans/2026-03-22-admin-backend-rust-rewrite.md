# Admin Dashboard Backend Rust Rewrite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Go admin dashboard backend with a Rust (axum) implementation, fixing known bugs and adding performance optimizations.

**Architecture:** axum + tokio + tower middleware stack. AppState shared via `Arc`. Valkey for sessions (deadpool-redis), bollard for Docker, hyper for H2C reverse proxy. Single binary with embedded frontend via rust-embed.

**Tech Stack:** Rust, axum, tokio, tower-http, deadpool-redis, bollard, hyper/hyper-util, serde, rust-embed, sysinfo, utoipa, tracing

**Spec:** `docs/superpowers/specs/2026-03-22-admin-backend-rust-rewrite-design.md`

---

## Task 1: Project Scaffolding

**Files:**
- Create: `admin-dashboard/backend-rs/Cargo.toml`
- Create: `admin-dashboard/backend-rs/src/main.rs`
- Create: `admin-dashboard/backend-rs/static/dist/.gitkeep`

- [ ] **Step 1: Create Cargo.toml with all dependencies**

```toml
[package]
name = "admin-dashboard"
version = "0.1.0"
edition = "2024"

[dependencies]
# Web framework
axum = { version = "0.8", features = ["ws", "macros"] }
axum-extra = { version = "0.10", features = ["cookie", "typed-header"] }
axum-server = { version = "0.7", features = ["tls-rustls"] }
tokio = { version = "1", features = ["full"] }
tower = { version = "0.5", features = ["util"] }
tower-http = { version = "0.6", features = ["cors", "set-header", "trace"] }

# Session store
deadpool-redis = "0.18"
redis = { version = "0.27", features = ["tokio-comp"] }

# Docker
bollard = "0.18"

# Proxy
hyper = { version = "1", features = ["full"] }
hyper-util = { version = "0.1", features = ["client-legacy", "http1", "http2", "tokio"] }
http-body-util = "0.1"

# Auth
hmac = "0.12"
sha2 = "0.10"
bcrypt = "0.17"
rand = "0.9"
hex = "0.4"
base64 = "0.22"

# JSON
serde = { version = "1", features = ["derive"] }
serde_json = "1"

# Config
dotenvy = "0.15"

# Static files
rust-embed = { version = "8", features = ["compression"] }

# System stats
sysinfo = "0.33"

# OpenAPI
utoipa = { version = "5", features = ["axum_extras"] }
utoipa-swagger-ui = { version = "8", features = ["axum"] }

# Logging
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter", "json"] }

# Error handling
thiserror = "2"
anyhow = "1"

# HTTP client (status collector, SSR)
reqwest = { version = "0.12", features = ["json"] }

# Async trait
async-trait = "0.1"

# Misc
chrono = { version = "0.4", features = ["serde"] }
tokio-util = { version = "0.7", features = ["rt"] }
xxhash-rust = { version = "0.8", features = ["xxh3"] }
mime_guess = "2"
pin-project-lite = "0.2"

[dev-dependencies]
tower = { version = "0.5", features = ["util"] }
http-body-util = "0.1"
mockall = "0.13"

[profile.release]
lto = true
codegen-units = 1
strip = true
```

- [ ] **Step 2: Create minimal main.rs**

```rust
fn main() {
    println!("admin-dashboard placeholder");
}
```

- [ ] **Step 3: Create static directory placeholder**

```bash
mkdir -p admin-dashboard/backend-rs/static/dist
touch admin-dashboard/backend-rs/static/dist/.gitkeep
```

- [ ] **Step 4: Verify build**

Run: `cd admin-dashboard/backend-rs && cargo check`
Expected: compiles successfully

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): scaffold Rust backend project
```

---

## Task 2: Config Module

**Files:**
- Create: `admin-dashboard/backend-rs/src/config.rs`

- [ ] **Step 1: Write config tests**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_security_mode_enforce() {
        assert_eq!(SecurityMode::parse("enforce"), SecurityMode::Enforce);
    }

    #[test]
    fn test_parse_security_mode_monitor() {
        assert_eq!(SecurityMode::parse("monitor"), SecurityMode::Monitor);
    }

    #[test]
    fn test_parse_security_mode_off() {
        assert_eq!(SecurityMode::parse("off"), SecurityMode::Off);
    }

    #[test]
    fn test_parse_security_mode_unknown_defaults_enforce() {
        assert_eq!(SecurityMode::parse("invalid"), SecurityMode::Enforce);
        assert_eq!(SecurityMode::parse(""), SecurityMode::Enforce);
    }

    #[test]
    fn test_parse_security_mode_case_insensitive() {
        assert_eq!(SecurityMode::parse("ENFORCE"), SecurityMode::Enforce);
        assert_eq!(SecurityMode::parse("Monitor"), SecurityMode::Monitor);
    }

    #[test]
    fn test_normalize_origin() {
        assert_eq!(normalize_origin("  https://example.com/  "), "https://example.com");
        assert_eq!(normalize_origin("https://example.com"), "https://example.com");
    }

    #[test]
    fn test_is_localhost_origin() {
        assert!(is_localhost_origin("http://localhost:5173"));
        assert!(is_localhost_origin("http://127.0.0.1:3000"));
        assert!(is_localhost_origin("http://[::1]:3000"));
        assert!(!is_localhost_origin("https://admin.capu.blog"));
    }

    #[test]
    fn test_session_config_defaults() {
        let cfg = SessionConfig::default();
        assert_eq!(cfg.expiry_duration, std::time::Duration::from_secs(30 * 60));
        assert_eq!(cfg.absolute_timeout, std::time::Duration::from_secs(8 * 3600));
        assert_eq!(cfg.idle_session_ttl, std::time::Duration::from_secs(10));
        assert_eq!(cfg.grace_period, std::time::Duration::from_secs(30));
        assert_eq!(cfg.rotation_interval, std::time::Duration::from_secs(15 * 60));
    }

    // Env var alias priority tests (spec Section 12)
    #[test]
    fn test_admin_pass_hash_alias_first_non_empty_wins() {
        // ADMIN_PASS_HASH set → use it, ignore ADMIN_PASS_BCRYPT
        // Only ADMIN_PASS_BCRYPT set → use it
        // Neither set → panic (required)
    }

    #[test]
    fn test_session_secret_alias_first_non_empty_wins() {
        // SESSION_SECRET set → use it, ignore ADMIN_SECRET_KEY
        // Only ADMIN_SECRET_KEY set → use it
        // Neither set → panic (required)
    }

    #[test]
    fn test_production_filters_localhost_origins() {
        // ENV=production, ALLOWED_ORIGINS includes localhost → filtered out
        // ALLOW_LOCALHOST_IN_PROD=true → kept
    }

    #[test]
    fn test_fallback_origins_used_when_env_not_set() {
        // ALLOWED_ORIGINS not set → fallback list used + warning logged
    }
}
```

- [ ] **Step 2: Implement Config, SecurityConfig, SessionConfig**

Full implementation:
- `Config` struct with all env vars from spec Section 12
- `SecurityConfig` with 3-state modes, origin parsing, localhost filtering
- `SessionConfig` with hardcoded constants per spec Section 7.1
- `SecurityMode` enum with `parse()` method
- Helper functions: `env_string`, `env_bool`, `env_i64`, `env_int`
- `parse_allowed_origins()` with fallback + prod localhost filtering
- `normalize_origin()`, `is_localhost_origin()`, `filter_localhost_origins()`

Key: `Config::load()` reads all env vars with defaults. Required vars (`ADMIN_PASS_HASH`, `SESSION_SECRET`) panic if missing.

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test config`
Expected: all tests pass

- [ ] **Step 4: Wire into main.rs**

```rust
mod config;

fn main() {
    dotenvy::dotenv().ok();
    let _cfg = config::Config::load();
    println!("config loaded");
}
```

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): add config module with env parsing and security modes
```

---

## Task 3: Error Types

**Files:**
- Create: `admin-dashboard/backend-rs/src/error.rs`

- [ ] **Step 1: Define error types with IntoResponse**

```rust
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde_json::json;

#[derive(Debug, thiserror::Error)]
pub enum AppError {
    #[error(transparent)]
    Auth(#[from] AuthError),
    #[error(transparent)]
    Docker(#[from] DockerError),
    #[error(transparent)]
    Proxy(#[from] ProxyError),
    #[error("internal error: {0}")]
    Internal(#[from] anyhow::Error),
}

#[derive(Debug, thiserror::Error)]
pub enum AuthError {
    #[error("unauthorized")]
    Unauthorized,
    #[error("session expired")]
    SessionExpired,
    #[error("session absolute expired")]
    AbsoluteExpired,
    #[error("rate limited")]
    RateLimited { retry_after_secs: u64 },
    #[error("csrf violation")]
    CsrfViolation,
    #[error("session store unavailable")]
    StoreUnavailable,
}

#[derive(Debug, thiserror::Error)]
pub enum DockerError {
    #[error("docker unavailable")]
    Unavailable,
    #[error("container not managed: {0}")]
    NotManaged(String),
    #[error("docker error: {0}")]
    Internal(String),
}

#[derive(Debug, thiserror::Error)]
pub enum ProxyError {
    #[error("upstream unavailable")]
    Unavailable,
    #[error("websocket upstream unavailable")]
    WsUnavailable,
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, body) = match &self {
            AppError::Auth(e) => match e {
                AuthError::Unauthorized | AuthError::SessionExpired => {
                    (StatusCode::UNAUTHORIZED, json!({"error": "Unauthorized"}))
                }
                AuthError::AbsoluteExpired => {
                    (StatusCode::UNAUTHORIZED, json!({"error": "Session expired", "absolute_expired": true}))
                }
                AuthError::RateLimited { retry_after_secs } => {
                    (StatusCode::TOO_MANY_REQUESTS, json!({"error": "Too many login attempts", "retry_after": retry_after_secs}))
                }
                AuthError::CsrfViolation => {
                    (StatusCode::FORBIDDEN, json!({"error": "Forbidden"}))
                }
                AuthError::StoreUnavailable => {
                    (StatusCode::SERVICE_UNAVAILABLE, json!({"error": "Session store unavailable"}))
                }
            },
            AppError::Docker(e) => match e {
                DockerError::Unavailable => {
                    (StatusCode::SERVICE_UNAVAILABLE, json!({"error": "Docker service not available"}))
                }
                DockerError::NotManaged(name) => {
                    (StatusCode::NOT_FOUND, json!({"error": "container not found"}))
                }
                DockerError::Internal(_) => {
                    (StatusCode::INTERNAL_SERVER_ERROR, json!({"error": "An internal error occurred"}))
                }
            },
            AppError::Proxy(e) => match e {
                ProxyError::Unavailable => {
                    (StatusCode::BAD_GATEWAY, json!({"error": "Service unavailable"}))
                }
                ProxyError::WsUnavailable => {
                    (StatusCode::BAD_GATEWAY, json!({"error": "WebSocket service unavailable"}))
                }
            },
            AppError::Internal(e) => {
                tracing::error!(error = %e, "internal error");
                (StatusCode::INTERNAL_SERVER_ERROR, json!({"error": "An internal error occurred"}))
            }
        };
        (status, Json(body)).into_response()
    }
}
```

- [ ] **Step 2: Write test for IntoResponse**

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::StatusCode;

    #[test]
    fn test_auth_error_unauthorized_status() {
        let err = AppError::Auth(AuthError::Unauthorized);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    }

    #[test]
    fn test_rate_limited_status() {
        let err = AppError::Auth(AuthError::RateLimited { retry_after_secs: 900 });
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::TOO_MANY_REQUESTS);
    }

    #[test]
    fn test_docker_not_managed_status() {
        let err = AppError::Docker(DockerError::NotManaged("foo".into()));
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }

    #[test]
    fn test_proxy_unavailable_status() {
        let err = AppError::Proxy(ProxyError::Unavailable);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::BAD_GATEWAY);
    }
}
```

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test error`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add domain error types with IntoResponse
```

---

## Task 4: Auth — HMAC Signing

**Files:**
- Create: `admin-dashboard/backend-rs/src/auth/mod.rs`
- Create: `admin-dashboard/backend-rs/src/auth/hmac.rs`

- [ ] **Step 1: Write HMAC tests**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_sign_and_validate_roundtrip() {
        let session_id = "abcdef1234567890";
        let secret = "test-secret-key";
        let signed = sign_session_id(session_id, secret);

        assert!(signed.contains('.'));
        let (extracted_id, valid) = validate_session_signature(&signed, secret);
        assert!(valid);
        assert_eq!(extracted_id, session_id);
    }

    #[test]
    fn test_validate_wrong_secret() {
        let signed = sign_session_id("session123", "secret1");
        let (_, valid) = validate_session_signature(&signed, "secret2");
        assert!(!valid);
    }

    #[test]
    fn test_validate_tampered_signature() {
        let signed = sign_session_id("session123", "secret");
        let tampered = format!("session123.{}", "invalid_sig");
        let (_, valid) = validate_session_signature(&tampered, "secret");
        assert!(!valid);
    }

    #[test]
    fn test_validate_no_dot() {
        let (_, valid) = validate_session_signature("noseparator", "secret");
        assert!(!valid);
    }

    #[test]
    #[should_panic]
    fn test_sign_empty_secret_panics() {
        // Secret is required per spec Section 12 — empty secret must not be allowed
        sign_session_id("session123", "");
    }

    #[test]
    fn test_generate_session_id_length() {
        let id = generate_session_id();
        assert_eq!(id.len(), 64); // 32 bytes hex-encoded
    }

    #[test]
    fn test_generate_session_id_uniqueness() {
        let id1 = generate_session_id();
        let id2 = generate_session_id();
        assert_ne!(id1, id2);
    }

    #[test]
    fn test_truncate_session_id() {
        assert_eq!(truncate_session_id("abcdef1234567890"), "abcdef12...");
        assert_eq!(truncate_session_id("short"), "short");
    }
}
```

- [ ] **Step 2: Implement HMAC module**

```rust
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use hmac::{Hmac, Mac};
use rand::RngCore;
use sha2::Sha256;

type HmacSha256 = Hmac<Sha256>;

pub fn sign_session_id(session_id: &str, secret: &str) -> String {
    assert!(!secret.is_empty(), "session secret must not be empty");
    let mut mac = HmacSha256::new_from_slice(secret.as_bytes())
        .expect("HMAC accepts any key length");
    mac.update(session_id.as_bytes());
    let signature = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());
    format!("{session_id}.{signature}")
}

pub fn validate_session_signature(full_id: &str, secret: &str) -> (String, bool) {
    assert!(!secret.is_empty(), "session secret must not be empty");
    let Some((session_id, provided_sig)) = full_id.split_once('.') else {
        return (String::new(), false);
    };

    let mut mac = HmacSha256::new_from_slice(secret.as_bytes())
        .expect("HMAC accepts any key length");
    mac.update(session_id.as_bytes());
    let expected_sig = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());

    if !constant_time_eq(provided_sig.as_bytes(), expected_sig.as_bytes()) {
        return (String::new(), false);
    }
    (session_id.to_string(), true)
}

pub fn generate_session_id() -> String {
    let mut bytes = [0u8; 32];
    rand::rng().fill_bytes(&mut bytes);
    hex::encode(bytes)
}

pub fn truncate_session_id(session_id: &str) -> String {
    if session_id.len() <= 8 {
        return session_id.to_string();
    }
    format!("{}...", &session_id[..8])
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    a.iter().zip(b.iter()).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}
```

- [ ] **Step 3: Create auth/mod.rs**

```rust
pub mod hmac;
pub use hmac::{sign_session_id, validate_session_signature, generate_session_id, truncate_session_id};
```

- [ ] **Step 4: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test auth::hmac`
Expected: all pass

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): add HMAC session signing and verification
```

---

## Task 5: Auth — Session Store

**Files:**
- Create: `admin-dashboard/backend-rs/src/auth/session.rs`

- [ ] **Step 1: Define SessionProvider trait and Session struct**

```rust
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::time::Duration;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    pub id: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub absolute_expires_at: DateTime<Utc>,
    #[serde(default)]
    #[serde(default = "Utc::now")]
    pub last_rotated_at: DateTime<Utc>,
}

#[async_trait::async_trait]
pub trait SessionProvider: Send + Sync {
    async fn create_session(&self) -> Result<Session, anyhow::Error>;
    async fn get_session(&self, session_id: &str) -> Result<Option<Session>, anyhow::Error>;
    async fn validate_session(&self, session_id: &str) -> bool;
    async fn delete_session(&self, session_id: &str);
    async fn refresh_session_with_validation(
        &self, session_id: &str, idle: bool,
    ) -> Result<(bool, bool), anyhow::Error>; // (refreshed, absolute_expired)
    async fn rotate_session(&self, old_session_id: &str) -> Result<Option<Session>, anyhow::Error>;
}
```

- [ ] **Step 2: Implement ValkeySessionStore**

Key implementation details:
- `deadpool_redis::Pool` for connection pooling
- Key format: `session:admin:<id>`
- `create_session`: generate ID, serialize Session JSON, SET with EX
- `get_session`: GET + deserialize, check absolute expiry
- `validate_session`: get + check absolute_expires_at
- `delete_session`: DEL with timeout context
- `refresh_session_with_validation`: idle → short TTL, normal → full TTL refresh, absolute check
- `rotate_session`: atomic via Lua script (GET old → generate new → SET new → EXPIRE old grace period → DEL old after grace)

Lua script for atomic rotation:
```lua
local old_key = KEYS[1]
local new_key = KEYS[2]
local new_data = ARGV[1]
local new_ttl = tonumber(ARGV[2])
local grace_ttl = tonumber(ARGV[3])
local old_data = redis.call('GET', old_key)
if not old_data then return nil end
redis.call('SET', new_key, new_data, 'EX', new_ttl)
redis.call('EXPIRE', old_key, grace_ttl)
return old_data
```

- [ ] **Step 3: Write unit tests (with mock Redis or testing against real behavior)**

Table-driven tests for:
- Session creation and retrieval roundtrip
- Absolute timeout detection and auto-delete
- Idle TTL application
- Rotation interval check (skip if too recent)
- Grace period on old session after rotation

Note: Full integration tests with real Valkey in Task 20.

- [ ] **Step 4: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test auth::session`
Expected: all pass

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): add Valkey session store with atomic rotation
```

---

## Task 6: Auth — Rate Limiter

**Files:**
- Create: `admin-dashboard/backend-rs/src/auth/rate_limiter.rs`

- [ ] **Step 1: Write rate limiter tests**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_first_attempt_allowed() {
        let rl = LoginRateLimiter::new();
        let (allowed, _) = rl.is_allowed("1.2.3.4");
        assert!(allowed);
    }

    #[tokio::test]
    async fn test_lockout_after_max_failures() {
        let rl = LoginRateLimiter::new();
        for _ in 0..5 {
            rl.record_failure("1.2.3.4");
        }
        let (allowed, remaining) = rl.is_allowed("1.2.3.4");
        assert!(!allowed);
        assert!(remaining > Duration::ZERO);
    }

    #[tokio::test]
    async fn test_success_resets_count() {
        let rl = LoginRateLimiter::new();
        rl.record_failure("1.2.3.4");
        rl.record_failure("1.2.3.4");
        rl.record_success("1.2.3.4");
        let (allowed, _) = rl.is_allowed("1.2.3.4");
        assert!(allowed);
    }

    #[tokio::test]
    async fn test_different_ips_independent() {
        let rl = LoginRateLimiter::new();
        for _ in 0..5 {
            rl.record_failure("1.2.3.4");
        }
        let (allowed, _) = rl.is_allowed("5.6.7.8");
        assert!(allowed);
    }

    #[tokio::test]
    async fn test_record_failure_returns_count() {
        let rl = LoginRateLimiter::new();
        assert_eq!(rl.record_failure("1.2.3.4"), 1);
        assert_eq!(rl.record_failure("1.2.3.4"), 2);
    }
}
```

- [ ] **Step 2: Implement LoginRateLimiter**

```rust
use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tokio_util::sync::CancellationToken;

struct AttemptInfo {
    count: usize,
    first_attempt: Instant,
    locked_until: Option<Instant>,
}

pub struct LoginRateLimiter {
    attempts: Mutex<HashMap<String, AttemptInfo>>,
    max_attempts: usize,    // 5
    window: Duration,       // 5 minutes
    lockout: Duration,      // 15 minutes
    cancel: CancellationToken,
}

impl LoginRateLimiter {
    pub fn new() -> Self { /* ... */ }
    pub fn is_allowed(&self, ip: &str) -> (bool, Duration) { /* ... */ }
    pub fn record_failure(&self, ip: &str) -> usize { /* ... */ }
    pub fn record_success(&self, ip: &str) { /* ... */ }
    pub fn start_cleanup_task(&self) { /* tokio::spawn with CancellationToken */ }
    pub fn shutdown(&self) { self.cancel.cancel(); }
}
```

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test auth::rate_limiter`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add login rate limiter with tokio cleanup
```

---

## Task 7: Auth — Middleware + Security Headers

**Files:**
- Create: `admin-dashboard/backend-rs/src/auth/middleware.rs`

- [ ] **Step 1: Implement auth middleware as axum extractor or tower layer**

```rust
use axum::{extract::Request, middleware::Next, response::Response};

/// AuthMiddleware: validates admin_session cookie
pub async fn auth_middleware(
    State(state): State<Arc<AppState>>,
    mut req: Request,
    next: Next,
) -> Result<Response, AppError> {
    let session_cookie = extract_cookie(&req, "admin_session")
        .ok_or(AuthError::Unauthorized)?;

    let (session_id, valid) = validate_session_signature(&session_cookie, &state.config.admin_secret_key);
    if !valid {
        return Err(AuthError::Unauthorized.into());
    }

    if !state.sessions.validate_session(&session_id).await {
        // Clear cookie on the RESPONSE, not request
        let mut response = StatusCode::UNAUTHORIZED.into_response();
        set_clear_cookie(response.headers_mut(), "admin_session", state.config.force_https);
        return Ok(response);
    }

    // Store session_id in request extensions for handlers
    req.extensions_mut().insert(SessionId(session_id));
    Ok(next.run(req).await)
}
```

- [ ] **Step 2: Implement security headers layer**

```rust
pub fn security_headers_layer(force_https: bool) -> tower::util::MapResponseLayer<...> {
    // Pre-build all HeaderValue instances at construction time
    // X-Content-Type-Options: nosniff
    // X-Frame-Options: DENY
    // X-XSS-Protection: 1; mode=block
    // Referrer-Policy: strict-origin-when-cross-origin
    // Strict-Transport-Security (if HTTPS)
    // Content-Security-Policy (without Report-Only — bug fix)
}
```

- [ ] **Step 3: Write tests**

Test auth middleware with:
- Missing cookie → 401
- Invalid HMAC signature → 401
- Expired session → 401 + cookie cleared on RESPONSE (Set-Cookie with Max-Age=-1)
- Valid session → passes through, SessionId in extensions

Test cookie attributes (spec 7.1):
- `admin_session` cookie: HttpOnly=true, SameSite=Strict, Path=/, Secure when FORCE_HTTPS
- `csrf_token` cookie: HttpOnly=false, SameSite=Strict, Path=/
- Cookie delete: same attributes with Max-Age=-1

Test security headers:
- All required headers present (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection, Referrer-Policy, CSP)
- HSTS only when force_https=true
- No CSP Report-Only header (bug fix verification)

- [ ] **Step 4: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test auth::middleware`
Expected: all pass

- [ ] **Step 5: Update auth/mod.rs exports**

- [ ] **Step 6: Commit**

```
feat(admin-dashboard): add auth middleware and security headers layer
```

---

## Task 8: CSRF Middleware

**Files:**
- Create: `admin-dashboard/backend-rs/src/middleware/mod.rs`
- Create: `admin-dashboard/backend-rs/src/middleware/csrf.rs`

- [ ] **Step 1: Write CSRF tests**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_csrf_token_format() {
        let token = new_csrf_token("session123", "secret");
        assert!(!token.is_empty());
        let parts: Vec<&str> = token.split('.').collect();
        assert_eq!(parts.len(), 2);
        assert_eq!(parts[0].len(), 64); // 32 bytes hex
    }

    #[test]
    fn test_validate_csrf_token_roundtrip() {
        let token = new_csrf_token("session123", "secret");
        assert!(validate_csrf_token("session123", &token, "secret"));
    }

    #[test]
    fn test_validate_csrf_token_wrong_session() {
        let token = new_csrf_token("session123", "secret");
        assert!(!validate_csrf_token("other_session", &token, "secret"));
    }

    #[test]
    fn test_validate_csrf_token_wrong_secret() {
        let token = new_csrf_token("session123", "secret1");
        assert!(!validate_csrf_token("session123", &token, "secret2"));
    }

    #[test]
    fn test_validate_csrf_token_tampered_nonce() {
        let token = new_csrf_token("session123", "secret");
        let tampered = format!("0000{}", &token[4..]);
        assert!(!validate_csrf_token("session123", &tampered, "secret"));
    }

    #[test]
    fn test_validate_csrf_token_invalid_format() {
        assert!(!validate_csrf_token("session123", "no-dot", "secret"));
        assert!(!validate_csrf_token("session123", "", "secret"));
    }
}
```

- [ ] **Step 2: Implement CSRF token generation and validation**

```rust
pub fn new_csrf_token(session_id: &str, secret: &str) -> String { /* ... */ }
pub fn validate_csrf_token(session_id: &str, token: &str, secret: &str) -> bool { /* ... */ }
pub fn set_csrf_cookie(headers: &mut HeaderMap, token: &str, force_https: bool) { /* ... */ }
pub fn clear_csrf_cookie(headers: &mut HeaderMap, force_https: bool) { /* ... */ }
```

- [ ] **Step 3: Implement CSRF middleware with 3-state mode**

```rust
pub async fn csrf_middleware(
    State(state): State<Arc<AppState>>,
    req: Request,
    next: Next,
) -> Result<Response, AppError> {
    // Skip non-mutating methods
    // Check mode (off → skip, monitor → log only, enforce → reject)
    // Validate: header token == cookie token, verify signature against session
}
```

- [ ] **Step 4: Write CSRF middleware 3-mode integration tests**

```rust
#[cfg(test)]
mod middleware_tests {
    #[tokio::test]
    async fn test_csrf_enforce_missing_header_returns_403() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_enforce_missing_cookie_returns_403() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_enforce_mismatch_returns_403() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_enforce_invalid_session_returns_403() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_enforce_valid_passes() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_monitor_allows_invalid_and_logs() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_off_skips_validation() { /* ... */ }
    #[tokio::test]
    async fn test_csrf_get_request_skips_check() { /* ... */ }
}
```

- [ ] **Step 5: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test middleware::csrf`
Expected: all pass

- [ ] **Step 6: Commit**

```
feat(admin-dashboard): add CSRF middleware with 3-state mode
```

---

## Task 9: ETag Middleware

**Files:**
- Create: `admin-dashboard/backend-rs/src/middleware/etag.rs`

- [ ] **Step 1: Implement buffered ETag middleware (bug fix)**

```rust
/// ETag middleware for API GET responses.
/// Buffers response body, computes xxhash, supports If-None-Match → 304.
/// Only applies to /admin/api/* GET requests (excludes WebSocket, static assets).
///
/// Bug fix: Go version wrote body to client before checking If-None-Match.
/// This version buffers first, then decides whether to send body or 304.
pub async fn etag_middleware(req: Request, next: Next) -> Response {
    // Skip non-GET, non-API paths, WebSocket upgrades
    // Run next handler
    // If status != 200, return as-is
    // Buffer body, compute xxhash (first 8 bytes hex)
    // Check If-None-Match → 304 if match
    // Otherwise set ETag header and return buffered body
}
```

- [ ] **Step 2: Write tests**

- GET /admin/api/status → ETag header present
- Same response + If-None-Match → 304
- POST requests → no ETag
- Non-API paths → no ETag
- Different body → different ETag

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test middleware::etag`
Expected: all pass

- [ ] **Step 4: Commit**

```
fix(admin-dashboard): implement buffered ETag middleware (fixes Go double-write bug)
```

---

## Task 10: Stream Limiter

**Files:**
- Create: `admin-dashboard/backend-rs/src/stream_limiter.rs`

- [ ] **Step 1: Write stream limiter tests**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_acquire_and_release() {
        let limiter = StreamLimiter::new(10, 2, SecurityMode::Enforce);
        let (allowed, _) = limiter.try_acquire("session1");
        assert!(allowed);
        limiter.release("session1");
    }

    #[test]
    fn test_global_limit_enforced() {
        let limiter = StreamLimiter::new(2, 10, SecurityMode::Enforce);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s2").0);
        assert!(!limiter.try_acquire("s3").0); // global limit hit
    }

    #[test]
    fn test_per_session_limit_enforced() {
        let limiter = StreamLimiter::new(10, 2, SecurityMode::Enforce);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s1").0);
        let (allowed, result) = limiter.try_acquire("s1");
        assert!(!allowed);
        assert!(result.per_session_hit);
    }

    #[test]
    fn test_monitor_mode_allows_over_limit() {
        let limiter = StreamLimiter::new(1, 1, SecurityMode::Monitor);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s1").0); // over limit but monitor mode
    }

    #[test]
    fn test_off_mode_no_tracking() {
        let limiter = StreamLimiter::new(1, 1, SecurityMode::Off);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s1").0);
    }

    #[test]
    fn test_release_frees_slot() {
        let limiter = StreamLimiter::new(1, 1, SecurityMode::Enforce);
        assert!(limiter.try_acquire("s1").0);
        limiter.release("s1");
        assert!(limiter.try_acquire("s1").0);
    }

    #[test]
    fn test_stats() {
        let limiter = StreamLimiter::new(10, 2, SecurityMode::Enforce);
        limiter.try_acquire("s1");
        limiter.try_acquire("s2");
        let (global, limit, sessions) = limiter.stats();
        assert_eq!(global, 2);
        assert_eq!(limit, 10);
        assert_eq!(sessions, 2);
    }
}
```

- [ ] **Step 2: Implement StreamLimiter**

Uses `tokio::sync::Semaphore` for global limit + `Mutex<HashMap<String, usize>>` for per-session.

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test stream_limiter`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add stream limiter with semaphore-based global limit
```

---

## Task 11: Docker Service

**Files:**
- Create: `admin-dashboard/backend-rs/src/docker/mod.rs`
- Create: `admin-dashboard/backend-rs/src/docker/service.rs`

- [ ] **Step 1: Define DockerProvider trait**

```rust
#[async_trait::async_trait]
pub trait DockerProvider: Send + Sync {
    async fn available(&self) -> bool;
    async fn list_containers(&self) -> Result<Vec<Container>, DockerError>;
    async fn restart_container(&self, name: &str) -> Result<(), DockerError>;
    async fn stop_container(&self, name: &str) -> Result<(), DockerError>;
    async fn start_container(&self, name: &str) -> Result<(), DockerError>;
    async fn get_log_stream(&self, name: &str) -> Result<std::pin::Pin<Box<dyn tokio::io::AsyncRead + Send + Unpin>>, DockerError>;
    fn is_managed(&self, name: &str) -> bool;
}
```

- [ ] **Step 2: Implement DockerService with bollard**

Key details:
- `bollard::Docker::connect_with_http()` for TCP docker-proxy
- `managed_filters`: `["hololive", "valkey", "postgres", "deunhealth"]`
- `exclude_filters`: `["-init"]`
- 5-second TTL cache for `list_containers` via `RwLock<(Instant, Vec<Container>)>`
- Container struct with all fields from Go spec
- Health parsing from status string

- [ ] **Step 3: Write tests with mockall**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_managed() {
        let svc = DockerService::new_test();
        assert!(svc.is_managed("hololive-kakao-bot-go"));
        assert!(svc.is_managed("valkey-cache"));
        assert!(!svc.is_managed("random-container"));
        assert!(!svc.is_managed("hololive-init")); // exclude filter
    }
}
```

- [ ] **Step 4: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test docker`
Expected: all pass

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): add Docker service with bollard and TTL cache
```

---

## Task 12: Static File Serving

**Files:**
- Create: `admin-dashboard/backend-rs/src/static_files.rs`

- [ ] **Step 1: Implement rust-embed static file serving**

```rust
use rust_embed::Embed;

#[derive(Embed)]
#[folder = "static/dist/"]
struct StaticAssets;

pub fn has_embedded() -> bool { /* check if index.html exists */ }
pub fn index_html() -> Option<Vec<u8>> { /* ... */ }
pub fn favicon() -> Option<Vec<u8>> { /* ... */ }

pub async fn serve_static(uri: axum::http::Uri) -> impl IntoResponse {
    // Serve from embedded assets
    // Set appropriate Content-Type via mime_guess
    // Cache-Control: immutable for /assets/*, no-cache for HTML
}

pub async fn serve_favicon() -> impl IntoResponse {
    // Cache-Control: public, max-age=86400
}
```

- [ ] **Step 2: Write tests**

- `has_embedded()` returns false in dev (no dist), true in production build
- `serve_static` returns 404 for unknown paths
- Content-Type detection works

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test static_files`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add rust-embed static file serving
```

---

## Task 13: SSR Injector

**Files:**
- Create: `admin-dashboard/backend-rs/src/ssr/mod.rs`
- Create: `admin-dashboard/backend-rs/src/ssr/injector.rs`

- [ ] **Step 1: Implement SsrInjector**

Key details:
- `reqwest::Client` shared instance for upstream fetches
- `html_cache: Vec<u8>` loaded from embedded or filesystem
- `inject_for_path()`: check auth → fetch data → inject `<script>` before `</head>`
- Body size limit: `response.bytes().await` with Content-Length check, max 2MB
- `serde_json` auto-escapes `</` — no manual XSS defense needed
- Fallback chain: injection failure → cached HTML → embedded index.html

```rust
pub struct SsrInjector {
    docker_svc: Option<Arc<dyn DockerProvider>>,
    holo_bot_url: String,
    html_cache: Vec<u8>,
    http_client: reqwest::Client,
}
```

- [ ] **Step 2: Write tests**

- Unauthenticated → returns cached HTML without injection
- Authenticated + `/dashboard/members` → injects members data
- Upstream returns >2MB → skips injection, returns cached HTML
- Upstream returns error → returns cached HTML (fallback)
- XSS: verify `</script>` in upstream data is safe (serde_json test)
- `</head>` injection point found correctly

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test ssr`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add SSR injector with size-limited upstream fetch
```

---

## Task 14: Status Collector + System Stats

**Files:**
- Create: `admin-dashboard/backend-rs/src/status/mod.rs`
- Create: `admin-dashboard/backend-rs/src/status/collector.rs`
- Create: `admin-dashboard/backend-rs/src/status/system_stats.rs`

- [ ] **Step 1: Implement StatusCollector**

```rust
pub struct StatusCollector {
    http_client: reqwest::Client,  // shared, 3s timeout
    endpoints: Vec<ServiceEndpoint>,
    start_time: Instant,
    version: String,
}
```

- Parallel health checks via `tokio::join!` / `futures::future::join_all`
- Partial degradation: failed endpoints report `available: false`
- Admin dashboard always reports self as available

- [ ] **Step 2: Implement SystemStats with broadcast**

```rust
pub struct SystemStatsCollector {
    tx: broadcast::Sender<SystemStats>,
}

impl SystemStatsCollector {
    pub fn start(cancel: CancellationToken) -> broadcast::Sender<SystemStats> {
        // Single background task, polls sysinfo every 2 seconds
        // Broadcasts to all subscribers
        // Stops on CancellationToken
    }
}
```

- [ ] **Step 3: Write tests**

- Collector with no endpoints → only admin-dashboard in response
- Format duration correctly
- SystemStats broadcast: subscriber receives updates

- [ ] **Step 4: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test status`
Expected: all pass

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): add status collector with broadcast system stats
```

---

## Task 15: Bot Proxy (H2C + WebSocket)

**Files:**
- Create: `admin-dashboard/backend-rs/src/proxy/mod.rs`
- Create: `admin-dashboard/backend-rs/src/proxy/bot_proxy.rs`

- [ ] **Step 1: Implement BotProxy**

```rust
pub struct BotProxy {
    h2c_client: hyper_util::client::legacy::Client<...>,  // H2C transport
    http11_client: hyper_util::client::legacy::Client<...>, // HTTP/1.1 for WS
    target: Uri,
    api_key: Option<String>,
}
```

Key details:
- Path rewrite: `/admin/api/holo/<path>` → `/api/holo/<path>`
- Header injection: `X-API-Key` if configured
- Header removal: `Origin` header
- WS detection: `Upgrade: websocket` header → use HTTP/1.1 client
- Non-WS: use H2C client
- Error handling: HTTP → 502 "Service unavailable", WS → 502 "WebSocket service unavailable"
- Upstream 404 → log warning, forward as-is

- [ ] **Step 2: Write tests**

- Path rewrite correctness
- X-API-Key injection when configured
- Origin header removed
- WS request uses HTTP/1.1 transport
- Non-WS request uses H2C transport
- Upstream error → 502

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test proxy`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add H2C/WS reverse proxy for holo-bot
```

---

## Task 16: Handlers — Auth

**Files:**
- Create: `admin-dashboard/backend-rs/src/handlers/mod.rs`
- Create: `admin-dashboard/backend-rs/src/handlers/auth.rs`

- [ ] **Step 1: Implement login handler**

```rust
pub async fn handle_login(
    State(state): State<Arc<AppState>>,
    ConnectInfo(addr): ConnectInfo<SocketAddr>,
    Json(req): Json<LoginRequest>,
) -> Result<impl IntoResponse, AppError> {
    // 1. Rate limit check → 429 with Retry-After
    // 2. Validate username
    // 3. bcrypt verify password
    // 4. On failure: record_failure, progressive delay (min(count*500ms, 3s)), return 401
    // 5. On success: create session, sign session ID, set cookie
    // 6. Generate CSRF token, set CSRF cookie
    // 7. Return 200 with csrf_token in body
}
```

- [ ] **Step 2: Implement logout handler**

```rust
pub async fn handle_logout(
    State(state): State<Arc<AppState>>,
    // SessionId from auth middleware extension
) -> impl IntoResponse {
    // Delete session from Valkey
    // Clear session cookie
    // Clear CSRF cookie
    // Return 200
}
```

- [ ] **Step 3: Implement heartbeat handler**

```rust
pub async fn handle_heartbeat(
    State(state): State<Arc<AppState>>,
    // SessionId from auth middleware extension
    Json(req): Json<HeartbeatRequest>,
) -> Result<impl IntoResponse, AppError> {
    // Per spec Section 7.7:
    // 1. refresh_session_with_validation(session_id, idle)
    // 2. If absolute_expired → 401 + clear cookies
    // 3. If idle && !refreshed → {"status": "idle", "idle_rejected": true}
    // 4. If !refreshed → 401 + clear cookies
    // 5. If rotation enabled → rotate_session, new CSRF token, set cookies
    // 6. Return response per contract
}
```

- [ ] **Step 4: Write tests**

- Login success flow: cookie set, CSRF token returned
- Login failure: 401 (not 200 — bug fix), progressive delay
- Login rate limit: 429 after 5 failures
- Logout: session deleted, cookies cleared
- Heartbeat normal: 200 ok
- Heartbeat with rotation: rotated=true, new csrf_token
- Heartbeat absolute expired: 401
- Heartbeat idle: idle_rejected response

- [ ] **Step 5: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test handlers::auth`
Expected: all pass

- [ ] **Step 6: Commit**

```
feat(admin-dashboard): add auth handlers (login, logout, heartbeat)
```

---

## Task 17: Handlers — Docker

**Files:**
- Create: `admin-dashboard/backend-rs/src/handlers/docker.rs`

- [ ] **Step 1: Implement Docker handlers**

```rust
pub async fn handle_docker_health(State(state): State<Arc<AppState>>) -> impl IntoResponse { /* ... */ }
pub async fn handle_docker_containers(State(state): State<Arc<AppState>>) -> Result<impl IntoResponse, AppError> { /* ... */ }
pub async fn handle_docker_restart(State(state): State<Arc<AppState>>, Path(name): Path<String>) -> Result<impl IntoResponse, AppError> { /* ... */ }
pub async fn handle_docker_stop(/* ... */) -> Result<impl IntoResponse, AppError> { /* ... */ }
pub async fn handle_docker_start(/* ... */) -> Result<impl IntoResponse, AppError> { /* ... */ }
pub async fn handle_docker_log_stream(/* ... WebSocket upgrade */) -> Result<impl IntoResponse, AppError> {
    // Extract session ID for per-session limiting
    // StreamLimiter::try_acquire
    // WebSocket upgrade
    // 10-minute timeout
    // Docker multiplexed log stream → WS text frames
    // defer: release stream slot
}
```

- [ ] **Step 2: Write tests with mock DockerProvider**

- health: available=true/false
- containers: returns list
- restart/stop/start: managed → 200, unmanaged → 404
- Docker unavailable → 503

- [ ] **Step 3: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test handlers::docker`
Expected: all pass

- [ ] **Step 4: Commit**

```
feat(admin-dashboard): add Docker management handlers with log streaming
```

---

## Task 18: Handlers — Status

**Files:**
- Create: `admin-dashboard/backend-rs/src/handlers/status.rs`

- [ ] **Step 1: Implement status handlers**

```rust
pub async fn handle_aggregated_status(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    // Collect from StatusCollector
    // Return aggregated status JSON
}

pub async fn handle_system_stats_stream(
    State(state): State<Arc<AppState>>,
    ws: WebSocketUpgrade,
) -> impl IntoResponse {
    // WS Origin verification (3-state mode)
    // Subscribe to broadcast channel
    // Forward stats to WS client
    // Close channel closes the stream (producer sends, no manual close needed)
}
```

- [ ] **Step 2: Implement WS Origin verification helper**

```rust
/// Verify WebSocket Origin header per spec 7.4.
/// Returns Ok(()) if allowed, Err(response) if rejected.
pub fn verify_ws_origin(
    origin: Option<&str>,
    allowed: &HashSet<String>,
    mode: SecurityMode,
) -> Result<(), StatusCode> {
    if mode == SecurityMode::Off { return Ok(()); }

    match origin {
        None => {
            if mode == SecurityMode::Monitor {
                tracing::warn!(mode = "monitor", "ws_origin_missing");
                return Ok(());
            }
            Err(StatusCode::FORBIDDEN)
        }
        Some(o) => {
            if !allowed.contains(o) {
                if mode == SecurityMode::Monitor {
                    tracing::warn!(origin = o, mode = "monitor", "ws_origin_rejected");
                    return Ok(());
                }
                Err(StatusCode::FORBIDDEN)
            } else {
                Ok(())
            }
        }
    }
}
```

- [ ] **Step 3: Write tests**

```rust
// Status tests
#[tokio::test]
async fn test_aggregated_status_includes_self() { /* admin-dashboard always available */ }
#[tokio::test]
async fn test_partial_degradation_one_service_down() { /* one service fails, others OK */ }

// WS Origin tests (spec 7.4)
#[test]
fn test_ws_origin_enforce_valid_allowed() { /* allowed origin → Ok */ }
#[test]
fn test_ws_origin_enforce_invalid_rejected() { /* unknown origin → Forbidden */ }
#[test]
fn test_ws_origin_enforce_missing_rejected() { /* no Origin header → Forbidden */ }
#[test]
fn test_ws_origin_monitor_invalid_allowed_and_logs() { /* unknown but monitor → Ok */ }
#[test]
fn test_ws_origin_off_skips_check() { /* off mode → always Ok */ }
#[test]
fn test_ws_origin_localhost_filtered_in_prod() { /* prod + localhost not in allowed → Forbidden */ }
#[test]
fn test_ws_origin_localhost_allowed_with_flag() { /* ALLOW_LOCALHOST_IN_PROD=true → Ok */ }
```

- [ ] **Step 4: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test handlers::status`
Expected: all pass

- [ ] **Step 5: Commit**

```
feat(admin-dashboard): add status handlers with WS Origin verification
```

---

## Task 19: AppState + Route Assembly

**Files:**
- Create: `admin-dashboard/backend-rs/src/state.rs`
- Modify: `admin-dashboard/backend-rs/src/main.rs`

- [ ] **Step 1: Define AppState**

```rust
pub struct AppState {
    pub config: Config,
    pub security_config: SecurityConfig,
    pub sessions: ValkeySessionStore,
    pub rate_limiter: LoginRateLimiter,
    pub docker_svc: Option<DockerService>,
    pub bot_proxy: Option<BotProxy>,
    pub status_collector: StatusCollector,
    pub ssr_injector: SsrInjector,
    pub stream_limiter: StreamLimiter,
    pub stats_tx: broadcast::Sender<SystemStats>,
}
```

- [ ] **Step 2: Build route tree**

```rust
pub fn build_router(state: Arc<AppState>) -> Router {
    let auth_layer = middleware::from_fn_with_state(state.clone(), auth_middleware);
    let csrf_layer = middleware::from_fn_with_state(state.clone(), csrf_middleware);

    // --- Layer application order (outermost → innermost): ---
    // 1. Security headers (all responses)
    // 2. CORS (all responses)
    // 3. ETag (GET /admin/api/* only, internal filter)
    // 4. Static cache (assets only, internal filter)
    // 5. Auth middleware (authenticated group only)
    // 6. CSRF middleware (POST routes within authenticated group only)

    // Public routes (no auth, no CSRF)
    let public = Router::new()
        .route("/health", get(|| async { Json(json!({"status": "ok"})) }))
        .route("/admin/api/auth/login", post(handlers::auth::handle_login));

    // Authenticated + CSRF routes (POST only)
    let auth_csrf = Router::new()
        .route("/admin/api/auth/logout", post(handlers::auth::handle_logout))
        .route("/admin/api/auth/heartbeat", post(handlers::auth::handle_heartbeat))
        .route("/admin/api/docker/containers/:name/restart", post(handlers::docker::handle_docker_restart))
        .route("/admin/api/docker/containers/:name/stop", post(handlers::docker::handle_docker_stop))
        .route("/admin/api/docker/containers/:name/start", post(handlers::docker::handle_docker_start))
        .layer(csrf_layer);

    // Authenticated GET routes (no CSRF)
    let auth_get = Router::new()
        .route("/admin/api/docker/health", get(handlers::docker::handle_docker_health))
        .route("/admin/api/docker/containers", get(handlers::docker::handle_docker_containers))
        .route("/admin/api/docker/containers/:name/logs/stream", get(handlers::docker::handle_docker_log_stream))
        .route("/admin/api/status", get(handlers::status::handle_aggregated_status))
        .route("/admin/api/ws/system-stats", get(handlers::status::handle_system_stats_stream));

    // Proxy: ANY /admin/api/holo/* (must be BEFORE api_fallback to take priority)
    let proxy = Router::new()
        .route("/admin/api/holo/{*path}", any(proxy::bot_proxy::proxy_holo));

    // Combine all authenticated routes
    let authenticated = Router::new()
        .merge(auth_csrf)
        .merge(auth_get)
        .merge(proxy)
        .layer(auth_layer);

    // API 404 fallback: /admin/api/* that doesn't match → 404 JSON (never SPA HTML)
    let api_fallback = Router::new()
        .route("/admin/api/{*path}", any(|| async {
            (StatusCode::NOT_FOUND, Json(json!({"error": "Not found"})))
        }));

    // SPA fallback (non-API paths)
    let spa = Router::new()
        .route("/", get(serve_with_ssr))
        .route("/assets/{*path}", get(serve_static))
        .route("/favicon.svg", get(serve_favicon))
        .fallback(serve_with_ssr);

    // Compose: specific routes first → API fallback → SPA fallback
    Router::new()
        .merge(public)
        .merge(authenticated)
        .merge(api_fallback)  // catches unmatched /admin/api/* as 404 JSON
        .merge(spa)           // everything else → SPA HTML
        .layer(security_headers_layer(state.config.force_https))
        .layer(cors_layer(&state.security_config))
        .layer(middleware::from_fn(etag_middleware))
        .with_state(state)
}
```

- [ ] **Step 3: Implement main.rs with graceful shutdown**

```rust
#[tokio::main]
async fn main() {
    dotenvy::dotenv().ok();

    // 1. Init tracing (LOG_LEVEL env → EnvFilter)
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_env("LOG_LEVEL"))
        .json()
        .init();

    // 2. Load config (panics if required vars missing)
    let cfg = Config::load();
    let security_cfg = SecurityConfig::load(&cfg);
    tracing::info!(port = %cfg.port, env = %cfg.environment, "starting admin-dashboard");

    // 3. Init Valkey pool (deadpool-redis)
    let pool = deadpool_redis::Config::from_url(&format!("redis://{}", cfg.valkey_url))
        .create_pool(Some(deadpool_redis::Runtime::Tokio1))
        .expect("valkey pool creation failed");

    // 4. Init optional services
    let docker_svc = DockerService::new(&cfg.docker_host).ok();
    let bot_proxy = BotProxy::new(&cfg.holo_bot_url, cfg.holo_bot_api_key.clone()).ok();

    // 5. Init status collector + system stats broadcast
    let status_collector = StatusCollector::new(/* endpoints */, &cfg);
    let (stats_tx, _) = broadcast::channel(16);
    let cancel_token = CancellationToken::new();
    SystemStatsCollector::start(stats_tx.clone(), cancel_token.clone());

    // 6. Init session store, rate limiter, SSR injector, stream limiter
    let sessions = ValkeySessionStore::new(pool);
    let rate_limiter = LoginRateLimiter::new();
    rate_limiter.start_cleanup_task();
    let ssr_injector = SsrInjector::new(docker_svc.clone(), &cfg.holo_bot_url);
    let stream_limiter = StreamLimiter::new(/* from security_cfg */);

    // 7. Build AppState + Router
    let state = Arc::new(AppState { /* all fields */ });
    let router = build_router(state.clone());

    // 8. Bind server (TLS branching per spec 7.9)
    let addr = SocketAddr::from(([0, 0, 0, 0], cfg.port.parse().unwrap()));

    if cfg.tls_enabled {
        // axum_server with tls-rustls
        let tls_config = RustlsConfig::from_pem_file(&cfg.tls_cert_path, &cfg.tls_key_path)
            .await
            .expect("TLS config failed — check TLS_CERT_PATH and TLS_KEY_PATH");
        let handle = axum_server::Handle::new();
        let shutdown_handle = handle.clone();

        tokio::spawn(async move {
            shutdown_signal().await;
            shutdown_handle.graceful_shutdown(Some(Duration::from_secs(30)));
        });

        axum_server::bind_rustls(addr, tls_config)
            .handle(handle)
            .serve(router.into_make_service_with_connect_info::<SocketAddr>())
            .await
            .unwrap();
    } else {
        // Plain HTTP
        let listener = tokio::net::TcpListener::bind(addr).await.unwrap();
        axum::serve(listener, router.into_make_service_with_connect_info::<SocketAddr>())
            .with_graceful_shutdown(shutdown_signal())
            .await
            .unwrap();
    }

    // 9. Cleanup
    cancel_token.cancel(); // stops stats collector + rate limiter cleanup
    tracing::info!("shutdown complete");
}

async fn shutdown_signal() {
    let ctrl_c = tokio::signal::ctrl_c();
    let mut sigterm = tokio::signal::unix::signal(SignalKind::terminate()).unwrap();
    tokio::select! {
        _ = ctrl_c => tracing::info!("SIGINT received"),
        _ = sigterm.recv() => tracing::info!("SIGTERM received"),
    }
}
```

- [ ] **Step 4: Write integration test for route structure**

```rust
#[tokio::test]
async fn test_health_endpoint() {
    let app = build_test_app().await;
    let response = app.get("/health").await;
    assert_eq!(response.status(), StatusCode::OK);
}

#[tokio::test]
async fn test_api_404_returns_json_not_html() {
    let app = build_test_app().await;
    let response = app.get("/admin/api/nonexistent").await;
    assert_eq!(response.status(), StatusCode::NOT_FOUND);
    assert!(response.headers().get("content-type").unwrap().to_str().unwrap().contains("json"));
}

#[tokio::test]
async fn test_spa_fallback_returns_html() {
    let app = build_test_app().await;
    let response = app.get("/dashboard/anything").await;
    assert_eq!(response.status(), StatusCode::OK);
    // Content-Type should be HTML
}
```

- [ ] **Step 5: Run tests**

Run: `cd admin-dashboard/backend-rs && cargo test`
Expected: all pass

- [ ] **Step 6: Commit**

```
feat(admin-dashboard): assemble AppState, routes, and main with graceful shutdown
```

---

## Task 20: OpenAPI Documentation

**Files:**
- Modify: `admin-dashboard/backend-rs/src/handlers/auth.rs`
- Modify: `admin-dashboard/backend-rs/src/handlers/docker.rs`
- Modify: `admin-dashboard/backend-rs/src/handlers/status.rs`
- Modify: `admin-dashboard/backend-rs/src/main.rs`

- [ ] **Step 1: Add utoipa annotations to all handlers**

```rust
#[utoipa::path(
    post,
    path = "/admin/api/auth/login",
    request_body = LoginRequest,
    responses(
        (status = 200, description = "Login successful", body = LoginResponse),
        (status = 401, description = "Authentication failed", body = ErrorResponse),
        (status = 429, description = "Rate limited", body = ErrorResponse),
    ),
    tag = "auth"
)]
pub async fn handle_login(/* ... */) { /* ... */ }
```

- [ ] **Step 2: Mount Swagger UI**

```rust
// In router build
.merge(SwaggerUi::new("/swagger-ui").url("/api-docs/openapi.json", ApiDoc::openapi()))
```

- [ ] **Step 3: Verify Swagger UI loads**

Run: `cargo run` and check `http://localhost:30190/swagger-ui/`

- [ ] **Step 4: Commit**

```
docs(admin-dashboard): add OpenAPI annotations and Swagger UI
```

---

## Task 21: Makefile + Dockerfile

**Files:**
- Create: `admin-dashboard/backend-rs/Makefile`
- Modify: `admin-dashboard/Dockerfile`

- [ ] **Step 1: Create Makefile**

```makefile
.PHONY: build test lint fmt clean

build:
	cargo build --release

test:
	cargo test

lint:
	cargo clippy -- -D warnings

fmt:
	cargo fmt --check

clean:
	cargo clean

docker-build:
	cd .. && docker build -f Dockerfile -t admin-dashboard:latest .
```

- [ ] **Step 2: Update Dockerfile for Rust build**

```dockerfile
# Stage 1: Frontend (unchanged)
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY admin-dashboard/frontend/package.json admin-dashboard/frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY admin-dashboard/frontend/ .
RUN npm run build

# Stage 2: Rust backend
FROM rust:1.87-alpine AS builder
RUN apk add --no-cache musl-dev
WORKDIR /build/admin-dashboard/backend-rs
COPY admin-dashboard/backend-rs/Cargo.toml admin-dashboard/backend-rs/Cargo.lock ./
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/build/target \
    mkdir src && echo "fn main(){}" > src/main.rs && cargo build --release && rm -rf src
COPY admin-dashboard/backend-rs/ .
COPY --from=frontend-builder /app/frontend/dist ./static/dist
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/build/target \
    cargo build --release && cp /build/target/release/admin-dashboard ./admin

# Stage 3: Runtime
FROM alpine:3.23
RUN apk add --no-cache ca-certificates tini tzdata
ENV TZ=Asia/Seoul
RUN addgroup -g 1000 appgroup && adduser -u 1000 -G appgroup -s /bin/sh -D appuser
WORKDIR /app
COPY --from=builder --link --chown=1000:1000 /build/admin-dashboard/backend-rs/admin ./
USER appuser
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT:-30190}/health || exit 1
EXPOSE 30190
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./admin"]
```

- [ ] **Step 3: Verify Docker build**

Run: `docker build -f admin-dashboard/Dockerfile -t admin-dashboard:test .`
Expected: builds successfully

- [ ] **Step 4: Commit**

```
build(admin-dashboard): add Rust Makefile and update Dockerfile
```

---

## Task 22: Integration Tests

**Files:**
- Create: `admin-dashboard/backend-rs/tests/integration_test.rs`

- [ ] **Step 1: Full auth flow integration test**

```rust
#[tokio::test]
async fn test_full_auth_flow() {
    // Build test app with real or mock Valkey
    // 1. Login with correct credentials → 200, session cookie, csrf token
    // 2. Access authenticated endpoint with session cookie → 200
    // 3. Heartbeat → 200
    // 4. Logout → 200
    // 5. Access authenticated endpoint → 401
}
```

- [ ] **Step 2: API contract compatibility tests**

Verify all response shapes match the Go implementation's contract:
- Login response: `{"status": "ok", "message": "Login successful", "csrf_token": "..."}`
- Container list: `{"status": "ok", "containers": [...]}`
- Docker health: `{"status": "ok", "available": true}`
- Status: aggregated format matches spec

- [ ] **Step 3: SPA fallback boundary test**

```rust
#[tokio::test]
async fn test_api_paths_never_return_html() {
    // GET /admin/api/nonexistent → 404 JSON
    // GET /admin/api/docker/nonexistent → 404 JSON (after auth)
    // GET /dashboard/anything → 200 HTML
}
```

- [ ] **Step 4: Run all tests**

Run: `cd admin-dashboard/backend-rs && cargo test`
Expected: all pass

- [ ] **Step 5: Commit**

```
test(admin-dashboard): add integration tests for auth flow and API contract
```

---

## Task 23: Migration — Go Backend Removal

**Files:**
- Remove: `admin-dashboard/backend/` (entire Go directory)
- Rename: `admin-dashboard/backend-rs/` → `admin-dashboard/backend/`
- Modify: `go.work`
- Modify: `admin-dashboard/AGENTS.md`

- [ ] **Step 1: Verify Rust backend passes all tests**

Run: `cd admin-dashboard/backend-rs && cargo test && cargo clippy -- -D warnings`
Expected: all pass, no warnings

- [ ] **Step 2: Remove Go backend**

```bash
rm -rf admin-dashboard/backend/
```

- [ ] **Step 3: Rename Rust backend**

```bash
mv admin-dashboard/backend-rs/ admin-dashboard/backend/
```

- [ ] **Step 4: Update go.work**

Remove `admin-dashboard/backend` entry from `go.work`.

- [ ] **Step 5: Update AGENTS.md**

Update `admin-dashboard/AGENTS.md`:
- Change tech stack from Go/Gin to Rust/axum
- Update commands: `cargo build`, `cargo test`, `cargo clippy`
- Update key files for Rust paths

- [ ] **Step 6: Full stack build test**

Run: `./build-all.sh --no-bump`
Expected: admin-dashboard builds with Rust backend

- [ ] **Step 7: Commit**

```
refactor(admin-dashboard): replace Go backend with Rust implementation
```
