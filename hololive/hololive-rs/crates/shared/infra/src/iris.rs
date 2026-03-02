use std::time::Duration;

use reqwest::StatusCode;
use secrecy::{ExposeSecret, SecretString};
use serde::Serialize;
use shared_core::error::SharedError;

#[derive(Serialize)]
struct ReplyRequest<'a> {
    room_id: &'a str,
    message: &'a str,
}

pub struct IrisClient {
    client: reqwest::Client,
    base_url: String,
    bot_token: SecretString,
}

impl IrisClient {
    pub fn new(base_url: &str, bot_token: SecretString) -> Result<Self, SharedError> {
        let parsed = url::Url::parse(base_url)
            .map_err(|e| SharedError::Config(format!("invalid iris.base_url: {e}")))?;

        match parsed.scheme() {
            "http" | "https" => {}
            scheme => {
                return Err(SharedError::Config(format!(
                    "unsupported iris.base_url scheme: {scheme}"
                )));
            }
        }

        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|e| SharedError::Config(format!("build iris client: {e}")))?;

        Ok(Self {
            client,
            base_url: base_url.trim_end_matches('/').to_owned(),
            bot_token,
        })
    }

    pub async fn send_reply(&self, room_id: &str, message: &str) -> Result<(), SharedError> {
        let url = format!("{}/api/v1/reply", self.base_url);
        let response = self
            .client
            .post(url)
            .header("X-Bot-Token", self.bot_token.expose_secret())
            .json(&ReplyRequest { room_id, message })
            .send()
            .await
            .map_err(|e| SharedError::Http(e.without_url().to_string()))?;

        if response.status().is_success() {
            return Ok(());
        }

        let status = response.status();
        let body = response
            .text()
            .await
            .map_err(|e| SharedError::Http(e.without_url().to_string()))?;

        map_http_error(status, body)
    }
}

fn map_http_error(status: StatusCode, body: String) -> Result<(), SharedError> {
    let message = if body.trim().is_empty() {
        status.to_string()
    } else {
        body
    };

    match status {
        StatusCode::UNAUTHORIZED => Err(SharedError::Unauthorized(message)),
        StatusCode::FORBIDDEN => Err(SharedError::Forbidden(message)),
        StatusCode::NOT_FOUND => Err(SharedError::NotFound(message)),
        StatusCode::CONFLICT => Err(SharedError::Conflict(message)),
        _ => Err(SharedError::HttpStatus {
            code: status.as_u16(),
            message,
        }),
    }
}
