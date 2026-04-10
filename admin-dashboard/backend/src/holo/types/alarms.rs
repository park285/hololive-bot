use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

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
