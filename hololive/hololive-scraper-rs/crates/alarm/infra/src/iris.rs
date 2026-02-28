use std::time::Duration;

use alarm_core::error::AlarmError;
use async_trait::async_trait;
use reqwest::Client;
use secrecy::{ExposeSecret, SecretString};
use serde::Serialize;
use tracing::debug;

use crate::{
    circuit_breaker::CircuitBreaker,
    config::{IrisConfig, validate_iris_base_url_policy},
};

/// Iris 메시지 발송 인터페이스 (카카오톡 봇 중계)
#[async_trait]
pub trait IrisClient: Send + Sync {
    /// 지정 채팅방에 텍스트 메시지 발송
    async fn send_reply(&self, room_id: &str, message: &str) -> Result<(), AlarmError>;
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP Iris 클라이언트 구현
// ─────────────────────────────────────────────────────────────────────────────

/// POST /api/v1/reply 요청 본문
#[derive(Serialize)]
struct ReplyRequest<'a> {
    room_id: &'a str,
    message: &'a str,
}

/// reqwest 기반 Iris HTTP 클라이언트
pub struct HttpIrisClient {
    http: Client,
    base_url: String,
    bot_token: SecretString,
    circuit: CircuitBreaker,
}

impl HttpIrisClient {
    pub fn new(config: &IrisConfig) -> Result<Self, AlarmError> {
        validate_iris_base_url_policy(&config.base_url)
            .map_err(|e| AlarmError::Config(format!("Iris base_url 보안 정책 위반: {e}")))?;

        let http = Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| AlarmError::Config(format!("Iris HTTP 클라이언트 생성 실패: {e}")))?;
        let circuit = CircuitBreaker::new(
            config.circuit_failure_threshold,
            Duration::from_secs(config.circuit_reset_secs),
            "iris",
        );

        Ok(Self {
            http,
            base_url: config.base_url.trim_end_matches('/').to_string(),
            bot_token: config.bot_token.clone(),
            circuit,
        })
    }

    /// 서킷 브레이커 현재 상태 (테스트용)
    #[cfg(test)]
    pub fn circuit_state(&self) -> crate::circuit_breaker::CircuitState {
        self.circuit.state()
    }
}

fn redact_reqwest_error(err: reqwest::Error) -> String {
    // reqwest error가 포함할 수 있는 URL(userinfo/query)을 제거
    err.without_url().to_string()
}

#[async_trait]
impl IrisClient for HttpIrisClient {
    async fn send_reply(&self, room_id: &str, message: &str) -> Result<(), AlarmError> {
        self.circuit.allow_request()?;
        let url = format!("{}/api/v1/reply", self.base_url);

        let resp = self
            .http
            .post(&url)
            .header("X-Bot-Token", self.bot_token.expose_secret())
            .json(&ReplyRequest { room_id, message })
            .send()
            .await
            .map_err(|e| {
                self.circuit.record_failure();
                AlarmError::Http(format!(
                    "Iris 메시지 발송 실패: {}",
                    redact_reqwest_error(e)
                ))
            })?;

        if !resp.status().is_success() {
            self.circuit.record_failure();
            return Err(AlarmError::Api {
                platform: "iris".into(),
                message: format!("room={room_id} HTTP {}", resp.status()),
            });
        }

        self.circuit.record_success();
        debug!("Iris 메시지 발송 성공: room={room_id}");
        Ok(())
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock 구현 (테스트용)
// ─────────────────────────────────────────────────────────────────────────────

/// 테스트용 Iris 클라이언트 Mock — 발송 내역을 메모리에 기록
pub struct MockIrisClient {
    pub sent: std::sync::Arc<tokio::sync::Mutex<Vec<(String, String)>>>,
    error: Option<String>,
}

impl MockIrisClient {
    pub fn new() -> Self {
        Self {
            sent: std::sync::Arc::new(tokio::sync::Mutex::new(Vec::new())),
            error: None,
        }
    }

    pub fn with_error(message: impl Into<String>) -> Self {
        Self {
            sent: std::sync::Arc::new(tokio::sync::Mutex::new(Vec::new())),
            error: Some(message.into()),
        }
    }

    /// 발송된 메시지 목록 반환
    pub async fn sent_messages(&self) -> Vec<(String, String)> {
        self.sent.lock().await.clone()
    }
}

impl Default for MockIrisClient {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl IrisClient for MockIrisClient {
    async fn send_reply(&self, room_id: &str, message: &str) -> Result<(), AlarmError> {
        if let Some(msg) = &self.error {
            return Err(AlarmError::Http(msg.clone()));
        }
        self.sent
            .lock()
            .await
            .push((room_id.to_string(), message.to_string()));
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use secrecy::SecretString;

    use super::*;
    use crate::circuit_breaker::CircuitState;

    #[tokio::test]
    async fn mock_records_sent_messages() {
        let client = MockIrisClient::new();

        client.send_reply("room1", "안녕하세요").await.unwrap();
        client.send_reply("room2", "방송 시작!").await.unwrap();

        let sent = client.sent_messages().await;
        assert_eq!(sent.len(), 2);
        assert_eq!(sent[0], ("room1".into(), "안녕하세요".into()));
        assert_eq!(sent[1], ("room2".into(), "방송 시작!".into()));
    }

    #[tokio::test]
    async fn mock_returns_error() {
        let client = MockIrisClient::with_error("연결 실패");
        let result = client.send_reply("room1", "메시지").await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn mock_empty_message_recorded() {
        let client = MockIrisClient::new();
        client.send_reply("room1", "").await.unwrap();

        let sent = client.sent_messages().await;
        assert_eq!(sent[0].1, "");
    }

    fn make_http_config() -> IrisConfig {
        IrisConfig {
            base_url: "http://127.0.0.1:9".into(),
            bot_token: SecretString::from("dummy-token".to_string()),
            timeout_secs: 1,
            circuit_failure_threshold: 1,
            circuit_reset_secs: 60,
        }
    }

    #[tokio::test]
    async fn http_client_opens_circuit_after_request_failure() {
        let client = HttpIrisClient::new(&make_http_config()).expect("client should be created");

        let first = client.send_reply("room1", "msg").await;
        assert!(
            first.is_err(),
            "first request should fail and count toward circuit"
        );
        assert_eq!(client.circuit_state(), CircuitState::Open);

        let second = client.send_reply("room1", "msg").await;
        assert!(
            matches!(second, Err(AlarmError::CircuitOpen { .. })),
            "second request should be short-circuited when circuit is open"
        );
    }

    #[test]
    fn http_client_rejects_public_http_base_url() {
        let mut cfg = make_http_config();
        cfg.base_url = "http://api.example.com".into();
        let err = match HttpIrisClient::new(&cfg) {
            Ok(_) => panic!("public HTTP should be blocked"),
            Err(err) => err,
        };
        assert!(
            matches!(err, AlarmError::Config(msg) if msg.contains("보안 정책 위반")),
            "invalid iris.base_url must be rejected"
        );
    }

    #[test]
    fn http_client_allows_internal_http_base_url() {
        let mut cfg = make_http_config();
        cfg.base_url = "http://iris:8080".into();
        assert!(
            HttpIrisClient::new(&cfg).is_ok(),
            "internal HTTP endpoint should be allowed"
        );
    }
}
