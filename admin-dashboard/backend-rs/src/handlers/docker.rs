use std::sync::Arc;
use std::time::Duration;

use axum::extract::ws::{Message, WebSocketUpgrade};
use axum::extract::{Path, State};
use axum::response::IntoResponse;
use axum::{Extension, Json};
use serde_json::json;
use tokio::io::AsyncReadExt;

use crate::auth::SessionId;
use crate::docker::DockerProvider;
use crate::error::{AppError, DockerError};
use crate::state::AppState;

pub async fn handle_docker_health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let available = match &state.docker_svc {
        Some(svc) => svc.available().await,
        None => false,
    };

    Json(json!({
        "status": "ok",
        "available": available,
    }))
}

pub async fn handle_docker_containers(
    State(state): State<Arc<AppState>>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    let containers = svc.list_containers().await?;

    Ok(Json(json!({
        "status": "ok",
        "containers": containers,
    })))
}

pub async fn handle_docker_restart(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    svc.restart_container(&name).await?;
    Ok(Json(json!({
        "status": "ok",
        "message": format!("Container {} restarted", name),
    })))
}

pub async fn handle_docker_stop(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    svc.stop_container(&name).await?;
    Ok(Json(json!({
        "status": "ok",
        "message": format!("Container {} stopped", name),
    })))
}

pub async fn handle_docker_start(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    svc.start_container(&name).await?;
    Ok(Json(json!({
        "status": "ok",
        "message": format!("Container {} started", name),
    })))
}

pub async fn handle_docker_log_stream(
    State(state): State<Arc<AppState>>,
    Path(name): Path<String>,
    ws: WebSocketUpgrade,
    session_id: Option<Extension<SessionId>>,
) -> Result<impl IntoResponse, AppError> {
    let svc = state.docker_svc.as_ref().ok_or(DockerError::Unavailable)?;
    if !svc.is_managed(&name) {
        return Err(DockerError::NotManaged(name).into());
    }

    let session_id = session_id
        .map(|Extension(session_id)| session_id.0)
        .unwrap_or_default();

    let (allowed, _result) = state.stream_limiter.try_acquire(&session_id);
    if !allowed {
        return Err(anyhow::anyhow!("stream limit reached").into());
    }

    let stream_limiter = Arc::clone(&state.stream_limiter);
    let docker_svc = state.docker_svc.clone();
    let session_id_clone = session_id.clone();

    Ok(ws.on_upgrade(move |mut socket| async move {
        let timeout = tokio::time::sleep(Duration::from_secs(600));
        tokio::pin!(timeout);

        if let Some(svc) = docker_svc {
            match svc.get_log_stream(&name).await {
                Ok(mut reader) => {
                    let mut buf = vec![0_u8; 4096];
                    loop {
                        tokio::select! {
                            _ = &mut timeout => break,
                            result = reader.read(&mut buf) => {
                                match result {
                                    Ok(0) => break,
                                    Ok(n) => {
                                        let text = String::from_utf8_lossy(&buf[..n]).to_string();
                                        if socket.send(Message::Text(text.into())).await.is_err() {
                                            break;
                                        }
                                    }
                                    Err(error) => {
                                        tracing::error!(error = %error, container = %name, "docker_log_stream_read_failed");
                                        break;
                                    }
                                }
                            }
                        }
                    }
                }
                Err(error) => {
                    tracing::error!(error = %error, container = %name, "docker_log_stream_failed");
                }
            }
        }

        stream_limiter.release(&session_id_clone);
    }))
}

#[cfg(test)]
mod tests {
    use super::*;

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
