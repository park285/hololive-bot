use std::sync::Arc;

use axum::Json;
use axum::extract::{Request, State};
use axum::response::IntoResponse;
use serde::{Deserialize, Serialize};
use tracing::warn;
use utoipa::ToSchema;

use crate::auth::SessionId;
use crate::auth::session::{Session, SessionProvider, SessionRefreshResult};
use crate::error::{ApiError, AppError, AuthError};
use crate::state::AppState;

use super::truncate_session_id_for_log;

#[derive(Debug, Deserialize, ToSchema)]
pub struct HeartbeatRequest {
    #[serde(default)]
    pub idle: bool,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct HeartbeatResponse {
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub rotated: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub absolute_expires_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub csrf_token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub idle_rejected: Option<bool>,
}

fn parse_heartbeat_request(body: &[u8]) -> Result<HeartbeatRequest, AppError> {
    if body.is_empty() {
        return Ok(HeartbeatRequest { idle: false });
    }

    serde_json::from_slice(body).map_err(|_| {
        ApiError::BadRequest {
            message: "Invalid heartbeat payload",
        }
        .into()
    })
}

fn session_cookie_max_age_secs(session: &Session) -> u64 {
    session
        .expires_at
        .signed_duration_since(chrono::Utc::now())
        .to_std()
        .map_or(1, |duration| duration.as_secs().max(1))
}

fn build_heartbeat_session_response(
    state: &AppState,
    secure_cookie: bool,
    session: &Session,
    rotated: bool,
) -> axum::response::Response {
    let signed = crate::auth::sign_session_id(&session.id, &state.config.session_secret);
    let csrf_token =
        crate::middleware::csrf::new_csrf_token(&session.id, &state.config.session_secret);

    let mut response = Json(HeartbeatResponse {
        status: "ok".to_string(),
        rotated: Some(rotated),
        absolute_expires_at: Some(session.absolute_expires_at.timestamp()),
        csrf_token: Some(csrf_token.clone()),
        idle_rejected: None,
    })
    .into_response();

    crate::auth::middleware::set_session_cookie(
        response.headers_mut(),
        "admin_session",
        &signed,
        session_cookie_max_age_secs(session),
        secure_cookie,
    );
    crate::auth::middleware::set_csrf_cookie(response.headers_mut(), &csrf_token, secure_cookie);

    response
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/heartbeat",
    request_body = HeartbeatRequest,
    responses(
        (status = 200, description = "Heartbeat processed", body = HeartbeatResponse),
        (status = 400, description = "Invalid heartbeat payload", body = crate::error::ErrorResponse),
        (status = 401, description = "Session expired or unauthorized"),
        (status = 503, description = "Session store unavailable")
    ),
    tag = "auth"
)]
pub async fn handle_heartbeat(
    State(state): State<Arc<AppState>>,
    req: Request,
) -> Result<impl IntoResponse, AppError> {
    let secure_cookie = crate::auth::middleware::should_set_secure_cookie(
        req.headers(),
        state.config.security.force_https,
    );
    let session_id = req
        .extensions()
        .get::<SessionId>()
        .map(|s| s.0.clone())
        .ok_or(AuthError::Unauthorized)?;

    let body = axum::body::to_bytes(req.into_body(), 1024)
        .await
        .map_err(|e| anyhow::anyhow!("body read failed: {e}"))?;
    let hb = parse_heartbeat_request(&body)?;

    let refresh_result = state
        .sessions
        .refresh_session_with_validation(&session_id, hb.idle)
        .await
        .map_err(|_| AuthError::StoreUnavailable)?;

    let active_session = match refresh_result {
        SessionRefreshResult::Refreshed(session) => session,
        SessionRefreshResult::Rotated(session) => {
            return Ok(build_heartbeat_session_response(
                &state,
                secure_cookie,
                &session,
                true,
            ));
        }
        SessionRefreshResult::IdleShortened => {
            return Ok(Json(HeartbeatResponse {
                status: "idle".to_string(),
                idle_rejected: Some(true),
                rotated: None,
                absolute_expires_at: None,
                csrf_token: None,
            })
            .into_response());
        }
        SessionRefreshResult::AbsoluteExpired => {
            return Ok(
                crate::auth::middleware::auth_error_response_with_cookie_clear(
                    AuthError::AbsoluteExpired,
                    secure_cookie,
                ),
            );
        }
        SessionRefreshResult::Missing | SessionRefreshResult::NotRefreshable => {
            return Ok(
                crate::auth::middleware::auth_error_response_with_cookie_clear(
                    AuthError::Unauthorized,
                    secure_cookie,
                ),
            );
        }
    };

    if state.config.session.token_rotation_enabled {
        match state.sessions.rotate_session(&session_id).await {
            Ok(Some(new_session)) => {
                return Ok(build_heartbeat_session_response(
                    &state,
                    secure_cookie,
                    &new_session,
                    true,
                ));
            }
            Ok(None) => {}
            Err(err) => {
                warn!(
                    session_id = %truncate_session_id_for_log(&session_id),
                    error = %err,
                    "session rotation failed"
                );
                return Err(AuthError::StoreUnavailable.into());
            }
        }
    }

    Ok(Json(HeartbeatResponse {
        status: "ok".to_string(),
        rotated: None,
        absolute_expires_at: Some(active_session.absolute_expires_at.timestamp()),
        csrf_token: None,
        idle_rejected: None,
    })
    .into_response())
}
