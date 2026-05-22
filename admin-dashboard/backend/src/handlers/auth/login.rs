use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use axum::Json;
use axum::extract::{ConnectInfo, State};
use axum::http::HeaderMap;
use axum::response::IntoResponse;
use tracing::{info, warn};

use crate::auth::session::SessionProvider;
use crate::error::{AppError, AuthError};
use crate::state::AppState;

use super::{LoginRequest, LoginResponse, client_ip_for_rate_limit, constant_time_str_eq};

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
    headers: HeaderMap,
    Json(req): Json<LoginRequest>,
) -> Result<impl IntoResponse, AppError> {
    let ip = client_ip_for_rate_limit(&headers, addr);
    let forwarded_for = headers
        .get("x-forwarded-for")
        .and_then(|value| value.to_str().ok())
        .unwrap_or("-");

    let (allowed, remaining) = state.rate_limiter.is_allowed(&ip);
    if !allowed {
        warn!(
            ip = %ip,
            forwarded_for,
            retry_after_secs = remaining.as_secs(),
            "admin login rate limited"
        );
        return Err(AuthError::RateLimited {
            retry_after_secs: remaining.as_secs(),
        }
        .into());
    }

    let username_matches = constant_time_str_eq(&req.username, &state.config.admin_user);
    let password_matches =
        bcrypt::verify(&req.password, &state.config.admin_pass_hash).unwrap_or(false);

    if !(username_matches && password_matches) {
        let count = state.rate_limiter.record_failure(&ip);
        let delay = std::cmp::min(count as u64 * 500, 3000);
        warn!(
            ip = %ip,
            forwarded_for,
            delay_ms = delay,
            "admin login failed: invalid credentials"
        );
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
    let csrf_token =
        crate::middleware::csrf::new_csrf_token(&session.id, &state.config.session_secret);
    info!(
        ip = %ip,
        forwarded_for,
        username = %state.config.admin_user,
        "admin login succeeded"
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
        state.config.session.expiry_duration.as_secs(),
        crate::auth::middleware::should_set_secure_cookie(
            &headers,
            state.config.security.force_https,
        ),
    );
    crate::auth::middleware::set_csrf_cookie(
        response.headers_mut(),
        &csrf_token,
        crate::auth::middleware::should_set_secure_cookie(
            &headers,
            state.config.security.force_https,
        ),
    );

    Ok(response)
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;
    use std::time::Duration;

    use axum::Json;
    use axum::body::to_bytes;
    use axum::extract::{ConnectInfo, State};
    use axum::http::{StatusCode, header};
    use axum::response::IntoResponse;

    use crate::config::SessionConfig;
    use crate::handlers::auth::LoginRequest;
    use crate::handlers::auth::test_support::{FakeValkey, test_state_with_session_config};

    use super::*;

    #[tokio::test]
    async fn test_login_sets_session_cookie_max_age_from_session_expiry() {
        let fake_valkey = FakeValkey::start().await;
        let custom_session_config = SessionConfig {
            expiry_duration: Duration::from_secs(45 * 60),
            ..SessionConfig::default()
        };
        let state = test_state_with_session_config(fake_valkey.url(), custom_session_config);

        let response = handle_login(
            State(Arc::clone(&state)),
            ConnectInfo(SocketAddr::from(([127, 0, 0, 1], 12345))),
            HeaderMap::new(),
            Json(LoginRequest {
                username: state.config.admin_user.clone(),
                password: "testpass".to_string(),
            }),
        )
        .await
        .expect("login response")
        .into_response();

        assert_eq!(response.status(), StatusCode::OK);
        let session_cookie = response
            .headers()
            .get_all(header::SET_COOKIE)
            .iter()
            .find_map(|value| {
                value.to_str().ok().filter(|cookie| {
                    cookie.starts_with("admin_session=") && cookie.contains("Max-Age=")
                })
            })
            .expect("session cookie");
        assert!(session_cookie.contains("Max-Age=2700"));
    }

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
}
