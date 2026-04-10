use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, Query, State};
use axum::http::StatusCode;
use reqwest::Method;

use crate::error::AppError;
use crate::state::AppState;

use super::types::{
    AddAliasRequest, AddMemberRequest, AddRoomRequest, AlarmsResponse, ChannelStatsResponse,
    DeleteAlarmRequest, MembersResponse, MilestoneStatsResponse, MilestonesQuery,
    MilestonesResponse, NearMilestonesQuery, NearMilestonesResponse, RemoveAliasRequest,
    RemoveRoomRequest, RoomNameUpdateRequest, RoomsResponse, SetAclRequest, SetAclResponse,
    SetGraduationRequest, Settings, SettingsResponse, StatsResponse, StatusOnlyResponse,
    StreamsQuery, StreamsResponse, UpdateChannelRequest, UpdateMemberNameRequest,
    UserNameUpdateRequest,
};

async fn get_typed<T: serde::de::DeserializeOwned>(
    state: &Arc<AppState>,
    path: &str,
    query: Option<Vec<(&str, String)>>,
) -> Result<(StatusCode, Json<T>), AppError> {
    let (status, payload) = state.holo_api.get(path, query.as_deref()).await?;
    Ok((status, Json(payload)))
}

async fn send_typed<B, T>(
    state: &Arc<AppState>,
    method: Method,
    path: &str,
    body: &B,
) -> Result<(StatusCode, Json<T>), AppError>
where
    B: serde::Serialize + ?Sized,
    T: serde::de::DeserializeOwned,
{
    let (status, payload) = state.holo_api.send(method, path, Some(body)).await?;
    Ok((status, Json(payload)))
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/alarms",
    operation_id = "holoGetAlarms",
    responses((status = 200, body = AlarmsResponse)),
    tag = "holo"
)]
pub async fn get_alarms(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<AlarmsResponse>), AppError> {
    get_typed(&state, "/api/holo/alarms", None).await
}

#[utoipa::path(
    delete,
    path = "/admin/api/holo/alarms",
    operation_id = "holoDeleteAlarm",
    request_body = DeleteAlarmRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn delete_alarm(
    State(state): State<Arc<AppState>>,
    Json(body): Json<DeleteAlarmRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::DELETE, "/api/holo/alarms", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/names/room",
    operation_id = "holoSetRoomName",
    request_body = RoomNameUpdateRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn set_room_name(
    State(state): State<Arc<AppState>>,
    Json(body): Json<RoomNameUpdateRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/names/room", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/names/user",
    operation_id = "holoSetUserName",
    request_body = UserNameUpdateRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn set_user_name(
    State(state): State<Arc<AppState>>,
    Json(body): Json<UserNameUpdateRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/names/user", &body).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/members",
    operation_id = "holoGetMembers",
    responses((status = 200, body = MembersResponse)),
    tag = "holo"
)]
pub async fn get_members(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<MembersResponse>), AppError> {
    get_typed(&state, "/api/holo/members", None).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/members",
    operation_id = "holoAddMember",
    request_body = AddMemberRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn add_member(
    State(state): State<Arc<AppState>>,
    Json(body): Json<AddMemberRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/members", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/members/{id}/aliases",
    operation_id = "holoAddAlias",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = AddAliasRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn add_alias(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<AddAliasRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::POST,
        &format!("/api/holo/members/{id}/aliases"),
        &body,
    )
    .await
}

#[utoipa::path(
    delete,
    path = "/admin/api/holo/members/{id}/aliases",
    operation_id = "holoRemoveAlias",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = RemoveAliasRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn remove_alias(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<RemoveAliasRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::DELETE,
        &format!("/api/holo/members/{id}/aliases"),
        &body,
    )
    .await
}

#[utoipa::path(
    patch,
    path = "/admin/api/holo/members/{id}/graduation",
    operation_id = "holoSetGraduation",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = SetGraduationRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn set_graduation(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<SetGraduationRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::PATCH,
        &format!("/api/holo/members/{id}/graduation"),
        &body,
    )
    .await
}

