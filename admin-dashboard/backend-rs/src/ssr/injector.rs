use reqwest::Client;

#[allow(dead_code, missing_debug_implementations)]
pub struct SsrInjector {
    holo_bot_url: String,
    html_cache: Vec<u8>,
    http_client: Client,
}

#[allow(dead_code)]
impl SsrInjector {
    pub fn new(holo_bot_url: &str) -> Self {
        let html_cache = crate::static_files::index_html().unwrap_or_default();
        let http_client = Client::builder()
            .timeout(std::time::Duration::from_secs(3))
            .build()
            .unwrap_or_else(|e| panic!("http client build failed: {e}"));
        Self {
            holo_bot_url: holo_bot_url.to_string(),
            html_cache,
            http_client,
        }
    }

    /// Serve HTML with optional SSR data injection
    /// If authenticated and path matches a data route, fetch upstream data and inject
    /// Otherwise return cached HTML
    pub async fn serve_with_injection(&self, path: &str, session_id: Option<&str>) -> Vec<u8> {
        if session_id.is_none() || self.html_cache.is_empty() {
            return self.html_cache.clone();
        }

        let Some(data_url) = self.data_url_for_path(path) else {
            return self.html_cache.clone();
        };

        match self.fetch_upstream(&data_url).await {
            Ok(data) => self.inject_data(&data),
            Err(e) => {
                tracing::warn!(error = %e, path = %path, "ssr_fetch_failed");
                self.html_cache.clone()
            }
        }
    }

    fn data_url_for_path(&self, path: &str) -> Option<String> {
        let api_path = match path {
            p if p.starts_with("/dashboard/members") => "/api/holo/members",
            p if p.starts_with("/dashboard/rooms") => "/api/holo/rooms",
            p if p.starts_with("/dashboard") => "/api/holo/status",
            _ => return None,
        };
        Some(format!("{}{}", self.holo_bot_url, api_path))
    }

    async fn fetch_upstream(&self, url: &str) -> Result<serde_json::Value, anyhow::Error> {
        let resp = self.http_client.get(url).send().await?;

        if let Some(len) = resp.content_length()
            && len > 2 * 1024 * 1024
        {
            anyhow::bail!("upstream response too large: {len} bytes");
        }

        let bytes = resp.bytes().await?;
        if bytes.len() > 2 * 1024 * 1024 {
            anyhow::bail!("upstream response too large: {} bytes", bytes.len());
        }

        Ok(serde_json::from_slice(&bytes)?)
    }

    fn inject_data(&self, data: &serde_json::Value) -> Vec<u8> {
        let html = String::from_utf8_lossy(&self.html_cache);
        let script = format!(
            r"<script>window.__SSR_DATA__={}</script>",
            serde_json::to_string(data).unwrap_or_default()
        );

        match html.find("</head>") {
            Some(pos) => {
                let mut result = Vec::with_capacity(self.html_cache.len() + script.len());
                result.extend_from_slice(&self.html_cache[..pos]);
                result.extend_from_slice(script.as_bytes());
                result.extend_from_slice(&self.html_cache[pos..]);
                result
            }
            None => self.html_cache.clone(),
        }
    }

    /// Reload HTML cache (e.g., if static files change)
    pub fn reload_cache(&mut self) {
        self.html_cache = crate::static_files::index_html().unwrap_or_default();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_injector() -> SsrInjector {
        SsrInjector {
            holo_bot_url: "http://localhost:30001".to_string(),
            html_cache: b"<html><head><title>Test</title></head><body></body></html>".to_vec(),
            http_client: Client::builder()
                .timeout(std::time::Duration::from_secs(1))
                .build()
                .unwrap(),
        }
    }

    #[test]
    fn test_inject_data_before_head() {
        let injector = test_injector();
        let data = serde_json::json!({"key": "value"});
        let result = injector.inject_data(&data);
        let html = String::from_utf8(result).unwrap();
        assert!(html.contains("window.__SSR_DATA__="));
        assert!(html.contains("</head>"));
        let ssr_pos = html.find("__SSR_DATA__").unwrap();
        let head_pos = html.find("</head>").unwrap();
        assert!(ssr_pos < head_pos);
    }

    #[test]
    fn test_inject_data_serialization_behavior() {
        let injector = test_injector();
        let data = serde_json::json!({"evil": "</script><script>alert(1)</script>"});
        let expected_json = serde_json::to_string(&data).unwrap();
        let result = injector.inject_data(&data);
        let html = String::from_utf8(result).unwrap();
        assert!(html.contains(&format!("window.__SSR_DATA__={expected_json}")));
    }

    #[test]
    fn test_inject_no_head_tag_returns_original() {
        let mut injector = test_injector();
        injector.html_cache = b"<html><body>no head</body></html>".to_vec();
        let data = serde_json::json!({"key": "value"});
        let result = injector.inject_data(&data);
        assert_eq!(result, injector.html_cache);
    }

    #[test]
    fn test_data_url_for_path() {
        let injector = test_injector();
        assert!(injector.data_url_for_path("/dashboard/members").is_some());
        assert!(injector.data_url_for_path("/dashboard/rooms").is_some());
        assert!(injector.data_url_for_path("/dashboard").is_some());
        assert!(injector.data_url_for_path("/login").is_none());
        assert!(injector.data_url_for_path("/other").is_none());
    }

    #[tokio::test]
    async fn test_serve_no_session_returns_cached() {
        let injector = test_injector();
        let result = injector.serve_with_injection("/dashboard", None).await;
        assert_eq!(result, injector.html_cache);
    }

    #[tokio::test]
    async fn test_serve_empty_cache_returns_empty() {
        let mut injector = test_injector();
        injector.html_cache = vec![];
        let result = injector
            .serve_with_injection("/dashboard", Some("session"))
            .await;
        assert!(result.is_empty());
    }
}
