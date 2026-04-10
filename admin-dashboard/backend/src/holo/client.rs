use reqwest::Method;
use serde::Serialize;
use serde::de::DeserializeOwned;

use crate::error::{AppError, ProxyError};

#[derive(Debug, Clone)]
pub struct HoloApiClient {
    base_url: String,
    api_key: Option<String>,
    client: reqwest::Client,
}

impl HoloApiClient {
    pub fn new(base_url: &str, api_key: Option<String>) -> anyhow::Result<Self> {
        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(10))
            .build()?;

        Ok(Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            api_key: api_key.filter(|value| !value.trim().is_empty()),
            client,
        })
    }

    pub async fn get<T: DeserializeOwned>(
        &self,
        path: &str,
        query: Option<&[(&str, String)]>,
    ) -> Result<(reqwest::StatusCode, T), AppError> {
        self.request(Method::GET, path, query, Option::<&()>::None)
            .await
    }

    pub async fn send<B: Serialize + ?Sized, T: DeserializeOwned>(
        &self,
        method: Method,
        path: &str,
        body: Option<&B>,
    ) -> Result<(reqwest::StatusCode, T), AppError> {
        self.request(method, path, None, body).await
    }

    async fn request<B: Serialize + ?Sized, T: DeserializeOwned>(
        &self,
        method: Method,
        path: &str,
        query: Option<&[(&str, String)]>,
        body: Option<&B>,
    ) -> Result<(reqwest::StatusCode, T), AppError> {
        let mut url = reqwest::Url::parse(&format!("{}{}", self.base_url, path))
            .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;
        if let Some(query) = query {
            url.query_pairs_mut()
                .extend_pairs(query.iter().map(|(key, value)| (*key, value.as_str())));
        }

        let mut request = self.client.request(method, url);
        if let Some(api_key) = &self.api_key {
            request = request.header("X-API-Key", api_key);
        }
        if let Some(body) = body {
            request = request.json(body);
        }

        let response = request
            .send()
            .await
            .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;
        let status = response.status();
        if status.is_server_error() {
            return Err(AppError::Proxy(ProxyError::Unavailable));
        }

        let bytes = response
            .bytes()
            .await
            .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;
        let parsed = serde_json::from_slice::<T>(&bytes)
            .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;

        Ok((status, parsed))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::extract::{Query, State};
    use axum::http::HeaderMap;
    use axum::routing::{get, post};
    use axum::{Json, Router};
    use serde_json::{Value, json};
    use std::collections::HashMap;
    use std::sync::Arc;
    use tokio::net::TcpListener;
    use tokio::sync::Mutex;

    #[derive(Debug, Default)]
    struct Capture {
        api_key: Option<String>,
        query: HashMap<String, String>,
        body: Option<Value>,
    }

    async fn spawn_server(capture: Arc<Mutex<Capture>>) -> String {
        let app = Router::new()
            .route(
                "/api/holo/members",
                get(
                    |State(capture): State<Arc<Mutex<Capture>>>,
                     headers: HeaderMap,
                     Query(query): Query<HashMap<String, String>>| async move {
                        let mut guard = capture.lock().await;
                        guard.api_key = headers
                            .get("x-api-key")
                            .and_then(|value| value.to_str().ok())
                            .map(str::to_string);
                        guard.query = query;
                        Json(json!({ "status": "ok", "members": [] }))
                    },
                ),
            )
            .route(
                "/api/holo/rooms",
                post(
                    |State(capture): State<Arc<Mutex<Capture>>>,
                     headers: HeaderMap,
                     Json(body): Json<Value>| async move {
                        let mut guard = capture.lock().await;
                        guard.api_key = headers
                            .get("x-api-key")
                            .and_then(|value| value.to_str().ok())
                            .map(str::to_string);
                        guard.body = Some(body);
                        Json(json!({ "status": "ok" }))
                    },
                ),
            )
            .with_state(capture);

        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        format!("http://{addr}")
    }

    #[tokio::test]
    async fn test_get_injects_api_key_and_query() {
        let capture = Arc::new(Mutex::new(Capture::default()));
        let base_url = spawn_server(Arc::clone(&capture)).await;
        let client = HoloApiClient::new(&base_url, Some("secret".to_string())).unwrap();

        let (_status, _response): (reqwest::StatusCode, serde_json::Value) = client
            .get(
                "/api/holo/members",
                Some(&[("org", "hololive".to_string())]),
            )
            .await
            .unwrap();

        let guard = capture.lock().await;
        assert_eq!(guard.api_key.as_deref(), Some("secret"));
        assert_eq!(guard.query.get("org").map(String::as_str), Some("hololive"));
    }

    #[tokio::test]
    async fn test_send_serializes_json_body() {
        let capture = Arc::new(Mutex::new(Capture::default()));
        let base_url = spawn_server(Arc::clone(&capture)).await;
        let client = HoloApiClient::new(&base_url, Some("secret".to_string())).unwrap();

        let body = json!({ "room": "room-1" });
        let (_status, _response): (reqwest::StatusCode, serde_json::Value) = client
            .send(Method::POST, "/api/holo/rooms", Some(&body))
            .await
            .unwrap();

        let guard = capture.lock().await;
        assert_eq!(guard.api_key.as_deref(), Some("secret"));
        assert_eq!(guard.body.as_ref(), Some(&body));
    }
}
