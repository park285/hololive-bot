use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

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
    #[serde(default)]
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
    #[serde(default)]
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
    #[serde(default)]
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
