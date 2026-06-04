use std::sync::{Arc, OnceLock};

use axum::body::Body;
use axum::extract::State;
use axum::http::{Request as HttpRequest, header};
use axum::response::IntoResponse;
use deadpool_redis::Runtime;

use crate::auth::SessionId;
use crate::auth::rate_limiter::LoginRateLimiter;
use crate::auth::session::{Session, ValkeySessionStore};
use crate::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
use crate::holo::client::HoloApiClient;
use crate::state::AppState;
use crate::status::{StatusCollector, SystemStats};

pub use super::fake_valkey::FakeValkey;
use super::heartbeat::handle_heartbeat;

fn test_admin_pass_hash() -> &'static str {
    static HASH: OnceLock<String> = OnceLock::new();
    HASH.get_or_init(|| bcrypt::hash("testpass", bcrypt::DEFAULT_COST).expect("bcrypt hash"))
        .as_str()
}

pub fn test_state_with_session_config(
    valkey_url: String,
    session_config: SessionConfig,
) -> Arc<AppState> {
    let config = Config {
        port: 30190,
        env: "test".to_string(),
        log_level: "info".to_string(),
        admin_user: "admin".to_string(),
        admin_pass_hash: test_admin_pass_hash().to_string(),
        session_secret: "test-secret-key-minimum-length".to_string(),
        valkey_url,
        docker_host: "tcp://127.0.0.1:2375".to_string(),
        holo_admin_api_url: "http://127.0.0.1:30006".to_string(),
        holo_bot_api_key: String::new(),
        enable_openapi: true,
        enable_swagger_ui: true,
        log_dir: "/tmp/admin-dashboard-test-logs".to_string(),
        security: SecurityConfig {
            allowed_origins: vec!["http://localhost:5173".to_string()],
            allow_localhost_in_prod: true,
            csrf_mode: SecurityMode::Enforce,
            ws_origin_mode: SecurityMode::Enforce,
            force_https: false,
            tls_enabled: false,
            tls_cert_path: "/tmp/test.crt".to_string(),
            tls_key_path: "/tmp/test.key".to_string(),
        },
        session: session_config,
    };

    let pool = deadpool_redis::Config::from_url(format!("redis://{}", config.valkey_url))
        .create_pool(Some(Runtime::Tokio1))
        .expect("valkey pool creation failed");

    let sessions = ValkeySessionStore::new(pool, config.session.clone());
    let rate_limiter = Arc::new(LoginRateLimiter::new());
    let status_collector =
        StatusCollector::new(vec![], env!("CARGO_PKG_VERSION")).expect("status collector init");
    let (stats_tx, _) = tokio::sync::broadcast::channel::<SystemStats>(16);
    let holo_api = Arc::new(
        HoloApiClient::new(&config.holo_admin_api_url, None).expect("holo api client init failed"),
    );

    Arc::new(AppState {
        config,
        sessions,
        rate_limiter,
        holo_api,
        docker_svc: None,
        status_collector,
        stats_tx,
    })
}

pub fn test_state(valkey_url: String) -> Arc<AppState> {
    test_state_with_session_config(valkey_url, SessionConfig::default())
}

pub fn build_session(
    session_id: &str,
    absolute_expires_at: chrono::DateTime<chrono::Utc>,
) -> Session {
    let now = chrono::Utc::now();
    Session {
        id: session_id.to_string(),
        created_at: now - chrono::Duration::hours(1),
        expires_at: now + chrono::Duration::minutes(30),
        absolute_expires_at,
        last_rotated_at: now - chrono::Duration::minutes(20),
        rotated_to: None,
    }
}

pub async fn call_heartbeat(
    state: Arc<AppState>,
    session_id: &str,
    body: &str,
) -> axum::response::Response {
    let mut req = HttpRequest::post("/admin/api/auth/heartbeat")
        .header(header::CONTENT_TYPE, "application/json")
        .body(Body::from(body.to_string()))
        .expect("heartbeat request");
    req.extensions_mut()
        .insert(SessionId(session_id.to_string()));

    match handle_heartbeat(State(state), req).await {
        Ok(response) => response.into_response(),
        Err(error) => error.into_response(),
    }
}
