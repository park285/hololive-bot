use std::sync::Arc;

use axum::Json;
use axum::extract::{Query, State};
use axum::http::StatusCode;

use crate::error::AppError;
use crate::state::AppState;

use super::helpers::get_typed;
use super::types::{
    AlarmsResponse, ChannelStatsQuery, ChannelStatsResponse, MembersResponse,
    MilestoneStatsResponse, MilestonesQuery, MilestonesResponse, NearMilestonesQuery,
    NearMilestonesResponse, RoomsResponse, SettingsResponse, StatsResponse, StreamsQuery,
    StreamsResponse, YouTubeCommunityShortsOpsResponse,
};

#[utoipa::path(
    get,
    path = "/admin/api/holo/alarms",
    operation_id = "holoGetAlarms",
    responses(
        (status = 200, body = AlarmsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_alarms(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<AlarmsResponse>), AppError> {
    get_typed(&state, "/api/holo/alarms", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/members",
    operation_id = "holoGetMembers",
    responses(
        (status = 200, body = MembersResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_members(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<MembersResponse>), AppError> {
    get_typed(&state, "/api/holo/members", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/rooms",
    operation_id = "holoGetRooms",
    responses(
        (status = 200, body = RoomsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_rooms(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<RoomsResponse>), AppError> {
    get_typed(&state, "/api/holo/rooms", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/settings",
    operation_id = "holoGetSettings",
    responses(
        (status = 200, body = SettingsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_settings(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<SettingsResponse>), AppError> {
    get_typed(&state, "/api/holo/settings", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/stats",
    operation_id = "holoGetStats",
    responses(
        (status = 200, body = StatsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
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
    params(ChannelStatsQuery),
    operation_id = "holoGetChannelStats",
    responses(
        (status = 200, body = ChannelStatsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_channel_stats(
    State(state): State<Arc<AppState>>,
    Query(query): Query<ChannelStatsQuery>,
) -> Result<(StatusCode, Json<ChannelStatsResponse>), AppError> {
    let (status, Json(mut response)) = get_typed(&state, "/api/holo/stats/channels", None).await?;

    if let Some(limit) = query.limit {
        trim_channel_stats(&mut response, limit);
    }

    Ok((status, Json(response)))
}

fn trim_channel_stats(response: &mut ChannelStatsResponse, limit: usize) {
    if response.stats.len() <= limit {
        return;
    }

    let mut sorted_stats = response.stats.drain().collect::<Vec<_>>();
    sorted_stats
        .sort_by(|(_, left), (_, right)| right.subscriber_count.cmp(&left.subscriber_count));
    response.stats = sorted_stats.into_iter().take(limit).collect();
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/stats/youtube/community-shorts",
    operation_id = "holoGetYouTubeCommunityShortsOps",
    responses(
        (status = 200, body = YouTubeCommunityShortsOpsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_youtube_community_shorts_ops(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<YouTubeCommunityShortsOpsResponse>), AppError> {
    get_typed(&state, "/api/holo/stats/youtube/community-shorts", None).await
}

#[utoipa::path(
    get,
    path = "/admin/api/holo/streams/live",
    operation_id = "holoGetLiveStreams",
    params(StreamsQuery),
    responses(
        (status = 200, body = StreamsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
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
    responses(
        (status = 200, body = StreamsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
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
    responses(
        (status = 200, body = MilestonesResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
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
    responses(
        (status = 200, body = NearMilestonesResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
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
    responses(
        (status = 200, body = MilestoneStatsResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn get_milestone_stats(
    State(state): State<Arc<AppState>>,
) -> Result<(StatusCode, Json<MilestoneStatsResponse>), AppError> {
    get_typed(&state, "/api/holo/milestones/stats", None).await
}
