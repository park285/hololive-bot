use std::net::SocketAddr;
use std::sync::{Arc, OnceLock};

use admin_dashboard::auth::rate_limiter::LoginRateLimiter;
use admin_dashboard::auth::session::ValkeySessionStore;
use admin_dashboard::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
use admin_dashboard::holo::client::HoloApiClient;
use admin_dashboard::openapi::ApiDoc;
use admin_dashboard::routes::build_router;
use admin_dashboard::state::AppState;
use admin_dashboard::status::{StatusCollector, SystemStats};
use axum::body::Body;
use axum::extract::ConnectInfo;
use axum::http::{Request, StatusCode, header};
use deadpool_redis::Runtime;
use tower::ServiceExt;
use utoipa::OpenApi;

fn test_admin_pass_hash() -> &'static str {
    static HASH: OnceLock<String> = OnceLock::new();
    HASH.get_or_init(|| bcrypt::hash("testpass", bcrypt::DEFAULT_COST).expect("bcrypt hash"))
        .as_str()
}

fn test_config() -> Config {
    Config {
        port: 30190,
        env: "test".to_string(),
        log_level: "info".to_string(),
        admin_user: "admin".to_string(),
        admin_pass_hash: test_admin_pass_hash().to_string(),
        session_secret: "test-secret-key-minimum-length".to_string(),
        valkey_url: "127.0.0.1:1".to_string(),
        docker_host: "tcp://127.0.0.1:2375".to_string(),
        holo_bot_url: "http://127.0.0.1:30001".to_string(),
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
        session: SessionConfig::default(),
    }
}

fn build_test_app() -> axum::Router {
    let config = test_config();
    let pool = deadpool_redis::Config::from_url(format!("redis://{}", config.valkey_url))
        .create_pool(Some(Runtime::Tokio1))
        .expect("valkey pool creation failed");

    let sessions = ValkeySessionStore::new(pool, config.session.clone());
    let rate_limiter = Arc::new(LoginRateLimiter::new());
    let status_collector = StatusCollector::new(vec![], env!("CARGO_PKG_VERSION"));
    let (stats_tx, _) = tokio::sync::broadcast::channel::<SystemStats>(16);
    let state = Arc::new(AppState {
        config,
        sessions,
        rate_limiter,
        holo_api: Arc::new(
            HoloApiClient::new("http://127.0.0.1:30001", None)
                .expect("holo api client init failed"),
        ),
        docker_svc: None,
        status_collector,
        stats_tx,
    });

    build_router(state)
}

fn with_connect_info(mut req: Request<Body>) -> Request<Body> {
    req.extensions_mut()
        .insert(ConnectInfo(SocketAddr::from(([127, 0, 0, 1], 12345))));
    req
}

#[tokio::test]
async fn test_health_endpoint() {
    let app = build_test_app();
    let req = Request::get("/health").body(Body::empty()).unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    let body = axum::body::to_bytes(resp.into_body(), 1024).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(json["status"], "ok");
}

#[tokio::test]
async fn test_api_404_returns_json_not_html() {
    let app = build_test_app();
    let req = Request::get("/admin/api/nonexistent")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::NOT_FOUND);

    let content_type = resp
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    assert!(
        content_type.contains("json"),
        "API 404 should return JSON, got: {content_type}",
    );
}

#[tokio::test]
async fn test_login_without_body_returns_error() {
    let app = build_test_app();
    let req = with_connect_info(
        Request::post("/admin/api/auth/login")
            .header(header::CONTENT_TYPE, "application/json")
            .body(Body::from("{}"))
            .unwrap(),
    );
    let resp = app.oneshot(req).await.unwrap();
    assert!(resp.status().is_client_error());
}

#[tokio::test]
async fn test_authenticated_endpoint_without_cookie_returns_401() {
    let app = build_test_app();
    let req = Request::get("/admin/api/docker/health")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn test_typed_holo_route_without_cookie_returns_401() {
    let app = build_test_app();
    let req = Request::get("/admin/api/holo/members")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn test_youtube_ops_route_without_cookie_returns_401() {
    let app = build_test_app();
    let req = Request::get("/admin/api/holo/stats/youtube/community-shorts")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn test_unknown_holo_route_without_cookie_returns_404() {
    let app = build_test_app();
    let req = Request::get("/admin/api/holo/legacy")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::NOT_FOUND);

    let body = axum::body::to_bytes(resp.into_body(), 4096).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(json["error"], "Not found");
}

#[tokio::test]
async fn test_swagger_ui_requires_auth() {
    let app = build_test_app();
    let req = Request::get("/admin/docs/").body(Body::empty()).unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn test_login_response_contract() {
    let app = build_test_app();
    let req = with_connect_info(
        Request::post("/admin/api/auth/login")
            .header(header::CONTENT_TYPE, "application/json")
            .body(Body::from(r#"{"username":"wrong","password":"wrong"}"#))
            .unwrap(),
    );
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);

    let body = axum::body::to_bytes(resp.into_body(), 4096).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert!(
        json["error"].is_string(),
        "Error response should have 'error' field"
    );
}

#[tokio::test]
async fn test_auth_session_without_cookie_returns_401() {
    let app = build_test_app();
    let req = Request::get("/admin/api/auth/session")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn test_auth_session_store_failure_returns_503() {
    let app = build_test_app();
    let signed = admin_dashboard::auth::sign_session_id(
        "session-that-will-hit-valkey",
        "test-secret-key-minimum-length",
    );
    let req = Request::get("/admin/api/auth/session")
        .header(header::COOKIE, format!("admin_session={signed}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::SERVICE_UNAVAILABLE);
}

#[tokio::test]
async fn test_openapi_endpoint_requires_auth() {
    let app = build_test_app();
    let req = Request::get("/admin/api/openapi.json")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[test]
fn test_openapi_contains_auth_session_route() {
    let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
    assert!(json["paths"]["/admin/api/auth/session"].is_object());
}
