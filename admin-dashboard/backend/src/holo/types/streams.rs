use serde::{Deserialize, Serialize};
use utoipa::{IntoParams, ToSchema};

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

#[derive(Debug, Deserialize, IntoParams)]
pub struct StreamsQuery {
    pub org: Option<String>,
}
