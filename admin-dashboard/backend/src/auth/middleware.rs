use axum::extract::{Request, State};
use axum::http::StatusCode;
use axum::http::header::{HeaderMap, HeaderValue, SET_COOKIE};
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use std::sync::Arc;

use super::hmac::validate_session_signature;
use super::session::SessionProvider;
use crate::error::{AppError, AuthError};
use crate::state::AppState;

/// request extensions 에 저장되는 세션 ID newtype
#[derive(Debug, Clone)]
pub struct SessionId(pub String);

/// 인증 미들웨어: admin_session 쿠키 검증 후 SessionId 를 extensions 에 삽입
pub async fn auth_middleware(
    State(state): State<Arc<AppState>>,
    mut req: Request,
    next: Next,
) -> Result<Response, AppError> {
    let session_cookie = extract_cookie(&req, "admin_session").ok_or(AuthError::Unauthorized)?;

    let (session_id, valid) =
        validate_session_signature(&session_cookie, &state.config.session_secret);
    if !valid {
        return Err(AuthError::Unauthorized.into());
    }

    if !state.sessions.validate_session(&session_id).await {
        let mut response = StatusCode::UNAUTHORIZED.into_response();
        set_clear_cookie(
            response.headers_mut(),
            "admin_session",
            state.config.security.force_https,
        );
        return Ok(response);
    }

    req.extensions_mut().insert(SessionId(session_id));
    Ok(next.run(req).await)
}

// ---------------------------------------------------------------------------
// Cookie helpers
// ---------------------------------------------------------------------------

/// Cookie 헤더에서 name 에 해당하는 값을 추출
pub fn extract_cookie(req: &Request, name: &str) -> Option<String> {
    let header = req
        .headers()
        .get(axum::http::header::COOKIE)?
        .to_str()
        .ok()?;
    for pair in header.split(';') {
        let pair = pair.trim();
        if let Some((k, v)) = pair.split_once('=')
            && k.trim() == name
        {
            return Some(v.trim().to_string());
        }
    }
    None
}

/// 세션 쿠키 설정 (HttpOnly, SameSite=Strict, Max-Age=1800)
pub fn set_session_cookie(headers: &mut HeaderMap, name: &str, value: &str, force_https: bool) {
    let secure = if force_https { "; Secure" } else { "" };
    let cookie = format!("{name}={value}; HttpOnly; SameSite=Strict; Path=/; Max-Age=1800{secure}");
    if let Ok(val) = HeaderValue::from_str(&cookie) {
        headers.append(SET_COOKIE, val);
    }
}

/// CSRF 쿠키 설정 (JS 에서 읽어야 하므로 HttpOnly 없음)
pub fn set_csrf_cookie(headers: &mut HeaderMap, token: &str, force_https: bool) {
    let secure = if force_https { "; Secure" } else { "" };
    let cookie = format!("csrf_token={token}; SameSite=Strict; Path=/{secure}");
    if let Ok(val) = HeaderValue::from_str(&cookie) {
        headers.append(SET_COOKIE, val);
    }
}

/// 쿠키 삭제 (Max-Age=-1 으로 즉시 만료)
pub fn set_clear_cookie(headers: &mut HeaderMap, name: &str, force_https: bool) {
    let secure = if force_https { "; Secure" } else { "" };
    let cookie = format!("{name}=; HttpOnly; SameSite=Strict; Path=/; Max-Age=-1{secure}");
    if let Ok(val) = HeaderValue::from_str(&cookie) {
        headers.append(SET_COOKIE, val);
    }
}

// ---------------------------------------------------------------------------
// Security headers
// ---------------------------------------------------------------------------

const CSP: &str = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; \
                    img-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'";

/// 모든 응답에 보안 헤더를 추가하는 standalone async fn
/// 라우터에서 `.layer(axum::middleware::from_fn(apply_security_headers))` 로 사용
pub async fn apply_security_headers(mut response: Response) -> Response {
    let headers = response.headers_mut();
    headers.insert(
        axum::http::header::X_CONTENT_TYPE_OPTIONS,
        HeaderValue::from_static("nosniff"),
    );
    headers.insert(
        axum::http::header::X_FRAME_OPTIONS,
        HeaderValue::from_static("DENY"),
    );
    headers.insert(
        axum::http::header::X_XSS_PROTECTION,
        HeaderValue::from_static("1; mode=block"),
    );
    headers.insert(
        axum::http::header::REFERRER_POLICY,
        HeaderValue::from_static("strict-origin-when-cross-origin"),
    );
    headers.insert(
        axum::http::header::CONTENT_SECURITY_POLICY,
        HeaderValue::from_static(CSP),
    );
    response
}

