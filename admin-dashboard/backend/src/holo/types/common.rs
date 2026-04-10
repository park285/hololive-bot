use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema)]
pub struct StatusOnlyResponse {
    pub status: String,
    pub message: Option<String>,
}
