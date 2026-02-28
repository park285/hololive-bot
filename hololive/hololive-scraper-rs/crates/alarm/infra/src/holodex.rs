use std::{
    num::NonZeroU32,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::Duration,
};

use alarm_core::{
    error::AlarmError,
    model::{Channel, Stream, StreamStatus},
};
use async_trait::async_trait;
use futures::{StreamExt, stream};
use governor::{
    Quota, RateLimiter as GovernorRateLimiter,
    clock::DefaultClock,
    state::{InMemoryState, direct::NotKeyed},
};
use reqwest::Client;
use reqwest_middleware::{ClientBuilder, ClientWithMiddleware};
use reqwest_retry::{RetryTransientMiddleware, policies::ExponentialBackoff};
use serde::Deserialize;
use tracing::{debug, warn};

use crate::{circuit_breaker::CircuitBreaker, config::HolodexConfig};

/// Holodex 배치 조회 최대 채널 수
const HOLODEX_BATCH_SIZE: usize = 50;

/// Holodex API 클라이언트 인터페이스
#[async_trait]
pub trait HolodexClient: Send + Sync {
    /// 복수 채널의 live/upcoming 스트림 배치 조회
    async fn get_live_streams(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError>;
    /// 단일 채널 스트림 조회 (배치 실패 시 폴백 용도)
    async fn get_channel_streams(&self, channel_id: &str) -> Result<Vec<Stream>, AlarmError>;
}

// ─────────────────────────────────────────────────────────────────────────────
// Holodex API 응답 역직렬화 구조체 (내부 전용)
// ─────────────────────────────────────────────────────────────────────────────

/// Holodex /users/live 응답 단일 스트림
#[derive(Debug, Deserialize)]
struct HolodexStream {
    id: String,
    title: String,
    #[serde(default)]
    channel_id: String,
    status: String,
    #[serde(default)]
    start_scheduled: Option<String>,
    #[serde(default)]
    start_actual: Option<String>,
    #[serde(default)]
    duration: Option<i32>,
    #[serde(default)]
    thumbnail: Option<String>,
    #[serde(default)]
    link: Option<String>,
    #[serde(default)]
    topic_id: Option<String>,
    #[serde(default, alias = "live_viewers")]
    viewer_count: Option<i32>,
    #[serde(default)]
    channel: Option<HolodexChannel>,
}

#[derive(Debug, Deserialize)]
struct HolodexChannel {
    id: String,
    name: String,
    #[serde(default)]
    english_name: Option<String>,
    #[serde(default)]
    photo: Option<String>,
    #[serde(default)]
    twitter: Option<String>,
    #[serde(default)]
    video_count: Option<i32>,
    #[serde(default)]
    subscriber_count: Option<i32>,
    #[serde(default)]
    org: Option<String>,
    #[serde(default)]
    suborg: Option<String>,
    #[serde(default)]
    group: Option<String>,
}

/// Holodex 응답을 alarm_core::model::Stream으로 변환
///
/// 채널 식별자가 없는 레코드는 채널별 구독 매핑이 불가능하므로 버린다.
fn to_stream(s: HolodexStream) -> Option<Stream> {
    let channel_id = if s.channel_id.is_empty() {
        s.channel.as_ref().map(|c| c.id.clone()).unwrap_or_default()
    } else {
        s.channel_id.clone()
    };
    if channel_id.is_empty() {
        warn!(
            stream_id = s.id,
            title = s.title,
            "Holodex stream missing channel_id and channel.id; dropping record"
        );
        return None;
    }

    let channel_name = s
        .channel
        .as_ref()
        .map(|c| c.name.clone())
        .unwrap_or_default();

    let channel = s.channel.as_ref().map(|c| Channel {
        id: c.id.clone(),
        name: c.name.clone(),
        english_name: c.english_name.clone(),
        photo: c.photo.clone(),
        twitter: c.twitter.clone(),
        video_count: c.video_count,
        subscriber_count: c.subscriber_count,
        org: c.org.clone(),
        suborg: c.suborg.clone(),
        group: c.group.clone(),
    });

    let status = match s.status.as_str() {
        "live" => StreamStatus::Live,
        "upcoming" => StreamStatus::Upcoming,
        _ => StreamStatus::Past,
    };

    let start_scheduled = s.start_scheduled.as_deref().and_then(|t| t.parse().ok());
    let start_actual = s.start_actual.as_deref().and_then(|t| t.parse().ok());

    Some(Stream {
        id: s.id,
        title: s.title,
        channel_id,
        channel_name,
        status,
        start_scheduled,
        start_actual,
        duration: s.duration,
        thumbnail: s.thumbnail,
        link: s.link,
        topic_id: s.topic_id,
        channel,
        viewer_count: s.viewer_count,
        // Chzzk/Twitch 필드는 Holodex 응답에 없음 — 기본값
        chzzk_channel_id: String::new(),
        chzzk_live_id: 0,
        chzzk_live_url: String::new(),
        is_integrated: false,
        is_chzzk_only: false,
        twitch_user_id: String::new(),
        twitch_user_login: String::new(),
        twitch_stream_id: String::new(),
        twitch_live_url: String::new(),
        is_twitch_only: false,
    })
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP Holodex 클라이언트 구현
// ─────────────────────────────────────────────────────────────────────────────

/// reqwest 기반 Holodex HTTP 클라이언트
/// API 키 로테이션 + rate limiting + 배치 실패 시 개별 폴백을 지원한다.
pub struct HttpHolodexClient {
    http: ClientWithMiddleware,
    base_url: String,
    api_keys: Vec<String>,
    /// 다음에 사용할 키 인덱스 (atomic round-robin)
    key_index: Arc<AtomicUsize>,
    rate_limiter: Arc<GovernorRateLimiter<NotKeyed, InMemoryState, DefaultClock>>,
    circuit: CircuitBreaker,
}

impl HttpHolodexClient {
    const RETRY_MIN_BACKOFF_MS: u64 = 100;
    const RETRY_MAX_BACKOFF_MS: u64 = 500;
    const RETRY_MAX_RETRIES: u32 = 2;

    fn build_rate_limiter(
        min_interval_ms: u64,
    ) -> GovernorRateLimiter<NotKeyed, InMemoryState, DefaultClock> {
        let min_interval = Duration::from_millis(min_interval_ms.max(1));
        let burst = NonZeroU32::new(1).expect("1 is non-zero");
        let quota = Quota::with_period(min_interval)
            .unwrap_or_else(|| Quota::per_second(burst))
            .allow_burst(burst);

        GovernorRateLimiter::direct(quota)
    }

    pub fn new(config: &HolodexConfig) -> Result<Self, AlarmError> {
        if config.api_keys.is_empty() {
            return Err(AlarmError::Config("Holodex API 키가 설정되지 않음".into()));
        }

        let base_http = Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| AlarmError::Config(format!("HTTP 클라이언트 생성 실패: {e}")))?;
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
            "holodex",
        );

        Ok(Self {
            http,
            base_url: config.base_url.trim_end_matches('/').to_string(),
            api_keys: config.api_keys.clone(),
            key_index: Arc::new(AtomicUsize::new(0)),
            rate_limiter: Arc::new(Self::build_rate_limiter(config.rate_limit_ms)),
            circuit,
        })
    }

