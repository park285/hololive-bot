use std::{
    sync::Arc,
    time::{Duration, Instant},
};

use alarm_core::{
    error::AlarmError,
    model::{Stream, StreamStatus},
};
use async_trait::async_trait;
use reqwest::Client;
use secrecy::{ExposeSecret, SecretString};
use serde::{Deserialize, Serialize};
use tokio::sync::Mutex;
use tracing::{debug, warn};

use crate::{circuit_breaker::CircuitBreaker, config::TwitchConfig};

/// Twitch 스트림 조회 인터페이스
#[async_trait]
pub trait TwitchClient: Send + Sync {
    /// user_login 목록 기반 라이브 스트림 배치 조회
    async fn get_streams(&self, user_logins: &[&str]) -> Result<Vec<Stream>, AlarmError>;
}

// ─────────────────────────────────────────────────────────────────────────────
// Twitch API 응답 역직렬화 구조체 (내부 전용)
// ─────────────────────────────────────────────────────────────────────────────

/// OAuth2 토큰 응답
#[derive(Debug, Deserialize)]
struct TokenResponse {
    access_token: String,
    expires_in: u64,
}

/// Twitch /helix/streams 응답
#[derive(Debug, Deserialize)]
struct TwitchStreamsResponse {
    data: Vec<TwitchStream>,
}

#[derive(Debug, Deserialize)]
struct TwitchStream {
    id: String,
    user_id: String,
    user_login: String,
    title: String,
    viewer_count: Option<i32>,
    #[serde(default)]
    thumbnail_url: Option<String>,
}

/// Twitch 스트림을 alarm_core::model::Stream으로 변환
fn to_stream(s: TwitchStream) -> Stream {
    let live_url = format!("https://twitch.tv/{}", s.user_login);
    Stream {
        id: String::new(),
        title: s.title,
        channel_id: String::new(),
        channel_name: s.user_login.clone(),
        status: StreamStatus::Live,
        start_scheduled: None,
        start_actual: None,
        duration: None,
        thumbnail: s.thumbnail_url,
        link: None,
        topic_id: None,
        channel: None,
        viewer_count: s.viewer_count,
        // Chzzk 필드 기본값
        chzzk_channel_id: String::new(),
        chzzk_live_id: 0,
        chzzk_live_url: String::new(),
        is_integrated: false,
        is_chzzk_only: false,
        // Twitch 전용 필드
        twitch_user_id: s.user_id,
        twitch_user_login: s.user_login,
        twitch_stream_id: s.id,
        twitch_live_url: live_url,
        is_twitch_only: true,
    }
}

fn redact_reqwest_error(err: reqwest::Error) -> String {
    // reqwest error가 포함할 수 있는 URL(userinfo/query)을 제거
    err.without_url().to_string()
}

// ─────────────────────────────────────────────────────────────────────────────
// OAuth2 토큰 캐시 (만료 전 자동 갱신)
// ─────────────────────────────────────────────────────────────────────────────

/// OAuth2 액세스 토큰 캐시
#[derive(Clone)]
struct TokenCache {
    token: String,
    /// 만료 시각 (여유 60초 전에 갱신)
    expires_at: Instant,
}

/// OAuth2 토큰 관리자 — 만료 전 자동 갱신
struct TokenManager {
    cache: Mutex<Option<TokenCache>>,
    http: Client,
    auth_url: String,
    client_id: String,
    client_secret: SecretString,
}

impl TokenManager {
    fn new(http: Client, auth_url: String, client_id: String, client_secret: SecretString) -> Self {
        Self {
            cache: Mutex::new(None),
            http,
            auth_url,
            client_id,
            client_secret,
        }
    }

    /// 유효한 토큰 반환 (만료 임박 시 자동 갱신)
    async fn get_token(&self) -> Result<String, AlarmError> {
        let mut cache = self.cache.lock().await;

        // 토큰이 있고 만료까지 60초 이상 남았으면 그대로 사용
        if let Some(ref c) = *cache
            && c.expires_at > Instant::now()
        {
            return Ok(c.token.clone());
        }

        debug!("Twitch OAuth2 토큰 갱신 중");
        let token = self.fetch_new_token().await?;
        *cache = Some(token.clone());
        Ok(token.token)
    }

