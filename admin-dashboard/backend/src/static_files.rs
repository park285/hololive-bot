use axum::http::{HeaderValue, StatusCode, header};
use axum::response::{IntoResponse, Response};
use rust_embed::RustEmbed;

#[derive(RustEmbed)]
#[folder = "static/dist/"]
struct StaticAssets;

/// Check if embedded assets exist (index.html present)
#[allow(dead_code)]
pub fn has_embedded() -> bool {
    StaticAssets::get("index.html").is_some()
}

/// Get index.html content
pub fn index_html() -> Option<Vec<u8>> {
    StaticAssets::get("index.html").map(|f| f.data.to_vec())
}

/// Get favicon
pub fn favicon() -> Option<Vec<u8>> {
    StaticAssets::get("favicon.svg").map(|f| f.data.to_vec())
}

/// Serve static files from embedded assets
pub async fn serve_static(uri: axum::http::Uri) -> impl IntoResponse {
    let path = uri.path().trim_start_matches("/assets/");
    let asset_path = format!("assets/{path}");

    match StaticAssets::get(&asset_path) {
        Some(file) => {
            let mime = mime_guess::from_path(&asset_path).first_or_octet_stream();
            let mut response = Response::new(axum::body::Body::from(file.data.to_vec()));
            response.headers_mut().insert(
                header::CONTENT_TYPE,
                HeaderValue::from_str(mime.as_ref())
                    .unwrap_or_else(|_| HeaderValue::from_static("application/octet-stream")),
            );
            response.headers_mut().insert(
                header::CACHE_CONTROL,
                HeaderValue::from_static("public, max-age=31536000, immutable"),
            );
            response
        }
        None => StatusCode::NOT_FOUND.into_response(),
    }
}

/// Serve favicon with cache
pub async fn serve_favicon() -> impl IntoResponse {
    match favicon() {
        Some(data) => {
            let mut response = Response::new(axum::body::Body::from(data));
            response.headers_mut().insert(
                header::CONTENT_TYPE,
                HeaderValue::from_static("image/svg+xml"),
            );
            response.headers_mut().insert(
                header::CACHE_CONTROL,
                HeaderValue::from_static("public, max-age=86400"),
            );
            response
        }
        None => StatusCode::NOT_FOUND.into_response(),
    }
}

/// Serve index.html for SPA fallback (no-cache)
pub async fn serve_index() -> impl IntoResponse {
    match index_html() {
        Some(data) => {
            let mut response = Response::new(axum::body::Body::from(data));
            response.headers_mut().insert(
                header::CONTENT_TYPE,
                HeaderValue::from_static("text/html; charset=utf-8"),
            );
            response
                .headers_mut()
                .insert(header::CACHE_CONTROL, HeaderValue::from_static("no-cache"));
            response
        }
        None => StatusCode::NOT_FOUND.into_response(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_has_embedded_in_dev() {
        assert!(!has_embedded());
    }

    #[test]
    fn test_index_html_not_present_in_dev() {
        assert!(index_html().is_none());
    }

    #[tokio::test]
    async fn test_serve_static_not_found() {
        let uri: axum::http::Uri = "/assets/nonexistent.js".parse().unwrap();
        let response = serve_static(uri).await.into_response();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }

    #[tokio::test]
    async fn test_serve_favicon_not_found_in_dev() {
        let response = serve_favicon().await.into_response();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }

    #[tokio::test]
    async fn test_serve_index_not_found_in_dev() {
        let response = serve_index().await.into_response();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }
}
