use std::sync::Arc;

use axum::http::StatusCode;
use axum::{
    Json, Router, middleware,
    routing::{any, get, post},
};
use serde_json::json;
use utoipa::OpenApi;
use utoipa_swagger_ui::SwaggerUi;

use crate::state::AppState;

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

    let proxy_routes = Router::new().route(
        "/admin/api/holo/{*path}",
        any(crate::proxy::bot_proxy::proxy_holo),
    );

    let authenticated = Router::new()
        .merge(auth_csrf)
        .merge(auth_get)
        .merge(proxy_routes)
        .layer(auth_layer);

    let api_fallback = Router::new().route(
        "/admin/api/{*path}",
        any(|| async { (StatusCode::NOT_FOUND, Json(json!({ "error": "Not found" }))) }),
    );

    let spa = Router::new()
        .route("/favicon.svg", get(crate::static_files::serve_favicon))
        .route("/assets/{*path}", get(crate::static_files::serve_static))
        .fallback(get(crate::static_files::serve_index));

    Router::new()
        .merge(public)
        .merge(authenticated)
        .merge(
            SwaggerUi::new("/swagger-ui")
                .url("/api-docs/openapi.json", crate::openapi::ApiDoc::openapi()),
        )
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
