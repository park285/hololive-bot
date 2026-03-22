use std::sync::Arc;

use axum::extract::{Request, State};
use axum::http::Method;
use axum::middleware::Next;
use axum::response::Response;
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use hmac::{Hmac, Mac};
use sha2::Sha256;

use crate::auth::middleware::{SessionId, extract_cookie};
use crate::auth::truncate_session_id;
use crate::config::SecurityMode;
use crate::error::{AppError, AuthError};
use crate::state::AppState;

type HmacSha256 = Hmac<Sha256>;

/// Generate a new CSRF token: nonce.hmac(nonce+session_id)
pub fn new_csrf_token(session_id: &str, secret: &str) -> String {
    let mut nonce_bytes = [0u8; 32];
    getrandom::fill(&mut nonce_bytes).expect("OS RNG unavailable");
    let nonce = hex::encode(nonce_bytes);

    let mut mac = HmacSha256::new_from_slice(secret.as_bytes()).expect("HMAC key");
    mac.update(nonce.as_bytes());
    mac.update(session_id.as_bytes());
    let sig = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());

    format!("{nonce}.{sig}")
}

/// Validate CSRF token against session
pub fn validate_csrf_token(session_id: &str, token: &str, secret: &str) -> bool {
    let Some((nonce, provided_sig)) = token.split_once('.') else {
        return false;
    };
    if nonce.is_empty() || provided_sig.is_empty() {
        return false;
    }

    let mut mac = HmacSha256::new_from_slice(secret.as_bytes()).expect("HMAC key");
    mac.update(nonce.as_bytes());
    mac.update(session_id.as_bytes());
    let expected_sig = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());

    constant_time_eq(provided_sig.as_bytes(), expected_sig.as_bytes())
}

pub async fn csrf_middleware(
    State(state): State<Arc<AppState>>,
    req: Request,
    next: Next,
) -> Result<Response, AppError> {
    if should_skip_csrf(req.method()) {
        return Ok(next.run(req).await);
    }

    let mode = state.config.security.csrf_mode;
    if mode == SecurityMode::Off {
        return Ok(next.run(req).await);
    }

    let session_id = req
        .extensions()
        .get::<SessionId>()
        .map(|s| s.0.clone())
        .unwrap_or_default();

    let header_token = req
        .headers()
        .get("X-CSRF-Token")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");

    let cookie_token = extract_cookie(&req, "csrf_token").unwrap_or_default();

    let valid = is_csrf_request_valid(
        &session_id,
        header_token,
        &cookie_token,
        &state.config.session_secret,
    );

    if valid {
        Ok(next.run(req).await)
    } else {
        match mode {
            SecurityMode::Monitor => {
                tracing::warn!(
                    session_id = %truncate_session_id(&session_id),
                    "csrf_violation_monitor"
                );
                Ok(next.run(req).await)
            }
            SecurityMode::Enforce => Err(AuthError::CsrfViolation.into()),
            SecurityMode::Off => unreachable!(),
        }
    }
}

const fn should_skip_csrf(method: &Method) -> bool {
    matches!(*method, Method::GET | Method::HEAD | Method::OPTIONS)
}

fn is_csrf_request_valid(
    session_id: &str,
    header_token: &str,
    cookie_token: &str,
    secret: &str,
) -> bool {
    !header_token.is_empty()
        && header_token == cookie_token
        && validate_csrf_token(session_id, header_token, secret)
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    a.iter()
        .zip(b.iter())
        .fold(0u8, |acc, (x, y)| acc | (x ^ y))
        == 0
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::Method;

    #[test]
    fn test_new_csrf_token_format() {
        let token = new_csrf_token("session123", "secret");
        assert!(!token.is_empty());
        let parts: Vec<&str> = token.split('.').collect();
        assert_eq!(parts.len(), 2);
        assert_eq!(parts[0].len(), 64);
    }

    #[test]
    fn test_validate_csrf_token_roundtrip() {
        let token = new_csrf_token("session123", "secret");
        assert!(validate_csrf_token("session123", &token, "secret"));
    }

    #[test]
    fn test_validate_csrf_token_wrong_session() {
        let token = new_csrf_token("session123", "secret");
        assert!(!validate_csrf_token("other_session", &token, "secret"));
    }

    #[test]
    fn test_validate_csrf_token_wrong_secret() {
        let token = new_csrf_token("session123", "secret1");
        assert!(!validate_csrf_token("session123", &token, "secret2"));
    }

    #[test]
    fn test_validate_csrf_token_tampered_nonce() {
        let token = new_csrf_token("session123", "secret");
        let tampered = format!("0000{}", &token[4..]);
        assert!(!validate_csrf_token("session123", &tampered, "secret"));
    }

    #[test]
    fn test_validate_csrf_token_invalid_format() {
        assert!(!validate_csrf_token("session123", "no-dot", "secret"));
        assert!(!validate_csrf_token("session123", "", "secret"));
    }

    #[test]
    fn test_should_skip_csrf_for_safe_methods() {
        assert!(should_skip_csrf(&Method::GET));
        assert!(should_skip_csrf(&Method::HEAD));
        assert!(should_skip_csrf(&Method::OPTIONS));
        assert!(!should_skip_csrf(&Method::POST));
    }

    #[test]
    fn test_is_csrf_request_valid_requires_matching_cookie() {
        let token = new_csrf_token("session123", "secret");
        assert!(!is_csrf_request_valid(
            "session123",
            &token,
            "other-token",
            "secret"
        ));
    }

    #[test]
    fn test_is_csrf_request_valid_rejects_empty_header() {
        let token = new_csrf_token("session123", "secret");
        assert!(!is_csrf_request_valid("session123", "", &token, "secret"));
    }

    #[test]
    fn test_is_csrf_request_valid_accepts_matching_valid_token() {
        let token = new_csrf_token("session123", "secret");
        assert!(is_csrf_request_valid(
            "session123",
            &token,
            &token,
            "secret"
        ));
    }
}