    /// Twitch OAuth2 client_credentials 토큰 발급
    async fn fetch_new_token(&self) -> Result<TokenCache, AlarmError> {
        #[derive(Serialize)]
        struct TokenRequest<'a> {
            client_id: &'a str,
            client_secret: &'a str,
            grant_type: &'static str,
        }

        let resp = self
            .http
            .post(&self.auth_url)
            .form(&TokenRequest {
                client_id: &self.client_id,
                client_secret: self.client_secret.expose_secret(),
                grant_type: "client_credentials",
            })
            .send()
            .await
            .map_err(|e| {
                AlarmError::Http(format!(
                    "Twitch 토큰 요청 실패: {}",
                    redact_reqwest_error(e)
                ))
            })?;

        if !resp.status().is_success() {
            return Err(AlarmError::Api {
                platform: "twitch".into(),
                message: format!("토큰 발급 HTTP {}", resp.status()),
            });
        }

        let body: TokenResponse = resp.json().await.map_err(|e| {
            AlarmError::Http(format!(
                "Twitch 토큰 응답 파싱 실패: {}",
                redact_reqwest_error(e)
            ))
        })?;

        // 만료 60초 전에 갱신하도록 여유 설정
        let expires_at = Instant::now() + Duration::from_secs(body.expires_in.saturating_sub(60));

        debug!("Twitch 토큰 발급 완료 (expires_in={}s)", body.expires_in);
        Ok(TokenCache {
            token: body.access_token,
            expires_at,
        })
    }

    /// 토큰 캐시 무효화 (401 수신 시 강제 갱신용)
    async fn invalidate(&self) {
        let mut cache = self.cache.lock().await;
        *cache = None;
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP Twitch 클라이언트 구현
// ─────────────────────────────────────────────────────────────────────────────

/// reqwest 기반 Twitch HTTP 클라이언트
/// OAuth2 자동 갱신 + 401 재시도 + 서킷 브레이커를 지원한다.
pub struct HttpTwitchClient {
    http: Client,
    base_url: String,
    client_id: String,
    token_manager: Arc<TokenManager>,
    circuit: CircuitBreaker,
}

impl HttpTwitchClient {
    pub fn new(config: &TwitchConfig) -> Result<Self, AlarmError> {
        let http = Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| AlarmError::Config(format!("Twitch HTTP 클라이언트 생성 실패: {e}")))?;

        let token_manager = Arc::new(TokenManager::new(
            http.clone(),
            config.auth_url.clone(),
            config.client_id.clone(),
            config.client_secret.clone(),
        ));

        let circuit = CircuitBreaker::new(
            config.circuit_failure_threshold,
            Duration::from_secs(config.circuit_reset_secs),
            "twitch",
        );

        Ok(Self {
            http,
            base_url: config.base_url.trim_end_matches('/').to_string(),
            client_id: config.client_id.clone(),
            token_manager,
            circuit,
        })
    }

    /// 스트림 목록 조회 내부 구현 (재시도 제어 포함)
    async fn fetch_streams(
        &self,
        user_logins: &[&str],
        retry: bool,
    ) -> Result<Vec<Stream>, AlarmError> {
        let token = self.token_manager.get_token().await?;
        let url = format!("{}/streams", self.base_url);

        let resp = self
            .http
            .get(&url)
            .header("Authorization", format!("Bearer {token}"))
            .header("Client-Id", &self.client_id)
            .query(
                &user_logins
                    .iter()
                    .map(|l| ("user_login", *l))
                    .collect::<Vec<_>>(),
            )
            .send()
            .await
            .map_err(|e| {
                self.circuit.record_failure();
                AlarmError::Http(format!(
                    "Twitch 스트림 요청 실패: {}",
                    redact_reqwest_error(e)
                ))
            })?;

        // 401 → 토큰 무효화 후 1회 재시도
        if resp.status() == 401 {
            warn!("Twitch 401 수신 — 토큰 갱신 후 재시도");
            self.token_manager.invalidate().await;

            if retry {
                return Box::pin(self.fetch_streams(user_logins, false)).await;
            }
            self.circuit.record_failure();
            return Err(AlarmError::Api {
                platform: "twitch".into(),
                message: "인증 실패 (401)".into(),
            });
        }

        if !resp.status().is_success() {
            self.circuit.record_failure();
            return Err(AlarmError::Api {
                platform: "twitch".into(),
                message: format!("HTTP {}", resp.status()),
            });
        }

        let body: TwitchStreamsResponse = resp.json().await.map_err(|e| {
            self.circuit.record_failure();
            AlarmError::Http(format!(
                "Twitch 응답 파싱 실패: {}",
                redact_reqwest_error(e)
            ))
        })?;

        self.circuit.record_success();
        debug!("Twitch 스트림 조회 성공: {} 스트림", body.data.len());
        Ok(body.data.into_iter().map(to_stream).collect())
    }
}