    /// 서킷 브레이커 현재 상태 (테스트용)
    #[cfg(test)]
    pub fn circuit_state(&self) -> crate::circuit_breaker::CircuitState {
        self.circuit.state()
    }

    /// 현재 API 키 반환
    fn current_key(&self) -> &str {
        let idx = self.key_index.load(Ordering::Relaxed) % self.api_keys.len();
        &self.api_keys[idx]
    }

    /// 다음 API 키로 로테이션
    fn rotate_key(&self) {
        self.key_index.fetch_add(1, Ordering::Relaxed);
        let new_idx = self.key_index.load(Ordering::Relaxed) % self.api_keys.len();
        warn!("Holodex API 키 로테이션 → 인덱스 {}", new_idx);
    }

    /// 배치 조회 — channel_ids를 HOLODEX_BATCH_SIZE 단위로 청크하여 조회
    async fn fetch_batch(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        let mut all_streams = Vec::new();
        for chunk in channel_ids.chunks(HOLODEX_BATCH_SIZE) {
            let streams = self.fetch_batch_chunk(chunk).await?;
            all_streams.extend(streams);
        }
        Ok(all_streams)
    }

    /// 단일 청크(≤50) 배치 조회
    async fn fetch_batch_chunk(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        self.circuit.allow_request()?;
        self.rate_limiter.until_ready().await;

        let channels_param = channel_ids.join(",");
        let url = format!("{}/users/live", self.base_url);

        let resp = self
            .http
            .get(&url)
            .header("X-APIKEY", self.current_key())
            .query(&[("channels", channels_param.as_str())])
            .send()
            .await
            .map_err(|e| {
                self.circuit.record_failure();
                AlarmError::Http(format!("Holodex 배치 요청 실패: {e}"))
            })?;

        if resp.status() == 429 {
            // Rate limit — 키 로테이션
            self.rotate_key();
            self.circuit.record_failure();
            return Err(AlarmError::Api {
                platform: "holodex".into(),
                message: "rate limited (429)".into(),
            });
        }

        if !resp.status().is_success() {
            let status = resp.status();
            self.circuit.record_failure();
            return Err(AlarmError::Api {
                platform: "holodex".into(),
                message: format!("HTTP {status}"),
            });
        }

        let streams: Vec<HolodexStream> = resp.json().await.map_err(|e| {
            self.circuit.record_failure();
            AlarmError::Http(format!("Holodex 응답 파싱 실패: {e}"))
        })?;

        self.circuit.record_success();
        let raw_count = streams.len();
        let mapped: Vec<Stream> = streams.into_iter().filter_map(to_stream).collect();
        let dropped = raw_count.saturating_sub(mapped.len());
        if dropped > 0 {
            warn!(
                dropped,
                raw_count, "Holodex 청크 응답 중 channel_id 누락 레코드를 건너뜀"
            );
        }
        debug!(
            "Holodex 청크 조회 성공: {} 스트림 (채널 {}개)",
            mapped.len(),
            channel_ids.len()
        );
        Ok(mapped)
    }

