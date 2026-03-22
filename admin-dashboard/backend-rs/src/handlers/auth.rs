use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use axum::Json;
use axum::extract::{ConnectInfo, Request, State};
use axum::response::IntoResponse;
use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

use crate::auth::SessionId;
use crate::auth::session::SessionProvider;
use crate::error::{AppError, AuthError};
use crate::state::AppState;

#[derive(Deserialize, ToSchema)]
pub struct LoginRequest {
    pub username: String,
    pub password: String,
}

#[derive(Serialize, ToSchema)]
pub struct LoginResponse {
    pub status: String,
    pub message: String,
    pub csrf_token: String,
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/login",
    request_body = LoginRequest,
    responses(
        (status = 200, description = "Login successful", body = LoginResponse),
        (status = 401, description = "Authentication failed"),
        (status = 429, description = "Rate limited")
    ),
    tag = "auth"
)]
pub async fn handle_login(
    State(state): State<Arc<AppState>>,
    ConnectInfo(addr): ConnectInfo<SocketAddr>,
    Json(req): Json<LoginRequest>,
) -> Result<impl IntoResponse, AppError> {
    let ip = addr.ip().to_string();

    let (allowed, remaining) = state.rate_limiter.is_allowed(&ip);
    if !allowed {
        return Err(AuthError::RateLimited {
            retry_after_secs: remaining.as_secs(),
        }
        .into());
    }

    if req.username != state.config.admin_user {
        let count = state.rate_limiter.record_failure(&ip);
        let delay = std::cmp::min(count as u64 * 500, 3000);
        tokio::time::sleep(Duration::from_millis(delay)).await;
        return Err(AuthError::Unauthorized.into());
    }

    let valid = bcrypt::verify(&req.password, &state.config.admin_pass_hash).unwrap_or(false);
    if !valid {
        let count = state.rate_limiter.record_failure(&ip);
        let delay = std::cmp::min(count as u64 * 500, 3000);
        tokio::time::sleep(Duration::from_millis(delay)).await;
        return Err(AuthError::Unauthorized.into());
    }

    state.rate_limiter.record_success(&ip);
    let session = state
        .sessions
        .create_session()
        .await
        .map_err(|_| AuthError::StoreUnavailable)?;

    let signed = crate::auth::sign_session_id(&session.id, &state.config.session_secret);
    let csrf_token = crate::middleware::csrf::new_csrf_token(
        &session.id,
        &state.config.session_secret,
    );

    let mut response = Json(LoginResponse {
        status: "ok".to_string(),
        message: "Login successful".to_string(),
        csrf_token: csrf_token.clone(),
    })
    .into_response();

    crate::auth::middleware::set_session_cookie(
        response.headers_mut(),
        "admin_session",
        &signed,
        state.config.security.force_https,
    );
    crate::auth::middleware::set_csrf_cookie(
        response.headers_mut(),
        &csrf_token,
        state.config.security.force_https,
    );

    Ok(response)
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/logout",
    responses(
        (status = 200, description = "Logout successful")
    ),
    tag = "auth"
)]
pub async fn handle_logout(
    State(state): State<Arc<AppState>>,
    req: Request,
) -> impl IntoResponse {
    if let Some(session_id) = req.extensions().get::<SessionId>() {
        state.sessions.delete_session(&session_id.0).await;
    }

    let mut response = Json(serde_json::json!({"status": "ok"})).into_response();
    crate::auth::middleware::set_clear_cookie(
        response.headers_mut(),
        "admin_session",
        state.config.security.force_https,
    );
    crate::auth::middleware::set_clear_cookie(
        response.headers_mut(),
        "csrf_token",
        false,
    );
    response
}

#[derive(Deserialize, ToSchema)]
pub struct HeartbeatRequest {
    #[serde(default)]
    pub idle: bool,
}

