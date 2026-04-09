//! Integration tests for route structure and API contracts.
//! These tests verify response shapes and routing behavior using axum's test utilities.
//! Full auth flow tests require a Valkey instance and are skipped in CI.

use std::net::SocketAddr;
use std::sync::{Arc, OnceLock};

use admin_dashboard::auth::rate_limiter::LoginRateLimiter;
use admin_dashboard::auth::session::ValkeySessionStore;
use admin_dashboard::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
use admin_dashboard::proxy::{BotProxy, HOLO_PROXY_ROUTE};
use admin_dashboard::routes::build_router;
use admin_dashboard::state::AppState;
use admin_dashboard::status::{StatusCollector, SystemStats};
use axum::body::Body;
use axum::extract::ConnectInfo;
use axum::http::{Request, StatusCode, header};
use axum::{Json, Router, routing::any};
use deadpool_redis::Runtime;
use hyper::body::Incoming;
use hyper::server::conn::http2;
use hyper::service::service_fn;
use hyper::{Request as HyperRequest, Response as HyperResponse};
use hyper_util::rt::{TokioExecutor, TokioIo};
use serde_json::json;
use tokio::net::TcpListener;
use tower::ServiceExt;

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
        bot_proxy: None,
        docker_svc: None,
        status_collector,
        stats_tx,
    });

    build_router(state)
}

fn build_proxy_test_app(target_url: &str, api_key: Option<String>) -> axum::Router {
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
        bot_proxy: Some(BotProxy::new(target_url, api_key)),
        docker_svc: None,
        status_collector,
        stats_tx,
    });

    Router::new()
        .route(
            HOLO_PROXY_ROUTE,
            any(admin_dashboard::proxy::bot_proxy::proxy_holo),
        )
        .with_state(state)
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
async fn test_holo_proxy_route_without_cookie_returns_401() {
    let app = build_test_app();
    let req = Request::get("/admin/api/holo/members")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn test_holo_proxy_route_rewrites_and_forwards_successfully() {
    let captured = Arc::new(tokio::sync::Mutex::new(None::<(String, Option<String>)>));
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let captured_clone = Arc::clone(&captured);

    let server = tokio::spawn(async move {
        let (stream, _) = listener.accept().await.unwrap();
        let io = TokioIo::new(stream);
        let svc = service_fn(move |req: HyperRequest<Incoming>| {
            let captured = Arc::clone(&captured_clone);
            async move {
                let path = req
                    .uri()
                    .path_and_query()
                    .map(|value| value.as_str().to_string())
                    .unwrap_or_default();
                let api_key = req
                    .headers()
                    .get("x-api-key")
                    .and_then(|value| value.to_str().ok())
                    .map(std::string::ToString::to_string);
                *captured.lock().await = Some((path, api_key));

                let response = HyperResponse::builder()
                    .status(StatusCode::OK)
                    .header(header::CONTENT_TYPE, "application/json")
                    .body(Body::from(Json(json!({ "status": "ok" })).to_string()))
                    .unwrap();
                Ok::<_, std::convert::Infallible>(response)
            }
        });

        http2::Builder::new(TokioExecutor::new())
            .serve_connection(io, svc)
            .await
            .unwrap();
    });

    let app = build_proxy_test_app(
        &format!("http://{addr}"),
        Some("proxy-test-key".to_string()),
    );
    let req = Request::get("/admin/api/holo/members?org=hololive")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    let body = axum::body::to_bytes(resp.into_body(), 4096).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(json["status"], "ok");

    let captured_request = captured.lock().await.clone().expect("captured request");
    assert_eq!(captured_request.0, "/api/holo/members?org=hololive");
    assert_eq!(captured_request.1.as_deref(), Some("proxy-test-key"));

    server.await.unwrap();
}

#[tokio::test]
async fn test_swagger_ui_accessible() {
    let app = build_test_app();
    let req = Request::get("/swagger-ui/").body(Body::empty()).unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert!(
        resp.status().is_success() || resp.status().is_redirection(),
        "Swagger UI should be accessible, got: {}",
        resp.status()
    );
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
async fn test_openapi_contains_auth_session_route() {
    let app = build_test_app();
    let req = Request::get("/api-docs/openapi.json")
        .body(Body::empty())
        .unwrap();
    let resp = app.oneshot(req).await.unwrap();
    assert_eq!(resp.status(), StatusCode::OK);

    let body = axum::body::to_bytes(resp.into_body(), 256 * 1024)
        .await
        .unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert!(json["paths"]["/admin/api/auth/session"].is_object());
}
