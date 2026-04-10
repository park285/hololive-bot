use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

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