/// force_https=true 일 때 HSTS 헤더도 추가하는 버전
#[allow(dead_code)]
pub async fn apply_security_headers_with_hsts(response: Response) -> Response {
    let mut response = apply_security_headers(response).await;
    response.headers_mut().insert(
        axum::http::header::STRICT_TRANSPORT_SECURITY,
        HeaderValue::from_static("max-age=31536000; includeSubDomains"),
    );
    response
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::{Request as HttpRequest, header};

    // -- extract_cookie --

    fn make_request_with_cookie(cookie_header: &str) -> Request {
        HttpRequest::builder()
            .header(header::COOKIE, cookie_header)
            .body(Body::empty())
            .unwrap()
    }

    #[test]
    fn test_extract_cookie_single() {
        let req = make_request_with_cookie("admin_session=abc123");
        assert_eq!(extract_cookie(&req, "admin_session"), Some("abc123".into()));
    }

    #[test]
    fn test_extract_cookie_multiple() {
        let req = make_request_with_cookie("foo=bar; admin_session=xyz; other=val");
        assert_eq!(extract_cookie(&req, "admin_session"), Some("xyz".into()));
    }

    #[test]
    fn test_extract_cookie_missing() {
        let req = make_request_with_cookie("foo=bar; other=val");
        assert_eq!(extract_cookie(&req, "admin_session"), None);
    }

    #[test]
    fn test_extract_cookie_no_header() {
        let req = HttpRequest::builder().body(Body::empty()).unwrap();
        assert_eq!(extract_cookie(&req, "admin_session"), None);
    }

    // -- set_session_cookie --

    #[test]
    fn test_set_session_cookie_https() {
        let mut headers = HeaderMap::new();
        set_session_cookie(&mut headers, "admin_session", "val123", true);
        let cookie = headers.get(SET_COOKIE).unwrap().to_str().unwrap();
        assert!(cookie.contains("admin_session=val123"));
        assert!(cookie.contains("HttpOnly"));
        assert!(cookie.contains("SameSite=Strict"));
        assert!(cookie.contains("Path=/"));
        assert!(cookie.contains("Max-Age=1800"));
        assert!(cookie.contains("Secure"));
    }

    #[test]
    fn test_set_session_cookie_no_https() {
        let mut headers = HeaderMap::new();
        set_session_cookie(&mut headers, "admin_session", "val123", false);
        let cookie = headers.get(SET_COOKIE).unwrap().to_str().unwrap();
        assert!(!cookie.contains("Secure"));
        assert!(cookie.contains("HttpOnly"));
    }

    // -- set_csrf_cookie --

    #[test]
    fn test_set_csrf_cookie_not_httponly() {
        let mut headers = HeaderMap::new();
        set_csrf_cookie(&mut headers, "csrf-token-value", true);
        let cookie = headers.get(SET_COOKIE).unwrap().to_str().unwrap();
        assert!(cookie.contains("csrf_token=csrf-token-value"));
        assert!(!cookie.contains("HttpOnly"));
        assert!(cookie.contains("SameSite=Strict"));
        assert!(cookie.contains("Path=/"));
        assert!(cookie.contains("Secure"));
    }

    #[test]
    fn test_set_csrf_cookie_no_https() {
        let mut headers = HeaderMap::new();
        set_csrf_cookie(&mut headers, "tok", false);
        let cookie = headers.get(SET_COOKIE).unwrap().to_str().unwrap();
        assert!(!cookie.contains("Secure"));
        assert!(!cookie.contains("HttpOnly"));
    }

    // -- set_clear_cookie --

    #[test]
    fn test_set_clear_cookie_attributes() {
        let mut headers = HeaderMap::new();
        set_clear_cookie(&mut headers, "admin_session", true);
        let cookie = headers.get(SET_COOKIE).unwrap().to_str().unwrap();
        assert!(cookie.contains("admin_session="));
        assert!(cookie.contains("Max-Age=-1"));
        assert!(cookie.contains("HttpOnly"));
        assert!(cookie.contains("SameSite=Strict"));
        assert!(cookie.contains("Path=/"));
        assert!(cookie.contains("Secure"));
    }

    #[test]
    fn test_set_clear_cookie_no_https() {
        let mut headers = HeaderMap::new();
        set_clear_cookie(&mut headers, "admin_session", false);
        let cookie = headers.get(SET_COOKIE).unwrap().to_str().unwrap();
        assert!(cookie.contains("Max-Age=-1"));
        assert!(!cookie.contains("Secure"));
    }

    // -- security headers --

    #[tokio::test]
    async fn test_security_headers_present() {
        let response = Response::builder()
            .status(StatusCode::OK)
            .body(Body::empty())
            .unwrap();
        let response = apply_security_headers(response).await;
        let h = response.headers();

        assert_eq!(h.get(header::X_CONTENT_TYPE_OPTIONS).unwrap(), "nosniff");
        assert_eq!(h.get(header::X_FRAME_OPTIONS).unwrap(), "DENY");
        assert_eq!(h.get(header::X_XSS_PROTECTION).unwrap(), "1; mode=block");
        assert_eq!(
            h.get(header::REFERRER_POLICY).unwrap(),
            "strict-origin-when-cross-origin"
        );
        assert!(
            h.get(header::CONTENT_SECURITY_POLICY)
                .unwrap()
                .to_str()
                .unwrap()
                .contains("default-src 'self'")
        );
        // HSTS 는 기본 함수에서 미포함
        assert!(h.get(header::STRICT_TRANSPORT_SECURITY).is_none());
    }

    #[tokio::test]
    async fn test_security_headers_with_hsts() {
        let response = Response::builder()
            .status(StatusCode::OK)
            .body(Body::empty())
            .unwrap();
        let response = apply_security_headers_with_hsts(response).await;
        let h = response.headers();

        // 기본 헤더도 포함
        assert_eq!(h.get(header::X_CONTENT_TYPE_OPTIONS).unwrap(), "nosniff");
        // HSTS 포함
        assert_eq!(
            h.get(header::STRICT_TRANSPORT_SECURITY).unwrap(),
            "max-age=31536000; includeSubDomains"
        );
    }

    #[tokio::test]
    async fn test_no_csp_report_only_header() {
        let response = Response::builder()
            .status(StatusCode::OK)
            .body(Body::empty())
            .unwrap();
        let response = apply_security_headers(response).await;
        assert!(
            response
                .headers()
                .get("content-security-policy-report-only")
                .is_none()
        );
    }
}
