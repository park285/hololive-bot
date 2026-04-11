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

    let holo_routes = Router::new()
        .route(
            "/admin/api/holo/alarms",
            get(crate::holo::handlers::get_alarms).delete(crate::holo::handlers::delete_alarm),
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
            get(crate::holo::handlers::get_members).post(crate::holo::handlers::add_member),
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
            get(crate::holo::handlers::get_rooms)
                .post(crate::holo::handlers::add_room)
                .delete(crate::holo::handlers::remove_room),
        )
        .route(
            "/admin/api/holo/rooms/acl",
            post(crate::holo::handlers::set_acl),
        )
        .route(
            "/admin/api/holo/settings",
            get(crate::holo::handlers::get_settings).post(crate::holo::handlers::update_settings),
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
        );

    let authenticated = Router::new()
        .merge(auth_csrf)
        .merge(auth_get)
        .merge(holo_routes)
        .layer(auth_layer);

    let api_fallback = Router::new().route(
        "/admin/api/{*path}",
        any(|| async { (StatusCode::NOT_FOUND, Json(json!({ "error": "Not found" }))) }),
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
