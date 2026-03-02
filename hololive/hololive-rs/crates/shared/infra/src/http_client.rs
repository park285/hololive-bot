use std::time::Duration;

use shared_core::error::SharedError;
use tracing::debug;

#[derive(Debug, Clone)]
pub struct HttpClientConfig {
    pub timeout_secs: u64,
    pub user_agent: String,
    pub max_retries: u32,
}

impl Default for HttpClientConfig {
    fn default() -> Self {
        Self {
            timeout_secs: 30,
            user_agent: "hololive-shared-infra/0.1".to_owned(),
            max_retries: 3,
        }
    }
}

pub fn create_http_client(config: &HttpClientConfig) -> Result<reqwest::Client, SharedError> {
    if config.max_retries > 0 {
        debug!(
            max_retries = config.max_retries,
            "http retry count configured; retries are caller-managed"
        );
    }

    reqwest::Client::builder()
        .timeout(Duration::from_secs(config.timeout_secs))
        .user_agent(config.user_agent.clone())
        .build()
        .map_err(|e| SharedError::Config(format!("build http client: {e}")))
}