    /// 단일 채널 조회 — 배치 실패 시 폴백
    async fn fetch_single(&self, channel_id: &str) -> Result<Vec<Stream>, AlarmError> {
        self.fetch_batch_chunk(&[channel_id]).await
    }
}

#[async_trait]
impl HolodexClient for HttpHolodexClient {
    async fn get_live_streams(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        if channel_ids.is_empty() {
            return Ok(vec![]);
        }

        match self.fetch_batch(channel_ids).await {
            Ok(streams) => Ok(streams),
            Err(e @ AlarmError::CircuitOpen { .. }) => Err(e),
            Err(e) => {
                // 배치 실패 → 채널별 개별 폴백
                // (fallback 단일 책임: service 계층에서는 추가 폴백을 두지 않는다)
                warn!("Holodex 배치 실패 ({e}), 채널별 개별 조회로 폴백");
                const FALLBACK_CONCURRENCY: usize = 8;

                let mut all = Vec::new();
                let fallback_ids: Vec<String> =
                    channel_ids.iter().map(|id| (*id).to_string()).collect();
                let mut fallback_fetches =
                    stream::iter(fallback_ids.into_iter().map(|id| async move {
                        let result = self.fetch_single(&id).await;
                        (id, result)
                    }))
                    .buffer_unordered(FALLBACK_CONCURRENCY);

                while let Some((id, result)) = fallback_fetches.next().await {
                    match result {
                        Ok(mut s) => all.append(&mut s),
                        Err(fe @ AlarmError::CircuitOpen { .. }) => {
                            warn!("Holodex 서킷 OPEN으로 폴백 중단: {fe}");
                            return Err(fe);
                        }
                        Err(fe) => {
                            warn!("Holodex 채널 {id} 개별 조회 실패: {fe}");
                        }
                    }
                }
                Ok(all)
            }
        }
    }

