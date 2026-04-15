use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct Alarm {
    pub room_id: String,
    pub room_name: String,
    #[serde(default)]
    #[schema(required = true)]
    pub user_id: String,
    #[serde(default)]
    #[schema(required = true)]
    pub user_name: String,
    pub channel_id: String,
    pub member_name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct AlarmsResponse {
    #[serde(default = "default_ok_status")]
    #[schema(required = true)]
    pub status: String,
    pub alarms: Vec<Alarm>,
}

fn default_ok_status() -> String {
    "ok".to_string()
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
    #[serde(default)]
    #[schema(required = true)]
    pub user_id: String,
    #[serde(default)]
    #[schema(required = true)]
    pub user_name: String,
}
