use std::sync::Arc;

use axum::Json;
use axum::http::StatusCode;
use reqwest::Method;

use crate::error::AppError;
use crate::state::AppState;

pub(super) async fn get_typed<T: serde::de::DeserializeOwned>(
    state: &Arc<AppState>,
    path: &str,
    query: Option<Vec<(&str, String)>>,
) -> Result<(StatusCode, Json<T>), AppError> {
    let (status, payload) = state.holo_api.get(path, query.as_deref()).await?;
    Ok((status, Json(payload)))
}

pub(super) async fn send_typed<B, T>(
    state: &Arc<AppState>,
    method: Method,
    path: &str,
    body: &B,
) -> Result<(StatusCode, Json<T>), AppError>
where
    B: serde::Serialize + ?Sized,
    T: serde::de::DeserializeOwned,
{
    let (status, payload) = state.holo_api.send(method, path, Some(body)).await?;
    Ok((status, Json(payload)))
}
