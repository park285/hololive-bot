use std::sync::Arc;

use axum::{
    Json, Router, middleware,
    routing::{any, get, post},
};
use serde_json::json;
use utoipa::OpenApi;
use utoipa_swagger_ui::SwaggerUi;

use crate::state::AppState;

fn build_docs_router(state: Arc<AppState>) -> Router<Arc<AppState>> {
    if !state.config.enable_openapi && !state.config.enable_swagger_ui {
        return Router::new().with_state(state);
    }

    let auth_layer =
        middleware::from_fn_with_state(state.clone(), crate::auth::middleware::auth_middleware);

    let docs = if state.config.enable_swagger_ui {
        Router::new().merge(
            SwaggerUi::new("/admin/docs")
                .url("/admin/api/openapi.json", crate::openapi::ApiDoc::openapi()),
        )
    } else if state.config.enable_openapi {
        Router::new().route(
            "/admin/api/openapi.json",
            get(|| async { Json(crate::openapi::ApiDoc::openapi()) }),
        )
    } else {
        Router::new()
    };

    docs.layer(auth_layer).with_state(state)
}

fn build_holo_query_routes() -> Router<Arc<AppState>> {
    Router::new()
        .route(
            "/admin/api/holo/alarms",
            get(crate::holo::handlers::get_alarms),
        )
        .route(
            "/admin/api/holo/members",
            get(crate::holo::handlers::get_members),
        )
        .route(
            "/admin/api/holo/rooms",
            get(crate::holo::handlers::get_rooms),
        )
        .route(
            "/admin/api/holo/settings",
            get(crate::holo::handlers::get_settings),
        )
        .route(
            "/admin/api/holo/stats",
            get(crate::holo::handlers::get_stats),
        )
        .route(
            "/admin/api/holo/stats/channels",
            get(crate::holo::handlers::get_channel_stats),
        )
        .route(
            "/admin/api/holo/stats/youtube/community-shorts",
            get(crate::holo::handlers::get_youtube_community_shorts_ops),
        )
        .route(
            "/admin/api/holo/streams/live",
            get(crate::holo::handlers::get_live_streams),
        )
        .route(
            "/admin/api/holo/streams/upcoming",
            get(crate::holo::handlers::get_upcoming_streams),
        )
        .route(
            "/admin/api/holo/milestones",
            get(crate::holo::handlers::get_milestones),
        )
        .route(
            "/admin/api/holo/milestones/near",
            get(crate::holo::handlers::get_near_milestones),
        )
        .route(
            "/admin/api/holo/milestones/stats",
            get(crate::holo::handlers::get_milestone_stats),
        )
        .route(
            "/admin/api/holo/members/calendar",
            get(crate::holo::handlers::get_calendar),
        )
}

fn build_holo_mutation_routes(state: Arc<AppState>) -> Router<Arc<AppState>> {
    let csrf_layer =
        middleware::from_fn_with_state(state, crate::middleware::csrf::csrf_middleware);

    Router::new()
        .route(
            "/admin/api/holo/alarms",
            axum::routing::delete(crate::holo::handlers::delete_alarm),
        )
        .route(
            "/admin/api/holo/names/room",
            post(crate::holo::handlers::set_room_name),
        )
        .route(
            "/admin/api/holo/names/user",
            post(crate::holo::handlers::set_user_name),
        )
        .route(
            "/admin/api/holo/members",
            post(crate::holo::handlers::add_member),
        )
        .route(
            "/admin/api/holo/members/{id}/aliases",
            post(crate::holo::handlers::add_alias).delete(crate::holo::handlers::remove_alias),
        )
        .route(
            "/admin/api/holo/members/{id}/graduation",
            axum::routing::patch(crate::holo::handlers::set_graduation),
        )
        .route(
            "/admin/api/holo/members/{id}/channel",
            axum::routing::patch(crate::holo::handlers::update_channel),
        )
        .route(
            "/admin/api/holo/members/{id}/name",
            axum::routing::patch(crate::holo::handlers::update_member_name),
        )
        .route(
            "/admin/api/holo/rooms",
            post(crate::holo::handlers::add_room).delete(crate::holo::handlers::remove_room),
        )
        .route(
            "/admin/api/holo/rooms/acl",
            post(crate::holo::handlers::set_acl),
        )
        .route(
            "/admin/api/holo/settings",
            post(crate::holo::handlers::update_settings),
        )
        .layer(csrf_layer)
}

