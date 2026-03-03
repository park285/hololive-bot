use secrecy::{ExposeSecret, SecretString};
use serde::Deserialize;
use validator::Validate;

/// PostgreSQL 연결 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct DatabaseConfig {
    pub host: String,
    #[validate(range(min = 1, max = 65535))]
    pub port: u16,
    pub name: String,
    pub user: String,
    pub password: SecretString,
    pub sslmode: String,
    #[validate(range(min = 1))]
    pub max_connections: u32,
}

impl DatabaseConfig {
    /// postgres:// 형식 URL 생성 (URL 인코딩 적용)
    pub fn database_url(&self) -> String {
        let encoded_user: String =
            url::form_urlencoded::byte_serialize(self.user.as_bytes()).collect();
        let encoded_password: String =
            url::form_urlencoded::byte_serialize(self.password.expose_secret().as_bytes())
                .collect();
        let encoded_name: String =
            url::form_urlencoded::byte_serialize(self.name.as_bytes()).collect();

        format!(
            "postgres://{}:{}@{}:{}/{}?sslmode={}",
            encoded_user,
            encoded_password,
            self.host,
            self.port,
            encoded_name,
            normalize_ssl_mode(&self.sslmode)
        )
    }
}

fn normalize_ssl_mode(mode: &str) -> &'static str {
    match mode.trim().to_ascii_lowercase().as_str() {
        "disable" => "disable",
        "allow" => "allow",
        "prefer" => "prefer",
        "require" => "require",
        "verify-ca" => "verify-ca",
        "verify-full" => "verify-full",
        _ => "require",
    }
}

/// HTTP 헬스체크 포트 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct HealthConfig {
    #[validate(range(min = 1, max = 65535))]
    pub port: u16,
}

/// 로깅 설정 (stdout 전용, Fluent Bit → Loki 정책)
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct LoggingConfig {
    pub level: String,
}

/// 알람 서비스 동작 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct AlarmConfig {
    /// false 시 Twitch 루프를 완전히 비활성화
    pub twitch_enabled: bool,
    /// 알림 발송 분 목록 (예: [5, 3, 1])
    #[validate(length(min = 1))]
    pub target_minutes: Vec<i32>,
    /// Chzzk 폴링 간격(초)
    #[validate(range(min = 1))]
    pub chzzk_poll_secs: u64,
    /// Twitch 폴링 간격(초)
    #[validate(range(min = 1))]
    pub twitch_poll_secs: u64,
    /// YouTube 체크 1회 타임아웃(초)
    #[validate(range(min = 1))]
    pub youtube_check_timeout_secs: u64,
    /// Chzzk 체크 1회 타임아웃(초)
    #[validate(range(min = 1))]
    pub chzzk_check_timeout_secs: u64,
    /// Twitch 체크 1회 타임아웃(초)
    #[validate(range(min = 1))]
    pub twitch_check_timeout_secs: u64,
}
