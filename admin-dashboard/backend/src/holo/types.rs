use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use utoipa::{IntoParams, ToSchema};

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct StatusOnlyResponse {
    pub status: String,
    pub message: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct Alarm {
    pub room_id: String,
    pub room_name: String,
    pub user_id: String,
    pub user_name: String,
    pub channel_id: String,
    pub member_name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct AlarmsResponse {
    pub status: String,
    pub alarms: Vec<Alarm>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
#[allow(clippy::struct_field_names)]
pub struct DeleteAlarmRequest {
    pub room_id: String,
    pub user_id: String,
    pub channel_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct RoomNameUpdateRequest {
    pub room_id: String,
    pub room_name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct UserNameUpdateRequest {
    pub user_id: String,
    pub user_name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct Aliases {
    pub ko: Vec<String>,
    pub ja: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct Member {
    pub id: i64,
    pub channel_id: String,
    pub name: String,
    pub aliases: Aliases,
    pub name_ja: Option<String>,
    pub name_ko: Option<String>,
    pub is_graduated: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct MembersResponse {
    pub status: String,
    pub members: Vec<Member>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct AddMemberRequest {
    pub name: String,
    pub channel_id: String,
    pub aliases: Aliases,
    pub name_ja: Option<String>,
    pub name_ko: Option<String>,
    pub is_graduated: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct AddAliasRequest {
    #[schema(value_type = String, example = "ko")]
    pub r#type: String,
    pub alias: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct RemoveAliasRequest {
    #[schema(value_type = String, example = "ja")]
    pub r#type: String,
    pub alias: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct SetGraduationRequest {
    pub is_graduated: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct UpdateChannelRequest {
    pub channel_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct UpdateMemberNameRequest {
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct RoomsResponse {
    pub status: String,
    pub rooms: Vec<String>,
    pub acl_enabled: bool,
    pub acl_mode: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct AddRoomRequest {
    pub room: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct RemoveRoomRequest {
    pub room: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct SetAclRequest {
    pub enabled: Option<bool>,
    pub mode: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct SetAclResponse {
    pub status: String,
    pub enabled: bool,
    pub mode: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct Settings {
    pub alarm_advance_minutes: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct SettingsResponse {
    pub status: String,
    pub settings: Settings,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct StatsResponse {
    pub status: String,
    pub members: i32,
    pub alarms: i32,
    pub rooms: i32,
    pub version: String,
    pub uptime: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct ChannelStat {
    #[serde(rename = "ChannelID")]
    pub channel_id: String,
    #[serde(rename = "ChannelTitle")]
    pub channel_title: String,
    #[serde(rename = "SubscriberCount")]
    pub subscriber_count: i64,
    #[serde(rename = "VideoCount")]
    pub video_count: i64,
    #[serde(rename = "ViewCount")]
    pub view_count: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct ChannelStatsResponse {
    pub status: String,
    pub stats: HashMap<String, ChannelStat>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct Stream {
    pub id: String,
    pub title: String,
    pub status: String,
    pub channel_name: Option<String>,
    pub channel_id: String,
    pub link: Option<String>,
    pub thumbnail: Option<String>,
    pub start_scheduled: Option<String>,
    pub start_actual: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct StreamsResponse {
    pub status: String,
    pub org: Option<String>,
    pub streams: Vec<Stream>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct Milestone {
    pub channel_id: String,
    pub member_name: String,
    pub r#type: String,
    pub value: i64,
    pub achieved_at: String,
    pub notified: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct MilestonesResponse {
    pub status: String,
    pub milestones: Vec<Milestone>,
    pub total: i64,
    pub limit: i64,
    pub offset: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct NearMilestone {
    pub channel_id: String,
    pub member_name: String,
    pub current_subs: i64,
    pub next_milestone: i64,
    pub remaining: i64,
    pub progress_pct: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct NearMilestonesResponse {
    pub status: String,
    pub members: Vec<NearMilestone>,
    pub count: i64,
    pub threshold: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct MilestoneStats {
    pub total_achieved: i64,
    pub total_near_milestone: i64,
    pub recent_achievements: i64,
    pub not_notified_count: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct MilestoneStatsResponse {
    pub status: String,
    pub stats: MilestoneStats,
}

#[derive(Debug, Deserialize, IntoParams)]
pub struct StreamsQuery {
    pub org: Option<String>,
}

#[derive(Debug, Deserialize, IntoParams)]
#[serde(rename_all = "camelCase")]
pub struct MilestonesQuery {
    pub limit: Option<i64>,
    pub offset: Option<i64>,
    pub channel_id: Option<String>,
    pub member_name: Option<String>,
}

#[derive(Debug, Deserialize, IntoParams)]
pub struct NearMilestonesQuery {
    pub threshold: Option<f64>,
}