#[derive(Serialize, ToSchema)]
pub struct HeartbeatResponse {
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub rotated: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub csrf_token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub idle_rejected: Option<bool>,
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/heartbeat",
    request_body = HeartbeatRequest,
    responses(
        (status = 200, description = "Heartbeat processed", body = HeartbeatResponse),
        (status = 401, description = "Session expired or unauthorized")
    ),
    tag = "auth"
)]
pub async fn handle_heartbeat(
    State(state): State<Arc<AppState>>,
    req: Request,
) -> Result<impl IntoResponse, AppError> {
    let session_id = req
        .extensions()
        .get::<SessionId>()
        .map(|s| s.0.clone())
        .ok_or(AuthError::Unauthorized)?;

    let body = axum::body::to_bytes(req.into_body(), 1024)
        .await
        .map_err(|e| anyhow::anyhow!("body read failed: {}", e))?;
    let hb: HeartbeatRequest =
        serde_json::from_slice(&body).unwrap_or(HeartbeatRequest { idle: false });

    let (refreshed, absolute_expired) = state
        .sessions
        .refresh_session_with_validation(&session_id, hb.idle)
        .await
        .map_err(|_| AuthError::StoreUnavailable)?;

    if absolute_expired {
        let mut response = axum::http::StatusCode::UNAUTHORIZED.into_response();
        crate::auth::middleware::set_clear_cookie(
            response.headers_mut(),
            "admin_session",
            state.config.security.force_https,
        );
        crate::auth::middleware::set_clear_cookie(
            response.headers_mut(),
            "csrf_token",
            false,
        );
        return Ok(response);
    }

    if hb.idle && !refreshed {
        return Ok(Json(HeartbeatResponse {
            status: "idle".to_string(),
            idle_rejected: Some(true),
            rotated: None,
            csrf_token: None,
        })
        .into_response());
    }

    if !refreshed {
        let mut response = axum::http::StatusCode::UNAUTHORIZED.into_response();
        crate::auth::middleware::set_clear_cookie(
            response.headers_mut(),
            "admin_session",
            state.config.security.force_https,
        );
        crate::auth::middleware::set_clear_cookie(
            response.headers_mut(),
            "csrf_token",
            false,
        );
        return Ok(response);
    }

    if state.config.session.token_rotation_enabled {
        if let Ok(Some(new_session)) = state.sessions.rotate_session(&session_id).await {
            let new_signed =
                crate::auth::sign_session_id(&new_session.id, &state.config.session_secret);
            let new_csrf = crate::middleware::csrf::new_csrf_token(
                &new_session.id,
                &state.config.session_secret,
            );

            let mut response = Json(HeartbeatResponse {
                status: "ok".to_string(),
                rotated: Some(true),
                csrf_token: Some(new_csrf.clone()),
                idle_rejected: None,
            })
            .into_response();

            crate::auth::middleware::set_session_cookie(
                response.headers_mut(),
                "admin_session",
                &new_signed,
                state.config.security.force_https,
            );
            crate::auth::middleware::set_csrf_cookie(
                response.headers_mut(),
                &new_csrf,
                state.config.security.force_https,
            );
            return Ok(response);
        }
    }

    Ok(Json(HeartbeatResponse {
        status: "ok".to_string(),
        rotated: None,
        csrf_token: None,
        idle_rejected: None,
    })
    .into_response())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_login_request_deserialize() {
        let json = r#"{"username":"admin","password":"pass"}"#;
        let req: LoginRequest = serde_json::from_str(json).unwrap();
        assert_eq!(req.username, "admin");
    }

    #[test]
    fn test_login_response_serialize() {
        let resp = LoginResponse {
            status: "ok".to_string(),
            message: "Login successful".to_string(),
            csrf_token: "token123".to_string(),
        };
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("csrf_token"));
    }

    #[test]
    fn test_heartbeat_request_defaults() {
        let json = r#"{}"#;
        let req: HeartbeatRequest = serde_json::from_str(json).unwrap();
        assert!(!req.idle);
    }

    #[test]
    fn test_heartbeat_response_skip_none() {
        let resp = HeartbeatResponse {
            status: "ok".to_string(),
            rotated: None,
            csrf_token: None,
            idle_rejected: None,
        };
        let json = serde_json::to_string(&resp).unwrap();
        assert!(!json.contains("rotated"));
        assert!(!json.contains("csrf_token"));
    }
}