#[async_trait]
impl TwitchClient for HttpTwitchClient {
    async fn get_streams(&self, user_logins: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        if user_logins.is_empty() {
            return Ok(vec![]);
        }
        // 서킷 브레이커 확인
        self.circuit.allow_request()?;
        self.fetch_streams(user_logins, true).await
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock 구현 (테스트용)
// ─────────────────────────────────────────────────────────────────────────────

/// 테스트용 Twitch 클라이언트 Mock
pub struct MockTwitchClient {
    streams: Vec<Stream>,
    error: Option<String>,
}

impl MockTwitchClient {
    pub fn new(streams: Vec<Stream>) -> Self {
        Self {
            streams,
            error: None,
        }
    }

    pub fn with_error(message: impl Into<String>) -> Self {
        Self {
            streams: vec![],
            error: Some(message.into()),
        }
    }
}

#[async_trait]
impl TwitchClient for MockTwitchClient {
    async fn get_streams(&self, user_logins: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        if let Some(msg) = &self.error {
            return Err(AlarmError::Http(msg.clone()));
        }
        let result = self
            .streams
            .iter()
            .filter(|s| user_logins.contains(&s.twitch_user_login.as_str()))
            .cloned()
            .collect();
        Ok(result)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_twitch_stream(user_login: &str) -> Stream {
        Stream {
            id: String::new(),
            title: "Twitch 방송".into(),
            channel_id: String::new(),
            channel_name: user_login.into(),
            status: StreamStatus::Live,
            start_scheduled: None,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: Some(500),
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: String::new(),
            is_integrated: false,
            is_chzzk_only: false,
            twitch_user_id: format!("uid_{user_login}"),
            twitch_user_login: user_login.into(),
            twitch_stream_id: format!("sid_{user_login}"),
            twitch_live_url: format!("https://twitch.tv/{user_login}"),
            is_twitch_only: true,
        }
    }

    #[tokio::test]
    async fn mock_filters_by_user_login() {
        let streams = vec![
            make_twitch_stream("streamer_a"),
            make_twitch_stream("streamer_b"),
            make_twitch_stream("streamer_c"),
        ];
        let client = MockTwitchClient::new(streams);

        let result = client
            .get_streams(&["streamer_a", "streamer_c"])
            .await
            .unwrap();
        assert_eq!(result.len(), 2);
    }

    #[tokio::test]
    async fn mock_empty_input_returns_empty() {
        let client = MockTwitchClient::new(vec![]);
        let result = client.get_streams(&[]).await.unwrap();
        assert!(result.is_empty());
    }

    #[tokio::test]
    async fn mock_returns_error() {
        let client = MockTwitchClient::with_error("service unavailable");
        let result = client.get_streams(&["streamer_a"]).await;
        assert!(result.is_err());
    }

    #[test]
    fn circuit_opens_after_threshold() {
        use crate::circuit_breaker::{CircuitBreaker, CircuitState};
        let cb = CircuitBreaker::new(5, Duration::from_secs(60), "twitch");
        for _ in 0..5 {
            cb.record_failure();
        }
        assert_eq!(cb.state(), CircuitState::Open);
    }
}
