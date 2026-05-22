use std::sync::Arc;

use axum::Json;
use axum::extract::{Request, State};
use axum::response::IntoResponse;

use crate::auth::SessionId;
use crate::auth::session::SessionProvider;
use crate::error::{AppError, AuthError};
use crate::state::AppState;

use super::{SessionPolicyResponse, SessionStatusResponse, clear_auth_cookies};

#[utoipa::path(
    get,
    path = "/admin/api/auth/session",
    responses(
        (status = 200, description = "Session is valid", body = SessionStatusResponse),
        (status = 401, description = "Unauthorized"),
        (status = 503, description = "Session store unavailable")
    ),
    tag = "auth"
)]
pub async fn handle_session_status(
    State(state): State<Arc<AppState>>,
    req: Request,
) -> Result<impl IntoResponse, AppError> {
    let session_id = req
        .extensions()
        .get::<SessionId>()
        .ok_or(AuthError::Unauthorized)?
        .0
        .clone();

    let session = state
        .sessions
        .get_session(&session_id)
        .await
        .map_err(|_| AuthError::StoreUnavailable)?
        .ok_or(AuthError::Unauthorized)?;

    Ok(Json(SessionStatusResponse {
        status: "ok".to_string(),
        authenticated: true,
        username: state.config.admin_user.clone(),
        absolute_expires_at: session.absolute_expires_at.timestamp(),
        session_policy: SessionPolicyResponse {
            heartbeat_interval_ms: state.config.session.heartbeat_interval.as_millis() as u64,
            idle_timeout_ms: state.config.session.idle_timeout.as_millis() as u64,
            idle_warning_timeout_ms: state.config.session.idle_warning_timeout.as_millis() as u64,
            idle_session_ttl_ms: state.config.session.idle_session_ttl.as_millis() as u64,
            absolute_warning_window_ms: state.config.session.absolute_warning_window.as_millis()
                as u64,
        },
    }))
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/logout",
    responses(
        (status = 200, description = "Logout successful")
    ),
    tag = "auth"
)]
pub async fn handle_logout(State(state): State<Arc<AppState>>, req: Request) -> impl IntoResponse {
    if let Some(session_id) = req.extensions().get::<SessionId>() {
        state.sessions.delete_session(&session_id.0).await;
    }

    let mut response = Json(serde_json::json!({"status": "ok"})).into_response();
    clear_auth_cookies(
        response.headers_mut(),
        crate::auth::middleware::should_set_secure_cookie(
            req.headers(),
            state.config.security.force_https,
        ),
    );
    response
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use axum::body::{Body, to_bytes};
    use axum::extract::State;
    use axum::http::{Request as HttpRequest, StatusCode};
    use axum::response::IntoResponse;

    use crate::auth::SessionId;
    use crate::config::SessionConfig;
    use crate::handlers::auth::SessionStatusResponse;
    use crate::handlers::auth::test_support::{FakeValkey, build_session, test_state};

    use super::*;

    #[tokio::test]
    async fn test_session_status_includes_warning_policy_and_absolute_expiry() {
        let fake_valkey = FakeValkey::start().await;
        let state = test_state(fake_valkey.url());
        let session = build_session(
            "session-status-policy",
            chrono::Utc::now() + chrono::Duration::hours(1),
        );
        let expected_absolute_expiry = session.absolute_expires_at.timestamp();
        fake_valkey.insert_session(&session);

        let mut req = HttpRequest::get("/admin/api/auth/session")
            .body(Body::empty())
            .expect("session status request");
        req.extensions_mut().insert(SessionId(session.id.clone()));

        let response = handle_session_status(State(Arc::clone(&state)), req)
            .await
            .expect("session status response")
            .into_response();

        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), 4096)
            .await
            .expect("session status body");
        let json: serde_json::Value = serde_json::from_slice(&body).expect("session status json");
        assert_eq!(json["status"], "ok");
        assert_eq!(json["absolute_expires_at"], expected_absolute_expiry);
        assert_eq!(
            json["session_policy"]["idle_timeout_ms"].as_u64(),
            Some(SessionConfig::default().idle_timeout.as_millis() as u64)
        );
        assert_eq!(
            json["session_policy"]["idle_warning_timeout_ms"].as_u64(),
            Some(SessionConfig::default().idle_warning_timeout.as_millis() as u64)
        );
        assert_eq!(
            json["session_policy"]["absolute_warning_window_ms"].as_u64(),
            Some(SessionConfig::default().absolute_warning_window.as_millis() as u64)
        );
    }

    #[test]
    fn test_session_status_response_serialize() {
        let resp = SessionStatusResponse {
            status: "ok".to_string(),
            authenticated: true,
            username: "admin".to_string(),
            absolute_expires_at: 1_735_568_988,
            session_policy: SessionPolicyResponse {
                heartbeat_interval_ms: 300_000,
                idle_timeout_ms: 600_000,
                idle_warning_timeout_ms: 540_000,
                idle_session_ttl_ms: 10_000,
                absolute_warning_window_ms: 300_000,
            },
        };
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("authenticated"));
        assert!(json.contains("username"));
        assert!(json.contains("absolute_expires_at"));
        assert!(json.contains("session_policy"));
    }
}