#[utoipa::path(
    patch,
    path = "/admin/api/holo/members/{id}/channel",
    operation_id = "holoUpdateChannel",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = UpdateChannelRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn update_channel(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<UpdateChannelRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::PATCH,
        &format!("/api/holo/members/{id}/channel"),
        &body,
    )
    .await
}

#[utoipa::path(
    patch,
    path = "/admin/api/holo/members/{id}/name",
    operation_id = "holoUpdateMemberName",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = UpdateMemberNameRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn update_member_name(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<UpdateMemberNameRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::PATCH,
        &format!("/api/holo/members/{id}/name"),
        &body,
    )
    .await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/rooms",
    operation_id = "holoGetRooms",
    responses((status = 200, body = RoomsResponse)),
    tag = "holo"
)]
pub async fn get_rooms(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<RoomsResponse>), AppError> {
    get_typed(&state, "/api/holo/rooms", None).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/rooms",
    operation_id = "holoAddRoom",
    request_body = AddRoomRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn add_room(
    State(state): State<Arc<AppState>>,
    Json(body): Json<AddRoomRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/rooms", &body).await
}

#[utoipa::path(
    delete,
    path = "/admin/api/holo/rooms",
    operation_id = "holoRemoveRoom",
    request_body = RemoveRoomRequest,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn remove_room(
    State(state): State<Arc<AppState>>,
    Json(body): Json<RemoveRoomRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::DELETE, "/api/holo/rooms", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/rooms/acl",
    operation_id = "holoSetAcl",
    request_body = SetAclRequest,
    responses((status = 200, body = SetAclResponse)),
    tag = "holo"
)]
pub async fn set_acl(
    State(state): State<Arc<AppState>>,
    Json(body): Json<SetAclRequest>,
) -> Result<(StatusCode, Json<SetAclResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/rooms/acl", &body).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/settings",
    operation_id = "holoGetSettings",
    responses((status = 200, body = SettingsResponse)),
    tag = "holo"
)]
pub async fn get_settings(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<SettingsResponse>), AppError> {
    get_typed(&state, "/api/holo/settings", None).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/settings",
    operation_id = "holoUpdateSettings",
    request_body = Settings,
    responses((status = 200, body = StatusOnlyResponse)),
    tag = "holo"
)]
pub async fn update_settings(
    State(state): State<Arc<AppState>>,
    Json(body): Json<Settings>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/settings", &body).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/stats",
    operation_id = "holoGetStats",
    responses((status = 200, body = StatsResponse)),
    tag = "holo"
)]
pub async fn get_stats(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<StatsResponse>), AppError> {
    get_typed(&state, "/api/holo/stats", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/stats/channels",
    operation_id = "holoGetChannelStats",
    responses((status = 200, body = ChannelStatsResponse)),
    tag = "holo"
)]
pub async fn get_channel_stats(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<ChannelStatsResponse>), AppError> {
    get_typed(&state, "/api/holo/stats/channels", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/streams/live",
    operation_id = "holoGetLiveStreams",
    params(StreamsQuery),
    responses((status = 200, body = StreamsResponse)),
    tag = "holo"
)]
pub async fn get_live_streams(
    State(state): State<Arc<AppState>>,
    Query(query): Query<StreamsQuery>,
) -> Result<(StatusCode, Json<StreamsResponse>), AppError> {
    let params = query.org.map(|org| vec![("org", org)]);
    get_typed(&state, "/api/holo/streams/live", params).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/streams/upcoming",
    operation_id = "holoGetUpcomingStreams",
    params(StreamsQuery),
    responses((status = 200, body = StreamsResponse)),
    tag = "holo"
)]
pub async fn get_upcoming_streams(
    State(state): State<Arc<AppState>>,
    Query(query): Query<StreamsQuery>,
) -> Result<(StatusCode, Json<StreamsResponse>), AppError> {
    let params = query.org.map(|org| vec![("org", org)]);
    get_typed(&state, "/api/holo/streams/upcoming", params).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/milestones",
    operation_id = "holoGetMilestones",
    params(MilestonesQuery),
    responses((status = 200, body = MilestonesResponse)),
    tag = "holo"
)]
pub async fn get_milestones(
    State(state): State<Arc<AppState>>,
    Query(query): Query<MilestonesQuery>,
) -> Result<(StatusCode, Json<MilestonesResponse>), AppError> {
    let mut params = Vec::new();
    if let Some(limit) = query.limit {
        params.push(("limit", limit.to_string()));
    }
    if let Some(offset) = query.offset {
        params.push(("offset", offset.to_string()));
    }
    if let Some(channel_id) = query.channel_id {
        params.push(("channelId", channel_id));
    }
    if let Some(member_name) = query.member_name {
        params.push(("memberName", member_name));
    }
    let query = if params.is_empty() {
        None
    } else {
        Some(params)
    };
    get_typed(&state, "/api/holo/milestones", query).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/milestones/near",
    operation_id = "holoGetNearMilestones",
    params(NearMilestonesQuery),
    responses((status = 200, body = NearMilestonesResponse)),
    tag = "holo"
)]
pub async fn get_near_milestones(
    State(state): State<Arc<AppState>>,
    Query(query): Query<NearMilestonesQuery>,
) -> Result<(StatusCode, Json<NearMilestonesResponse>), AppError> {
    let params = query
        .threshold
        .map(|threshold| vec![("threshold", threshold.to_string())]);
    get_typed(&state, "/api/holo/milestones/near", params).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/milestones/stats",
    operation_id = "holoGetMilestoneStats",
    responses((status = 200, body = MilestoneStatsResponse)),
    tag = "holo"
)]
pub async fn get_milestone_stats(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<MilestoneStatsResponse>), AppError> {
    get_typed(&state, "/api/holo/milestones/stats", None).await
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::auth::rate_limiter::LoginRateLimiter;
    use crate::auth::session::ValkeySessionStore;
    use crate::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
    use crate::holo::client::HoloApiClient;
    use crate::status::{StatusCollector, SystemStats};
    use axum::response::IntoResponse;
    use deadpool_redis::Runtime;
    use std::net::SocketAddr;
    use tokio::net::TcpListener;

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
            holo_bot_url: base_url.clone(),
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
        };

        let pool = deadpool_redis::Config::from_url(format!("redis://{}", config.valkey_url))
            .create_pool(Some(Runtime::Tokio1))
            .expect("valkey pool creation failed");
        let sessions = ValkeySessionStore::new(pool, config.session.clone());
        let rate_limiter = Arc::new(LoginRateLimiter::new());
        let status_collector = StatusCollector::new(vec![], env!("CARGO_PKG_VERSION"));
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
        use axum::response::IntoResponse;
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
                "/api/holo/stats/channels" => Json(json!({ "status": "ok", "stats": { "ch-1": { "ChannelID": "ch-1", "ChannelTitle": "Mio", "SubscriberCount": 100, "VideoCount": 10, "ViewCount": 1000 } } })).into_response(),
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
    async fn test_holo_handler_invalid_json_returns_502() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr: SocketAddr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let app = axum::Router::new().route(
                "/api/holo/members",
                axum::routing::get(|| async { (StatusCode::OK, "not-json") }),
            );
            axum::serve(listener, app).await.unwrap();
        });

        let state = test_state(format!("http://{addr}"));
        let error = get_members(State(state))
            .await
            .expect_err("expected proxy error");
        assert_eq!(error.into_response().status(), StatusCode::BAD_GATEWAY);
    }

    #[tokio::test]
    async fn test_holo_handler_upstream_5xx_returns_502() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr: SocketAddr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let app = axum::Router::new().route(
                "/api/holo/alarms",
                axum::routing::get(|| async { StatusCode::INTERNAL_SERVER_ERROR }),
            );
            axum::serve(listener, app).await.unwrap();
        });

        let state = test_state(format!("http://{addr}"));
        let error = get_alarms(State(state))
            .await
            .expect_err("expected proxy error");
        assert_eq!(error.into_response().status(), StatusCode::BAD_GATEWAY);
    }
}
