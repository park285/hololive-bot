use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct YouTubeCommunityShortsOpsOverview {
    pub channel_count: i64,
    pub detected_post_count: i64,
    pub alarm_sent_post_count: i64,
    pub success_post_count: i64,
    pub failed_post_count: i64,
    pub detected_unsent_post_count: i64,
    pub pending_post_count: i64,
    pub latency_measured_post_count: i64,
    pub within_target_post_count: i64,
    pub exceeded_post_count: i64,
    pub community_detected_post_count: i64,
    pub shorts_detected_post_count: i64,
    pub community_exceeded_post_count: i64,
    pub shorts_exceeded_post_count: i64,
    pub average_latency_millis: Option<i64>,
    pub max_latency_millis: Option<i64>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct YouTubeCommunityShortsOpsChannel {
    pub channel_id: String,
    pub member_name: Option<String>,
    pub earliest_observed_at: Option<String>,
    pub latest_observed_at: Option<String>,
    pub detected_post_count: i64,
    pub alarm_sent_post_count: i64,
    pub success_post_count: i64,
    pub failed_post_count: i64,
    pub detected_unsent_post_count: i64,
    pub pending_post_count: i64,
    pub latency_measured_post_count: i64,
    pub within_target_post_count: i64,
    pub exceeded_post_count: i64,
    pub community_post_count: i64,
    pub shorts_post_count: i64,
    pub average_latency_millis: Option<i64>,
    pub max_latency_millis: Option<i64>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct YouTubeCommunityShortsOpsResponse {
    pub status: String,
    pub generated_at: String,
    pub window_start: String,
    pub window_end: String,
    pub window_hours: i64,
    pub observed_at_basis: String,
    pub sla_threshold_millis: i64,
    pub overview: YouTubeCommunityShortsOpsOverview,
    pub channels: Vec<YouTubeCommunityShortsOpsChannel>,
}
