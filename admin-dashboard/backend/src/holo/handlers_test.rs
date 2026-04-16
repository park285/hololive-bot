use std::net::SocketAddr;
use std::sync::Arc;

use crate::auth::rate_limiter::LoginRateLimiter;
use crate::auth::session::ValkeySessionStore;
use crate::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
use crate::holo::client::HoloApiClient;
use crate::state::AppState;
use crate::status::{StatusCollector, SystemStats};
use axum::Json;
use axum::extract::{Query, State};
use axum::response::IntoResponse;
use deadpool_redis::Runtime;
use tokio::net::TcpListener;

use super::handlers::*;
use super::types::{ChannelStatsQuery, MilestonesQuery, NearMilestonesQuery, StreamsQuery};

fn test_state(base_url: String) -> Arc<AppState> {
    let config = Config {
        port: 30190,
        env: "test".to_string(),
        log_level: "info".to_string(),
        admin_user: "admin".to_string(),
        admin_pass_hash: "hash".to_string(),
        session_secret: "test-secret-key-minimum-length".to_string(),
        valkey_url: "127.0.0.1:1".to_string(),
        docker_host: "tcp://127.0.0.1:2375".to_string(),
        holo_admin_api_url: base_url.clone(),
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

    Arc::new(AppState {
        config,
        sessions,
        rate_limiter,
        holo_api: Arc::new(
            HoloApiClient::new(&base_url, None).expect("holo api client init failed"),
        ),
        docker_svc: None,
        status_collector,
        stats_tx,
    })
}

async fn spawn_holo_server() -> String {
    use axum::extract::Request;
    use axum::http::StatusCode;
    use axum::routing::{get, patch, post};
    use axum::{Json, Router};
    use serde_json::json;

    async fn route_response(req: Request) -> impl IntoResponse {
        let path = req.uri().path().to_string();
        let query = req.uri().query().unwrap_or_default().to_string();
        match path.as_str() {
            "/api/holo/alarms" => Json(json!({ "status": "ok", "alarms": [{ "roomId": "room-1", "roomName": "Room", "userId": "user-1", "userName": "User", "channelId": "ch-1", "memberName": "Mio" }] })).into_response(),
            "/api/holo/members" => Json(json!({ "status": "ok", "members": [{ "id": 1, "channelId": "ch-1", "name": "Mio", "aliases": { "ko": ["미오"], "ja": ["みお"] }, "nameJa": "みお", "nameKo": "미오", "isGraduated": false }] })).into_response(),
            "/api/holo/rooms" => Json(json!({ "status": "ok", "rooms": ["room-1"], "aclEnabled": true, "aclMode": "blacklist" })).into_response(),
            "/api/holo/settings" => Json(json!({ "status": "ok", "settings": { "alarmAdvanceMinutes": 5 } })).into_response(),
            "/api/holo/stats" => Json(json!({ "status": "ok", "members": 1, "alarms": 2, "rooms": 3, "version": "v1", "uptime": "1h" })).into_response(),
            "/api/holo/stats/channels" => Json(json!({
                "status": "ok",
                "stats": {
                    "ch-1": { "ChannelID": "ch-1", "ChannelTitle": "Mio", "SubscriberCount": 100, "VideoCount": 10, "ViewCount": 1000 },
                    "ch-2": { "ChannelID": "ch-2", "ChannelTitle": "Suisei", "SubscriberCount": 300, "VideoCount": 30, "ViewCount": 3000 },
                    "ch-3": { "ChannelID": "ch-3", "ChannelTitle": "Korone", "SubscriberCount": 200, "VideoCount": 20, "ViewCount": 2000 }
                }
            })).into_response(),
            "/api/holo/stats/youtube/community-shorts" => Json(json!({
                "status": "ok",
                "generatedAt": "2026-04-10T00:00:00Z",
                "windowStart": "2026-04-09T00:00:00Z",
                "windowEnd": "2026-04-10T00:00:00Z",
                "windowHours": 24,
                "observedAtBasis": "COALESCE(actual_published_at, detected_at)",
                "slaThresholdMillis": 120000,
                "overview": {
                    "channelCount": 1,
                    "detectedPostCount": 2,
                    "alarmSentPostCount": 1,
                    "successPostCount": 1,
                    "failedPostCount": 1,
                    "detectedUnsentPostCount": 1,
                    "pendingPostCount": 1,
                    "latencyMeasuredPostCount": 2,
                    "withinTargetPostCount": 1,
                    "exceededPostCount": 1,
                    "communityDetectedPostCount": 1,
                    "shortsDetectedPostCount": 1,
                    "communityExceededPostCount": 0,
                    "shortsExceededPostCount": 1,
                    "averageLatencyMillis": 90000,
                    "maxLatencyMillis": 180000
                },
                "channels": [{
                    "channelId": "ch-1",
                    "memberName": "Mio",
                    "earliestObservedAt": "2026-04-09T12:00:00Z",
                    "latestObservedAt": "2026-04-09T23:00:00Z",
                    "detectedPostCount": 2,
                    "alarmSentPostCount": 1,
                    "successPostCount": 1,
                    "failedPostCount": 1,
                    "detectedUnsentPostCount": 1,
                    "pendingPostCount": 1,
                    "latencyMeasuredPostCount": 2,
                    "withinTargetPostCount": 1,
                    "exceededPostCount": 1,
                    "communityPostCount": 1,
                    "shortsPostCount": 1,
                    "averageLatencyMillis": 90000,
                    "maxLatencyMillis": 180000
                }]
            })).into_response(),
            "/api/holo/streams/live" => Json(json!({ "status": "ok", "org": if query.contains("org=") { "hololive" } else { "" }, "streams": [{ "id": "s1", "title": "Live", "status": "live", "channel_name": "Mio", "channel_id": "ch-1", "thumbnail": null, "link": null, "start_scheduled": null, "start_actual": null }] })).into_response(),
            "/api/holo/streams/upcoming" => Json(json!({ "status": "ok", "org": "hololive", "streams": [] })).into_response(),
            "/api/holo/milestones" => Json(json!({ "status": "ok", "milestones": [{ "channelId": "ch-1", "memberName": "Mio", "type": "subs", "value": 100000, "achievedAt": "2026-01-01T00:00:00Z", "notified": true }], "total": 1, "limit": 50, "offset": 0 })).into_response(),
            "/api/holo/milestones/near" => Json(json!({ "status": "ok", "members": [{ "channelId": "ch-1", "memberName": "Mio", "currentSubs": 99000, "nextMilestone": 100000, "remaining": 1000, "progressPct": 0.99 }], "count": 1, "threshold": 0.9 })).into_response(),
            "/api/holo/milestones/stats" => Json(json!({ "status": "ok", "stats": { "totalAchieved": 1, "totalNearMilestone": 2, "recentAchievements": 3, "notNotifiedCount": 4 } })).into_response(),
            _ => (StatusCode::OK, Json(json!({ "status": "ok" }))).into_response(),
        }
    }

    let app = Router::new()
        .route(
            "/api/holo/alarms",
            get(route_response).delete(route_response),
        )
        .route(
            "/api/holo/members",
            get(route_response).post(route_response),
        )
        .route(
            "/api/holo/members/{id}/aliases",
            post(route_response).delete(route_response),
        )
        .route("/api/holo/members/{id}/graduation", patch(route_response))
        .route("/api/holo/members/{id}/channel", patch(route_response))
        .route("/api/holo/members/{id}/name", patch(route_response))
        .route(
            "/api/holo/rooms",
            get(route_response)
                .post(route_response)
                .delete(route_response),
        )
        .route("/api/holo/rooms/acl", post(route_response))
        .route(
            "/api/holo/settings",
            get(route_response).post(route_response),
        )
        .route("/api/holo/stats", get(route_response))
        .route("/api/holo/stats/channels", get(route_response))
        .route(
            "/api/holo/stats/youtube/community-shorts",
            get(route_response),
        )
        .route("/api/holo/streams/live", get(route_response))
        .route("/api/holo/streams/upcoming", get(route_response))
        .route("/api/holo/milestones", get(route_response))
        .route("/api/holo/milestones/near", get(route_response))
        .route("/api/holo/milestones/stats", get(route_response));

    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        axum::serve(listener, app).await.unwrap();
    });
    format!("http://{addr}")
}

