use std::time::Duration;

use alarm_core::{
    error::AlarmError,
    model::{Stream, StreamStatus},
};
use async_trait::async_trait;
use reqwest::Client;
use reqwest_middleware::{ClientBuilder, ClientWithMiddleware};
use reqwest_retry::{RetryTransientMiddleware, policies::ExponentialBackoff};
use serde::Deserialize;
use tracing::debug;

use crate::{circuit_breaker::CircuitBreaker, config::ChzzkConfig};

/// Chzzk 라이브 상태 조회 인터페이스
#[async_trait]
pub trait ChzzkClient: Send + Sync {
    /// Chzzk 채널의 현재 라이브 스태이터스 조회 (없으면 None)
    async fn get_live_status(&self, channel_id: &str) -> Result<Option<Stream>, AlarmError>;
}

// ─────────────────────────────────────────────────────────────────────────────
// Chzzk API 응답 역직렬화 구조체 (내부 전용)
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
struct ChzzkApiResponse {
    content: Option<ChzzkLiveStatus>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ChzzkLiveStatus {
    live_id: Option<i64>,
    live_title: Option<String>,
    status: Option<String>,
    channel: Option<ChzzkChannelInfo>,
    concurrent_user_count: Option<i32>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ChzzkChannelInfo {
    #[allow(dead_code)]
    channel_id: Option<String>,
    channel_name: Option<String>,
}

/// Chzzk API 응답을 alarm_core::model::Stream으로 변환
fn to_stream(channel_id: &str, live: &ChzzkLiveStatus) -> Stream {
    let live_id = live.live_id.unwrap_or(0);
    let live_url = if live_id > 0 {
        format!("https://chzzk.naver.com/live/{channel_id}")
    } else {
        String::new()
    };

    Stream {
        id: String::new(), // YouTube ID 없음
        title: live.live_title.clone().unwrap_or_default(),
        channel_id: String::new(),
        channel_name: live
            .channel
            .as_ref()
            .and_then(|c| c.channel_name.clone())
            .unwrap_or_default(),
        status: StreamStatus::Live,
        start_scheduled: None,
        start_actual: None,
        duration: None,
        thumbnail: None,
        link: None,
        topic_id: None,
        channel: None,
        viewer_count: live.concurrent_user_count,
        // Chzzk 전용 필드
        chzzk_channel_id: channel_id.to_string(),
        chzzk_live_id: live_id,
        chzzk_live_url: live_url,
        is_integrated: false,
        is_chzzk_only: true,
        // Twitch 필드 기본값
        twitch_user_id: String::new(),
        twitch_user_login: String::new(),
        twitch_stream_id: String::new(),
        twitch_live_url: String::new(),
        is_twitch_only: false,
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP Chzzk 클라이언트 구현 (서킷 브레이커 포함)
// ─────────────────────────────────────────────────────────────────────────────

/// reqwest 기반 Chzzk HTTP 클라이언트 — 서킷 브레이커 내장
pub struct HttpChzzkClient {
    http: ClientWithMiddleware,
    base_url: String,
    circuit: CircuitBreaker,
}

impl HttpChzzkClient {
    const RETRY_MIN_BACKOFF_MS: u64 = 100;
    const RETRY_MAX_BACKOFF_MS: u64 = 500;
    const RETRY_MAX_RETRIES: u32 = 2;

    pub fn new(config: &ChzzkConfig) -> Result<Self, AlarmError> {
        let base_http = Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| AlarmError::Config(format!("Chzzk HTTP 클라이언트 생성 실패: {e}")))?;

        let retry_policy = ExponentialBackoff::builder()
            .retry_bounds(
                Duration::from_millis(Self::RETRY_MIN_BACKOFF_MS),
                Duration::from_millis(Self::RETRY_MAX_BACKOFF_MS),
            )
            .build_with_max_retries(Self::RETRY_MAX_RETRIES);
        let http = ClientBuilder::new(base_http)
            .with(RetryTransientMiddleware::new_with_policy(retry_policy))
            .build();

        let circuit = CircuitBreaker::new(
            config.circuit_failure_threshold,
            Duration::from_secs(config.circuit_reset_secs),
            "chzzk",
        );

        Ok(Self {
            http,
            base_url: config.base_url.trim_end_matches('/').to_string(),
            circuit,
        })
    }

    /// 서킷 브레이커 현재 상태 (테스트용)
    #[cfg(test)]
    pub fn circuit_state(&self) -> crate::circuit_breaker::CircuitState {
        self.circuit.state()
    }
}

#[async_trait]
impl ChzzkClient for HttpChzzkClient {
    async fn get_live_status(&self, channel_id: &str) -> Result<Option<Stream>, AlarmError> {
        // 서킷 브레이커 확인 — Open이면 즉시 에러 반환
        self.circuit.allow_request()?;

        let url = format!(
            "{}/service/v3/channels/{}/live-detail",
            self.base_url, channel_id
        );

        let resp = self.http.get(&url).send().await.map_err(|e| {
            self.circuit.record_failure();
            AlarmError::Http(format!("Chzzk 요청 실패: {e}"))
        })?;

        if !resp.status().is_success() {
            self.circuit.record_failure();
            return Err(AlarmError::Api {
                platform: "chzzk".into(),
                message: format!("HTTP {}", resp.status()),
            });
        }

        let body: ChzzkApiResponse = resp.json().await.map_err(|e| {
            self.circuit.record_failure();
            AlarmError::Http(format!("Chzzk 응답 파싱 실패: {e}"))
        })?;

        self.circuit.record_success();

        let stream = body.content.as_ref().and_then(|live| {
            // 라이브 중인 경우만 반환
            if live.status.as_deref() == Some("OPEN") {
                debug!("Chzzk 채널 {} 라이브 중", channel_id);
                Some(to_stream(channel_id, live))
            } else {
                None
            }
        });

        Ok(stream)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock 구현 (테스트용)
// ─────────────────────────────────────────────────────────────────────────────

/// 테스트용 Chzzk 클라이언트 Mock
pub struct MockChzzkClient {
    live_stream: Option<Stream>,
    /// Some(err_msg)이면 해당 에러를 반환
    error: Option<String>,
}

impl MockChzzkClient {
    pub fn new(live_stream: Option<Stream>) -> Self {
        Self {
            live_stream,
            error: None,
        }
    }

    pub fn with_error(message: impl Into<String>) -> Self {
        Self {
            live_stream: None,
            error: Some(message.into()),
        }
    }
}

#[async_trait]
impl ChzzkClient for MockChzzkClient {
    async fn get_live_status(&self, _channel_id: &str) -> Result<Option<Stream>, AlarmError> {
        if let Some(msg) = &self.error {
            return Err(AlarmError::Http(msg.clone()));
        }
        Ok(self.live_stream.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::circuit_breaker::{CircuitBreaker, CircuitState};

    fn make_live_stream(channel_id: &str) -> Stream {
        Stream {
            id: String::new(),
            title: "라이브 방송".into(),
            channel_id: String::new(),
            channel_name: "테스트 채널".into(),
            status: StreamStatus::Live,
            start_scheduled: None,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: Some(100),
            chzzk_channel_id: channel_id.into(),
            chzzk_live_id: 99,
            chzzk_live_url: format!("https://chzzk.naver.com/live/{channel_id}"),
            is_integrated: false,
            is_chzzk_only: true,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: String::new(),
            is_twitch_only: false,
        }
    }

    #[tokio::test]
    async fn mock_returns_live_stream() {
        let stream = make_live_stream("ch_abc");
        let client = MockChzzkClient::new(Some(stream));

        let result = client.get_live_status("ch_abc").await.unwrap();
        assert!(result.is_some());
        assert_eq!(result.unwrap().chzzk_channel_id, "ch_abc");
    }

    #[tokio::test]
    async fn mock_returns_none_when_offline() {
        let client = MockChzzkClient::new(None);
        let result = client.get_live_status("ch_abc").await.unwrap();
        assert!(result.is_none());
    }

    #[tokio::test]
    async fn mock_returns_error() {
        let client = MockChzzkClient::with_error("timeout");
        let result = client.get_live_status("ch_abc").await;
        assert!(result.is_err());
    }

    #[test]
    fn circuit_breaker_opens_after_failures() {
        let cb = CircuitBreaker::new(3, Duration::from_secs(30), "chzzk");
        cb.record_failure();
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Closed);
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Open);
    }

    #[test]
    fn circuit_breaker_half_open_after_reset() {
        let cb = CircuitBreaker::new(1, Duration::ZERO, "chzzk");
        cb.record_failure();
        // reset_duration=0 → 즉시 HalfOpen
        assert_eq!(cb.state(), CircuitState::HalfOpen);
    }

    #[test]
    fn circuit_breaker_recovers_from_half_open() {
        let cb = CircuitBreaker::new(1, Duration::ZERO, "chzzk");
        cb.record_failure();
        let _ = cb.state(); // HalfOpen 전환 트리거
        cb.record_success();
        assert_eq!(cb.state(), CircuitState::Closed);
    }
}
