use serde::{Deserialize, Serialize};
use utoipa::{IntoParams, ToSchema};

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
#[serde(rename_all = "camelCase")]
pub struct CalendarMember {
    pub id: i64,
    pub channel_id: String,
    pub name: String,
    pub name_ko: Option<String>,
    pub short_korean_name: Option<String>,
    pub photo: Option<String>,
    pub org: Option<String>,
    pub suborg: Option<String>,
    #[serde(default)]
    pub is_graduated: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct CalendarEntry {
    pub kind: String,
    pub member: CalendarMember,
    pub day: i32,
    pub ordinal: Option<i32>,
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct CalendarResponse {
    pub status: String,
    pub month: i32,
    pub year: i32,
    pub entries: Vec<CalendarEntry>,
}

#[derive(Debug, Deserialize, IntoParams)]
pub struct CalendarQuery {
    pub month: Option<i32>,
    pub year: Option<i32>,
}