#[tokio::test]
async fn test_holo_handlers_return_typed_bodies() {
    let base_url = spawn_holo_server().await;
    let state = test_state(base_url);

    let (_, Json(alarms)) = get_alarms(State(Arc::clone(&state))).await.unwrap();
    assert_eq!(alarms.alarms[0].room_id, "room-1");

    let (_, Json(members)) = get_members(State(Arc::clone(&state))).await.unwrap();
    assert_eq!(members.members[0].channel_id, "ch-1");

    let (_, Json(rooms)) = get_rooms(State(Arc::clone(&state))).await.unwrap();
    assert!(rooms.acl_enabled);

    let (_, Json(settings)) = get_settings(State(Arc::clone(&state))).await.unwrap();
    assert_eq!(settings.settings.alarm_advance_minutes, 5);

    let (_, Json(stats)) = get_stats(State(Arc::clone(&state))).await.unwrap();
    assert_eq!(stats.version, "v1");

    let (_, Json(channel_stats)) = get_channel_stats(
        State(Arc::clone(&state)),
        Query(ChannelStatsQuery { limit: None }),
    )
    .await
    .unwrap();
    assert_eq!(channel_stats.stats.len(), 3);

    let (_, Json(youtube_ops)) = get_youtube_community_shorts_ops(State(Arc::clone(&state)))
        .await
        .unwrap();
    assert_eq!(youtube_ops.overview.exceeded_post_count, 1);

    let (_, Json(streams)) = get_live_streams(
        State(Arc::clone(&state)),
        Query(StreamsQuery {
            org: Some("hololive".to_string()),
        }),
    )
    .await
    .unwrap();
    assert_eq!(streams.org.as_deref(), Some("hololive"));

    let (_, Json(milestones)) = get_milestones(
        State(Arc::clone(&state)),
        Query(MilestonesQuery {
            limit: None,
            offset: None,
            channel_id: None,
            member_name: None,
        }),
    )
    .await
    .unwrap();
    assert_eq!(milestones.total, 1);

    let (_, Json(near)) = get_near_milestones(
        State(Arc::clone(&state)),
        Query(NearMilestonesQuery {
            threshold: Some(0.9),
        }),
    )
    .await
    .unwrap();
    assert_eq!(near.count, 1);

    let (_, Json(milestone_stats)) = get_milestone_stats(State(state)).await.unwrap();
    assert_eq!(milestone_stats.stats.total_achieved, 1);
}

