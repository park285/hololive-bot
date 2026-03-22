use axum::body::Body;
use axum::extract::{Request, State};
use axum::response::Response;
use hyper::Uri;
use hyper::header::UPGRADE;
use hyper_util::client::legacy::Client;
use hyper_util::client::legacy::connect::HttpConnector;
use hyper_util::rt::TokioExecutor;
use std::sync::Arc;

#[allow(missing_debug_implementations)]
pub struct BotProxy {
    // H2C client for normal API requests
    h2c_client: Client<HttpConnector, Body>,
    // HTTP/1.1 client for WebSocket upgrade
    http11_client: Client<HttpConnector, Body>,
    pub(crate) target_base: String,
    pub(crate) api_key: Option<String>,
}

impl BotProxy {
    pub fn new(target_url: &str, api_key: Option<String>) -> Self {
        let mut h2c_connector = HttpConnector::new();
        h2c_connector.enforce_http(false);

        let h2c_client = Client::builder(TokioExecutor::new())
            .http2_only(true)
            .build(h2c_connector);

        let mut http11_connector = HttpConnector::new();
        http11_connector.enforce_http(false);

        let http11_client = Client::builder(TokioExecutor::new()).build(http11_connector);

        Self {
            h2c_client,
            http11_client,
            target_base: target_url.trim_end_matches('/').to_string(),
            api_key,
        }
    }
}

fn should_forward_header(name: &hyper::header::HeaderName, is_websocket: bool) -> bool {
    let header = name.as_str();

    if matches!(
        header,
        "cookie" | "authorization" | "origin" | "host" | "x-csrf-token"
    ) {
        return false;
    }

    if matches!(
        header,
        "accept"
            | "accept-encoding"
            | "accept-language"
            | "cache-control"
            | "content-length"
            | "content-type"
            | "if-match"
            | "if-modified-since"
            | "if-none-match"
            | "if-unmodified-since"
            | "pragma"
            | "user-agent"
    ) {
        return true;
    }

    is_websocket
        && matches!(
            header,
            "connection"
                | "sec-websocket-extensions"
                | "sec-websocket-key"
                | "sec-websocket-protocol"
                | "sec-websocket-version"
                | "upgrade"
        )
}

/// Proxy handler for /admin/api/holo/*path
pub async fn proxy_holo(
    State(state): State<Arc<crate::state::AppState>>,
    req: Request,
) -> Result<Response, crate::error::AppError> {
    let proxy = state
        .bot_proxy
        .as_ref()
        .ok_or(crate::error::ProxyError::Unavailable)?;

    let (parts, body) = req.into_parts();

    // Path rewrite: /admin/api/holo/<path> -> /api/holo/<path>
    let original_path = parts.uri.path();
    let new_path = original_path
        .strip_prefix("/admin")
        .unwrap_or(original_path);
    let query = parts
        .uri
        .path_and_query()
        .and_then(|pq| pq.query())
        .map(|q| format!("?{q}"))
        .unwrap_or_default();
    let new_uri: Uri = format!("{}{}{}", proxy.target_base, new_path, query)
        .parse()
        .map_err(|e| anyhow::anyhow!("proxy uri parse failed: {e}"))?;

    // Check if WebSocket upgrade
    let is_ws = parts
        .headers
        .get(UPGRADE)
        .and_then(|v| v.to_str().ok())
        .is_some_and(|v| v.eq_ignore_ascii_case("websocket"));

    // Build new request
    let mut builder = hyper::Request::builder()
        .method(parts.method.clone())
        .uri(new_uri);

    // Forward only an explicit allowlist so admin cookies/tokens never leak upstream.
    for (name, value) in &parts.headers {
        if should_forward_header(name, is_ws) {
            builder = builder.header(name, value);
        }
    }

    // Inject API key
    if let Some(ref key) = proxy.api_key
        && !key.is_empty()
    {
        builder = builder.header("X-API-Key", key);
    }

    let proxy_req = builder
        .body(body)
        .map_err(|e| anyhow::anyhow!("proxy request build failed: {e}"))?;

    // Use appropriate client
    let result = if is_ws {
        proxy.http11_client.request(proxy_req).await
    } else {
        proxy.h2c_client.request(proxy_req).await
    };

    match result {
        Ok(resp) => {
            let (parts, body) = resp.into_parts();
            let body = Body::new(body);
            Ok(Response::from_parts(parts, body))
        }
        Err(e) => {
            tracing::error!(error = %e, "proxy_error");
            if is_ws {
                Err(crate::error::ProxyError::WsUnavailable.into())
            } else {
                Err(crate::error::ProxyError::Unavailable.into())
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use hyper::header::{ACCEPT, CONNECTION, CONTENT_TYPE, COOKIE, HeaderName, SEC_WEBSOCKET_KEY};

    #[test]
    fn test_path_rewrite() {
        let original = "/admin/api/holo/members";
        let rewritten = original.strip_prefix("/admin").unwrap();
        assert_eq!(rewritten, "/api/holo/members");
    }

    #[test]
    fn test_path_rewrite_nested() {
        let original = "/admin/api/holo/rooms/list";
        let rewritten = original.strip_prefix("/admin").unwrap();
        assert_eq!(rewritten, "/api/holo/rooms/list");
    }

    #[test]
    fn test_bot_proxy_creation() {
        let proxy = BotProxy::new("http://localhost:30001", None);
        assert_eq!(proxy.target_base, "http://localhost:30001");
    }

    #[test]
    fn test_bot_proxy_with_api_key() {
        let proxy = BotProxy::new("http://localhost:30001", Some("test-key".to_string()));
        assert_eq!(proxy.api_key, Some("test-key".to_string()));
    }

    #[test]
    fn test_target_base_trailing_slash_stripped() {
        let proxy = BotProxy::new("http://localhost:30001/", None);
        assert_eq!(proxy.target_base, "http://localhost:30001");
    }

    #[test]
    fn test_sensitive_headers_are_not_forwarded() {
        assert!(!should_forward_header(&COOKIE, false));
        assert!(!should_forward_header(
            &HeaderName::from_static("authorization"),
            false
        ));
        assert!(!should_forward_header(
            &HeaderName::from_static("x-csrf-token"),
            false
        ));
    }

    #[test]
    fn test_safe_http_headers_are_forwarded() {
        assert!(should_forward_header(&ACCEPT, false));
        assert!(should_forward_header(&CONTENT_TYPE, false));
    }

    #[test]
    fn test_websocket_upgrade_headers_are_forwarded_only_for_ws() {
        assert!(should_forward_header(&CONNECTION, true));
        assert!(should_forward_header(&SEC_WEBSOCKET_KEY, true));
        assert!(!should_forward_header(&SEC_WEBSOCKET_KEY, false));
    }
}
