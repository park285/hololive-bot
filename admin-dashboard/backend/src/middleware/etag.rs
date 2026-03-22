use axum::body::Body;
use axum::extract::Request;
use axum::http::{HeaderValue, Method, StatusCode, header};
use axum::middleware::Next;
use axum::response::Response;
use http_body_util::BodyExt;
use xxhash_rust::xxh3::xxh3_64;

/// ETag middleware for API GET responses.
///
/// Buffers response body, computes xxhash, supports If-None-Match -> 304.
/// Only applies to GET requests on /admin/api/* paths.
/// Excludes WebSocket upgrades and non-API paths.
pub async fn etag_middleware(req: Request, next: Next) -> Response {
    if req.method() != Method::GET {
        return next.run(req).await;
    }

    let path = req.uri().path().to_string();
    if !path.starts_with("/admin/api/") {
        return next.run(req).await;
    }

    if req.headers().contains_key(header::UPGRADE) {
        return next.run(req).await;
    }

    let if_none_match = req
        .headers()
        .get(header::IF_NONE_MATCH)
        .and_then(|value| value.to_str().ok())
        .map(str::to_owned);

    let response = next.run(req).await;

    if response.status() != StatusCode::OK {
        return response;
    }

    let (parts, body) = response.into_parts();
    let bytes = match body.collect().await {
        Ok(collected) => collected.to_bytes(),
        Err(_) => return Response::from_parts(parts, Body::empty()),
    };

    let hash = xxh3_64(&bytes);
    let etag = format!("\"{hash:016x}\"");

    if let Some(inm) = if_none_match
        && (inm == etag || inm == etag.trim_matches('"'))
    {
        let mut not_modified = Response::new(Body::empty());
        *not_modified.status_mut() = StatusCode::NOT_MODIFIED;
        not_modified.headers_mut().insert(
            header::ETAG,
            HeaderValue::from_str(&etag).unwrap_or_else(|_| HeaderValue::from_static("\"0\"")),
        );
        return not_modified;
    }

    let mut response = Response::from_parts(parts, Body::from(bytes));
    response.headers_mut().insert(
        header::ETAG,
        HeaderValue::from_str(&etag).unwrap_or_else(|_| HeaderValue::from_static("\"0\"")),
    );
    response
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::{Request, StatusCode, header};
    use axum::routing::{get, post};
    use axum::{Router, middleware};
    use tower::ServiceExt;

    fn test_app() -> Router {
        Router::new()
            .route("/admin/api/status", get(|| async { "hello world" }))
            .route("/other", get(|| async { "other" }))
            .layer(middleware::from_fn(etag_middleware))
    }

    #[tokio::test]
    async fn test_get_api_has_etag() {
        let app = test_app();
        let req = Request::get("/admin/api/status")
            .body(Body::empty())
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();

        assert_eq!(resp.status(), StatusCode::OK);
        assert!(resp.headers().contains_key(header::ETAG));
    }

    #[tokio::test]
    async fn test_if_none_match_returns_304() {
        let app = test_app();
        let req = Request::get("/admin/api/status")
            .body(Body::empty())
            .unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        let etag = resp
            .headers()
            .get(header::ETAG)
            .unwrap()
            .to_str()
            .unwrap()
            .to_string();

        let req = Request::get("/admin/api/status")
            .header(header::IF_NONE_MATCH, &etag)
            .body(Body::empty())
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();

        assert_eq!(resp.status(), StatusCode::NOT_MODIFIED);
    }

    #[tokio::test]
    async fn test_post_no_etag() {
        let app = Router::new()
            .route("/admin/api/status", post(|| async { "ok" }))
            .layer(middleware::from_fn(etag_middleware));
        let req = Request::post("/admin/api/status")
            .body(Body::empty())
            .unwrap();
        let resp = app.oneshot(req).await.unwrap();

        assert!(!resp.headers().contains_key(header::ETAG));
    }

    #[tokio::test]
    async fn test_non_api_no_etag() {
        let app = test_app();
        let req = Request::get("/other").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();

        assert!(!resp.headers().contains_key(header::ETAG));
    }

    #[tokio::test]
    async fn test_different_body_different_etag() {
        let app = Router::new()
            .route("/admin/api/a", get(|| async { "aaa" }))
            .route("/admin/api/b", get(|| async { "bbb" }))
            .layer(middleware::from_fn(etag_middleware));

        let resp_a = app
            .clone()
            .oneshot(Request::get("/admin/api/a").body(Body::empty()).unwrap())
            .await
            .unwrap();
        let resp_b = app
            .oneshot(Request::get("/admin/api/b").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let etag_a = resp_a.headers().get(header::ETAG).unwrap();
        let etag_b = resp_b.headers().get(header::ETAG).unwrap();

        assert_ne!(etag_a, etag_b);
    }
}
