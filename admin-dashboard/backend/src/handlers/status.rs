use std::collections::HashSet;
use std::sync::Arc;

use axum::Json;
use axum::extract::ws::{Message, WebSocket};
use axum::extract::{Request, State, WebSocketUpgrade};
use axum::response::IntoResponse;

use crate::config::SecurityMode;

#[utoipa::path(
    get,
    path = "/admin/api/status",
    responses(
        (status = 200, description = "Aggregated status retrieved")
    ),
    tag = "status"
)]
pub async fn handle_aggregated_status(
    State(state): State<Arc<crate::state::AppState>>,
) -> impl IntoResponse {
    let status = state.status_collector.collect().await;
    Json(status)
}

pub async fn handle_system_stats_stream(
    State(app_state): State<Arc<crate::state::AppState>>,
    ws: WebSocketUpgrade,
    req: Request,
) -> Result<impl IntoResponse, crate::error::AppError> {
    let origin = req
        .headers()
        .get(axum::http::header::ORIGIN)
        .and_then(|v| v.to_str().ok());

    let allowed_origins: HashSet<String> = app_state
        .config
        .security
        .allowed_origins
        .iter()
        .cloned()
        .collect();
    verify_ws_origin(
        origin,
        &allowed_origins,
        app_state.config.security.ws_origin_mode,
    )?;

    let mut rx = app_state.stats_tx.subscribe();

    Ok(ws.on_upgrade(move |mut socket: WebSocket| async move {
        loop {
            match rx.recv().await {
                Ok(system_stats) => {
                    let json = serde_json::to_string(&system_stats).unwrap_or_default();
                    if socket.send(Message::Text(json.into())).await.is_err() {
                        break;
                    }
                }
                Err(tokio::sync::broadcast::error::RecvError::Closed) => break,
                Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => {}
            }
        }
    }))
}

pub fn verify_ws_origin<S>(
    origin: Option<&str>,
    allowed: &HashSet<String, S>,
    mode: SecurityMode,
) -> Result<(), crate::error::AppError>
where
    S: std::hash::BuildHasher,
{
    if mode == SecurityMode::Off {
        return Ok(());
    }

    match origin {
        None => {
            if mode == SecurityMode::Monitor {
                tracing::warn!(mode = "monitor", "ws_origin_missing");
                return Ok(());
            }
            Err(crate::error::AuthError::CsrfViolation.into())
        }
        Some(o) => {
            if allowed.contains(o) {
                Ok(())
            } else {
                if mode == SecurityMode::Monitor {
                    tracing::warn!(origin = o, mode = "monitor", "ws_origin_rejected");
                    return Ok(());
                }
                Err(crate::error::AuthError::CsrfViolation.into())
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::SecurityMode;
    use std::collections::HashSet;

    fn allowed_origins() -> HashSet<String> {
        let mut s = HashSet::new();
        s.insert("https://admin.capu.blog".to_string());
        s.insert("http://localhost:5173".to_string());
        s
    }

    #[test]
    fn test_ws_origin_enforce_valid() {
        let result = verify_ws_origin(
            Some("https://admin.capu.blog"),
            &allowed_origins(),
            SecurityMode::Enforce,
        );
        assert!(result.is_ok());
    }

    #[test]
    fn test_ws_origin_enforce_invalid() {
        let result = verify_ws_origin(
            Some("https://evil.com"),
            &allowed_origins(),
            SecurityMode::Enforce,
        );
        assert!(result.is_err());
    }

    #[test]
    fn test_ws_origin_enforce_missing() {
        let result = verify_ws_origin(None, &allowed_origins(), SecurityMode::Enforce);
        assert!(result.is_err());
    }

    #[test]
    fn test_ws_origin_monitor_invalid_allowed() {
        let result = verify_ws_origin(
            Some("https://evil.com"),
            &allowed_origins(),
            SecurityMode::Monitor,
        );
        assert!(result.is_ok());
    }

    #[test]
    fn test_ws_origin_off_skips() {
        let result = verify_ws_origin(
            Some("https://evil.com"),
            &allowed_origins(),
            SecurityMode::Off,
        );
        assert!(result.is_ok());
    }

    #[test]
    fn test_ws_origin_off_missing_ok() {
        let result = verify_ws_origin(None, &allowed_origins(), SecurityMode::Off);
        assert!(result.is_ok());
    }
}