    async fn get_channel_streams(&self, channel_id: &str) -> Result<Vec<Stream>, AlarmError> {
        self.fetch_single(channel_id).await
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock 구현 (테스트용)
// ─────────────────────────────────────────────────────────────────────────────

/// 테스트용 Holodex 클라이언트 Mock
pub struct MockHolodexClient {
    streams: Vec<Stream>,
}

impl MockHolodexClient {
    pub fn new(streams: Vec<Stream>) -> Self {
        Self { streams }
    }
}

#[async_trait]
impl HolodexClient for MockHolodexClient {
    async fn get_live_streams(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        let result = self
            .streams
            .iter()
            .filter(|s| channel_ids.contains(&s.channel_id.as_str()))
            .cloned()
            .collect();
        Ok(result)
    }

    async fn get_channel_streams(&self, channel_id: &str) -> Result<Vec<Stream>, AlarmError> {
        let result = self
            .streams
            .iter()
            .filter(|s| s.channel_id == channel_id)
            .cloned()
            .collect();
        Ok(result)
    }
}

#[cfg(test)]
mod tests {
    use alarm_core::model::StreamStatus;

    use super::*;
    use crate::circuit_breaker::CircuitState;

    fn make_stream(channel_id: &str, status: StreamStatus) -> Stream {
        Stream {
            id: format!("vid_{channel_id}"),
            title: "테스트".into(),
            channel_id: channel_id.into(),
            channel_name: "채널명".into(),
            status,
            start_scheduled: None,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: None,
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: String::new(),
            is_integrated: false,
            is_chzzk_only: false,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: String::new(),
            is_twitch_only: false,
        }
    }

    #[tokio::test]
    async fn mock_get_live_streams_filters_by_channel_id() {
        let streams = vec![
            make_stream("UC_A", StreamStatus::Live),
            make_stream("UC_B", StreamStatus::Upcoming),
            make_stream("UC_C", StreamStatus::Live),
        ];
        let client = MockHolodexClient::new(streams);

        let result = client.get_live_streams(&["UC_A", "UC_C"]).await.unwrap();
        assert_eq!(result.len(), 2);
        assert!(
            result
                .iter()
                .all(|s| s.channel_id == "UC_A" || s.channel_id == "UC_C")
        );
    }

    #[tokio::test]
    async fn mock_get_channel_streams_returns_only_matching() {
        let streams = vec![
            make_stream("UC_A", StreamStatus::Live),
            make_stream("UC_B", StreamStatus::Live),
        ];
        let client = MockHolodexClient::new(streams);

        let result = client.get_channel_streams("UC_A").await.unwrap();
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].channel_id, "UC_A");
    }

    #[tokio::test]
    async fn mock_get_live_streams_empty_input() {
        let client = MockHolodexClient::new(vec![]);
        let result = client.get_live_streams(&[]).await.unwrap();
        assert!(result.is_empty());
    }

    fn make_http_config() -> HolodexConfig {
        HolodexConfig {
            base_url: "http://127.0.0.1:9".into(),
            api_keys: vec!["dummy-key".into()],
            timeout_secs: 1,
            rate_limit_ms: 0,
            circuit_failure_threshold: 1,
            circuit_reset_secs: 60,
        }
    }

    #[tokio::test]
    async fn http_client_opens_circuit_after_request_failure() {
        let client = HttpHolodexClient::new(&make_http_config()).expect("client should be created");

        let first = client.get_channel_streams("UC_TEST").await;
        assert!(
            first.is_err(),
            "first request should fail and count toward circuit"
        );
        assert_eq!(client.circuit_state(), CircuitState::Open);

        let second = client.get_channel_streams("UC_TEST").await;
        assert!(
            matches!(second, Err(AlarmError::CircuitOpen { .. })),
            "second request should be short-circuited when circuit is open"
        );
    }

    #[test]
    fn to_stream_falls_back_to_channel_object_id() {
        let raw: HolodexStream = serde_json::from_str(
            r#"
            {
                "id":"vid_1",
                "title":"테스트",
                "status":"upcoming",
                "start_scheduled":"2026-02-24T15:00:00.000Z",
                "live_viewers":123,
                "channel":{"id":"UC_TEST","name":"테스트 채널"}
            }
            "#,
        )
        .expect("json should parse");

        let stream = to_stream(raw).expect("stream should map with fallback channel id");
        assert_eq!(stream.channel_id, "UC_TEST");
        assert_eq!(stream.viewer_count, Some(123));
        assert_eq!(stream.channel_name, "테스트 채널");
    }

    #[test]
    fn to_stream_keeps_explicit_channel_id() {
        let raw: HolodexStream = serde_json::from_str(
            r#"
            {
                "id":"vid_2",
                "title":"테스트2",
                "channel_id":"UC_EXPLICIT",
                "status":"live",
                "channel":{"id":"UC_FALLBACK","name":"폴백 채널"}
            }
            "#,
        )
        .expect("json should parse");

        let stream = to_stream(raw).expect("stream should map with explicit channel id");
        assert_eq!(stream.channel_id, "UC_EXPLICIT");
    }

    #[test]
    fn to_stream_returns_none_without_any_channel_id_source() {
        let raw: HolodexStream = serde_json::from_str(
            r#"
            {
                "id":"vid_3",
                "title":"테스트3",
                "status":"live"
            }
            "#,
        )
        .expect("json should parse");

        assert!(to_stream(raw).is_none());
    }
}
