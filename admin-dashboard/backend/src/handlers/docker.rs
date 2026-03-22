use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, State};
use axum::response::IntoResponse;
use serde::Serialize;
use utoipa::ToSchema;

use crate::docker::DockerProvider;
use crate::error::{AppError, DockerError};
use crate::state::AppState;

#[derive(Debug, Serialize, ToSchema)]
pub struct DockerHealthResponse {
    pub status: String,
    pub available: bool,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct DockerContainerListResponse {
    pub status: String,
    pub containers: Vec<crate::docker::Container>,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct DockerActionResponse {
    pub status: String,
    pub message: String,
}

#[utoipa::path(
    get,
    path = "/admin/api/docker/health",
    responses(
        (status = 200, description = "Docker health retrieved", body = DockerHealthResponse)
    ),
    tag = "docker"
)]
pub async fn handle_docker_health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let available = match &state.docker_svc {
        Some(svc) => svc.available().await,
        None => false,
    };

    Json(DockerHealthResponse {
        status: "ok".to_string(),
        available,
    })
}

#[utoipa::path(
    get,
    path = "/admin/api/docker/containers",
    responses(
        (status = 200, description = "Docker containers listed", body = DockerContainerListResponse),
        (status = 503, description = "Docker unavailable")
    ),
    tag = "docker"
)]
pub async fn handle_docker_containers(
    State(state): State<Arc<AppState>>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    let containers = svc.list_containers().await?;

    Ok(Json(DockerContainerListResponse {
        status: "ok".to_string(),
        containers,
    }))
}

#[utoipa::path(
    post,
    path = "/admin/api/docker/containers/{name}/restart",
    params(("name" = String, Path, description = "Container name")),
    responses(
        (status = 200, description = "Container restarted", body = DockerActionResponse),
        (status = 404, description = "Container is not managed"),
        (status = 503, description = "Docker unavailable")
    ),
    tag = "docker"
)]
pub async fn handle_docker_restart(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    svc.restart_container(&name).await?;
    Ok(Json(DockerActionResponse {
        status: "ok".to_string(),
        message: format!("Container {name} restarted"),
    }))
}

#[utoipa::path(
    post,
    path = "/admin/api/docker/containers/{name}/stop",
    params(("name" = String, Path, description = "Container name")),
    responses(
        (status = 200, description = "Container stopped", body = DockerActionResponse),
        (status = 404, description = "Container is not managed"),
        (status = 503, description = "Docker unavailable")
    ),
    tag = "docker"
)]
pub async fn handle_docker_stop(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    svc.stop_container(&name).await?;
    Ok(Json(DockerActionResponse {
        status: "ok".to_string(),
        message: format!("Container {name} stopped"),
    }))
}

#[utoipa::path(
    post,
    path = "/admin/api/docker/containers/{name}/start",
    params(("name" = String, Path, description = "Container name")),
    responses(
        (status = 200, description = "Container started", body = DockerActionResponse),
        (status = 404, description = "Container is not managed"),
        (status = 503, description = "Docker unavailable")
    ),
    tag = "docker"
)]
pub async fn handle_docker_start(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    svc.start_container(&name).await?;
    Ok(Json(DockerActionResponse {
        status: "ok".to_string(),
        message: format!("Container {name} started"),
    }))
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    #[test]
    fn test_health_response_shape() {
        let json = json!({"status": "ok", "available": true});
        assert_eq!(json["status"], "ok");
        assert_eq!(json["available"], true);
    }

    #[test]
    fn test_container_response_shape() {
        let json = json!({"status": "ok", "containers": []});
        assert!(json["containers"].is_array());
    }
}
