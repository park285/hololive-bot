mod heartbeat;
#[cfg(test)]
mod heartbeat_test;
mod login;
mod session;
#[cfg(test)]
pub(super) mod test_support;

use std::net::SocketAddr;

use axum::http::HeaderMap;
use serde::{Deserialize, Serialize};
use utoipa::ToSchema;

#[doc(hidden)]
pub use heartbeat::__path_handle_heartbeat;
pub use heartbeat::{HeartbeatRequest, HeartbeatResponse, handle_heartbeat};
#[doc(hidden)]
pub use login::__path_handle_login;
pub use login::handle_login;
#[doc(hidden)]
pub use session::{__path_handle_logout, __path_handle_session_status};
pub use session::{handle_logout, handle_session_status};

#[derive(Debug, Deserialize, ToSchema)]
pub struct LoginRequest {
    pub username: String,
    pub password: String,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct LoginResponse {
    pub status: String,
    pub message: String,
    pub csrf_token: String,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct SessionStatusResponse {
    pub status: String,
    pub authenticated: bool,
    pub username: String,
    pub absolute_expires_at: i64,
    pub session_policy: SessionPolicyResponse,
}

#[allow(clippy::struct_field_names)]
#[derive(Debug, Serialize, ToSchema)]
pub struct SessionPolicyResponse {
    pub heartbeat_interval_ms: u64,
    pub idle_timeout_ms: u64,
    pub idle_warning_timeout_ms: u64,
    pub idle_session_ttl_ms: u64,
    pub absolute_warning_window_ms: u64,
}

fn constant_time_str_eq(left: &str, right: &str) -> bool {
    let left = left.as_bytes();
    let right = right.as_bytes();
    let max_len = left.len().max(right.len());

    let mut diff = left.len() ^ right.len();
    for idx in 0..max_len {
        let l = left.get(idx).copied().unwrap_or(0);
        let r = right.get(idx).copied().unwrap_or(0);
        diff |= usize::from(l ^ r);
    }

    diff == 0
}

fn truncate_session_id_for_log(session_id: &str) -> String {
    let prefix: String = session_id.chars().take(8).collect();
    if session_id.chars().count() > 8 {
        format!("{prefix}...")
    } else {
        prefix
    }
}

fn trust_forwarded_headers_from_env() -> bool {
    std::env::var("TRUST_FORWARDED_HEADERS")
        .ok()
        .is_some_and(|value| {
            matches!(
                value.trim().to_ascii_lowercase().as_str(),
                "1" | "true" | "yes" | "on"
            )
        })
}

fn first_forwarded_ip(headers: &HeaderMap) -> Option<String> {
    headers
        .get("x-forwarded-for")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.split(',').next())
        .map(str::trim)
        .filter(|value| value.parse::<std::net::IpAddr>().is_ok())
        .map(str::to_string)
        .or_else(|| {
            headers
                .get("x-real-ip")
                .and_then(|value| value.to_str().ok())
                .map(str::trim)
                .filter(|value| value.parse::<std::net::IpAddr>().is_ok())
                .map(str::to_string)
        })
}

fn client_ip_for_rate_limit(headers: &HeaderMap, peer_addr: SocketAddr) -> String {
    if trust_forwarded_headers_from_env()
        && let Some(forwarded_ip) = first_forwarded_ip(headers)
    {
        return forwarded_ip;
    }

    peer_addr.ip().to_string()
}

fn clear_auth_cookies(headers: &mut HeaderMap, secure_cookie: bool) {
    crate::auth::middleware::set_clear_cookie(headers, "admin_session", secure_cookie, true);
    crate::auth::middleware::set_clear_cookie(headers, "csrf_token", secure_cookie, false);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_constant_time_str_eq_matches_equality_contract() {
        assert!(constant_time_str_eq("admin", "admin"));
        assert!(!constant_time_str_eq("admin", "Admin"));
        assert!(!constant_time_str_eq("admin", "admin1"));
        assert!(!constant_time_str_eq("", "admin"));
    }

    #[test]
    fn test_first_forwarded_ip_uses_first_x_forwarded_for_entry() {
        let mut headers = HeaderMap::new();
        headers.insert("x-forwarded-for", "203.0.113.7, 10.0.0.1".parse().unwrap());

        assert_eq!(first_forwarded_ip(&headers).as_deref(), Some("203.0.113.7"));
    }
}
