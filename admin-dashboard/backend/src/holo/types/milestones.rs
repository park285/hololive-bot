use serde::{Deserialize, Serialize};
use utoipa::{IntoParams, ToSchema};

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