#[allow(clippy::too_many_lines)]
pub fn build_router(state: Arc<AppState>) -> Router {
    let auth_layer =
        middleware::from_fn_with_state(state.clone(), crate::auth::middleware::auth_middleware);
    let csrf_layer =
        middleware::from_fn_with_state(state.clone(), crate::middleware::csrf::csrf_middleware);

    let public = Router::new()
        .route("/health", get(|| async { Json(json!({ "status": "ok" })) }))
        .route(
            "/admin/api/auth/login",
            post(crate::handlers::auth::handle_login),
        );

    let auth_csrf = Router::new()
        .route(
            "/admin/api/auth/logout",
            post(crate::handlers::auth::handle_logout),
        )
        .route(
            "/admin/api/auth/heartbeat",
            post(crate::handlers::auth::handle_heartbeat),
        )
        .route(
            "/admin/api/docker/containers/{name}/restart",
            post(crate::handlers::docker::handle_docker_restart),
        )
        .route(
            "/admin/api/docker/containers/{name}/stop",
            post(crate::handlers::docker::handle_docker_stop),
        )
        .route(
            "/admin/api/docker/containers/{name}/start",
            post(crate::handlers::docker::handle_docker_start),
        )
        .layer(csrf_layer);

    let auth_get = Router::new()
        .route(
            "/admin/api/auth/session",
            get(crate::handlers::auth::handle_session_status),
        )
        .route(
            "/admin/api/docker/health",
            get(crate::handlers::docker::handle_docker_health),
        )
        .route(
            "/admin/api/docker/containers",
            get(crate::handlers::docker::handle_docker_containers),
        )
        .route(
            "/admin/api/status",
            get(crate::handlers::status::handle_aggregated_status),
        )
        .route(
            "/admin/api/ws/system-stats",
            get(crate::handlers::status::handle_system_stats_stream),
        );

    let holo_query_routes = build_holo_query_routes();
    let holo_mutation_routes = build_holo_mutation_routes(state.clone());

    let authenticated = Router::new()
        .merge(auth_csrf)
        .merge(auth_get)
        .merge(holo_query_routes)
        .merge(holo_mutation_routes)
        .layer(auth_layer);

    let api_fallback = Router::new().route(
        "/admin/api/{*path}",
        any(|| async { crate::error::AppError::Api(crate::error::ApiError::NotFound) }),
    );

    let spa = Router::new()
        .route("/favicon.svg", get(crate::static_files::serve_favicon))
        .route("/assets/{*path}", get(crate::static_files::serve_static))
        .fallback(get(crate::static_files::serve_index));
    let docs_router = build_docs_router(state.clone());

    Router::new()
        .merge(public)
        .merge(authenticated)
        .merge(docs_router)
        .merge(api_fallback)
        .merge(spa)
        .layer(middleware::map_response(
            crate::auth::middleware::apply_security_headers,
        ))
        .layer(middleware::from_fn(
            crate::middleware::etag::etag_middleware,
        ))
        .with_state(state)
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, OnceLock};

    use axum::body::Body;
    use axum::extract::Request;
    use axum::http::StatusCode;
    use axum::middleware;
    use deadpool_redis::Runtime;
    use tower::ServiceExt;

    use crate::auth::SessionId;
    use crate::auth::rate_limiter::LoginRateLimiter;
    use crate::auth::session::ValkeySessionStore;
    use crate::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
    use crate::holo::client::HoloApiClient;
    use crate::state::AppState;
    use crate::status::{StatusCollector, SystemStats};

    fn test_admin_pass_hash() -> &'static str {
        static HASH: OnceLock<String> = OnceLock::new();
        HASH.get_or_init(|| bcrypt::hash("testpass", bcrypt::DEFAULT_COST).expect("bcrypt hash"))
            .as_str()
    }

    fn test_state() -> Arc<AppState> {
        let config = Config {
            port: 30190,
            env: "test".to_string(),
            log_level: "info".to_string(),
            admin_user: "admin".to_string(),
            admin_pass_hash: test_admin_pass_hash().to_string(),
            session_secret: "test-secret-key-minimum-length".to_string(),
            valkey_url: "127.0.0.1:1".to_string(),
            docker_host: "tcp://127.0.0.1:2375".to_string(),
            holo_admin_api_url: "http://127.0.0.1:1".to_string(),
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
            HoloApiClient::new(&config.holo_admin_api_url, None)
                .expect("holo api client init failed"),
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

    async fn inject_test_session(
        mut req: Request,
        next: middleware::Next,
    ) -> axum::response::Response {
        req.extensions_mut()
            .insert(SessionId("test-session-id".to_string()));
        next.run(req).await
    }

    #[tokio::test]
    async fn test_holo_mutation_routes_reject_missing_csrf() {
        let state = test_state();
        let app = super::build_holo_mutation_routes(state.clone())
            .layer(middleware::from_fn(inject_test_session))
            .with_state(state);

        let req = Request::post("/admin/api/holo/members")
            .header(axum::http::header::CONTENT_TYPE, "application/json")
            .body(Body::from(
                r#"{"channelId":"ch-1","name":"Mio","nameJa":"みお","nameKo":"미오"}"#,
            ))
            .expect("request");
        let resp = app.oneshot(req).await.expect("response");

        assert_eq!(resp.status(), StatusCode::FORBIDDEN);
    }
}
