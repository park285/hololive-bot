use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use utoipa::IntoParams;
use utoipa::ToSchema;

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

#[derive(Debug, Deserialize, IntoParams)]
pub struct ChannelStatsQuery {
    #[param(minimum = 0, maximum = 500)]
    pub limit: Option<usize>,
}