#[tokio::test]
async fn test_channel_stats_limit_keeps_top_subscribers_only() {
    let base_url = spawn_holo_server().await;
    let state = test_state(base_url);

    let (_, Json(channel_stats)) =
        get_channel_stats(State(state), Query(ChannelStatsQuery { limit: Some(2) }))
            .await
            .unwrap();

    assert_eq!(channel_stats.stats.len(), 2);
    assert!(channel_stats.stats.contains_key("ch-2"));
    assert!(channel_stats.stats.contains_key("ch-3"));
    assert!(!channel_stats.stats.contains_key("ch-1"));
}

#[tokio::test]
async fn test_holo_handler_invalid_json_returns_502() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr: SocketAddr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let app = axum::Router::new().route(
            "/api/holo/members",
            axum::routing::get(|| async { (axum::http::StatusCode::OK, "not-json") }),
        );
        axum::serve(listener, app).await.unwrap();
    });

    let state = test_state(format!("http://{addr}"));
    let error = get_members(State(state))
        .await
        .expect_err("expected proxy error");
    assert_eq!(
        error.into_response().status(),
        axum::http::StatusCode::BAD_GATEWAY
    );
}

#[tokio::test]
async fn test_holo_handler_upstream_5xx_returns_502() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr: SocketAddr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let app = axum::Router::new().route(
            "/api/holo/alarms",
            axum::routing::get(|| async { axum::http::StatusCode::INTERNAL_SERVER_ERROR }),
        );
        axum::serve(listener, app).await.unwrap();
    });

    let state = test_state(format!("http://{addr}"));
    let error = get_alarms(State(state))
        .await
        .expect_err("expected proxy error");
    assert_eq!(
        error.into_response().status(),
        axum::http::StatusCode::BAD_GATEWAY
    );
}
