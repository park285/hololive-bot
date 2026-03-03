use std::path::Path;

use serde::Deserialize;
use thiserror::Error;
use validator::Validate;

use super::{
    AlarmConfig, ChzzkConfig, DatabaseConfig, HealthConfig, HolodexConfig, IrisConfig,
    LoggingConfig, TelemetryConfig, TwitchConfig, ValkeyConfig,
};

/// 설정 로드 실패 에러
#[derive(Debug, Error)]
pub enum ConfigLoadError {
    #[error("failed to build config source: {0}")]
    Build(#[source] Box<config::ConfigError>),
    #[error("invalid security policy: {0}")]
    Validation(String),
}

impl From<config::ConfigError> for ConfigLoadError {
    fn from(value: config::ConfigError) -> Self {
        Self::Build(Box::new(value))
    }
}

/// 알람 서비스 전체 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct AlarmAppConfig {
    #[validate(nested)]
    pub valkey: ValkeyConfig,
    #[validate(nested)]
    pub holodex: HolodexConfig,
    #[validate(nested)]
    pub chzzk: ChzzkConfig,
    #[validate(nested)]
    pub twitch: TwitchConfig,
    #[validate(nested)]
    pub iris: IrisConfig,
    #[validate(nested)]
    pub database: DatabaseConfig,
    #[validate(nested)]
    pub health: HealthConfig,
    #[validate(nested)]
    pub logging: LoggingConfig,
    #[validate(nested)]
    pub alarm: AlarmConfig,
    #[serde(default)]
    pub telemetry: TelemetryConfig,
}

impl AlarmAppConfig {
    /// 기본 경로(alarm-config.toml)에서 설정 로드
    pub fn load() -> Result<Self, ConfigLoadError> {
        Self::load_from_path("alarm-config.toml")
    }

    /// 지정 경로에서 설정 로드 (ALARM__ 환경변수로 오버라이드 가능)
    pub fn load_from_path(path: impl AsRef<Path>) -> Result<Self, ConfigLoadError> {
        let settings = config::Config::builder()
            // Valkey 기본값
            .set_default("valkey.url", "redis://valkey:6379")?
            .set_default("valkey.pool_size", 4)?
            // Holodex 기본값
            .set_default("holodex.base_url", "https://holodex.net/api/v2")?
            .set_default("holodex.api_keys", Vec::<String>::new())?
            .set_default("holodex.timeout_secs", 30)?
            .set_default("holodex.rate_limit_ms", 100)?
            .set_default("holodex.circuit_failure_threshold", 5)?
            .set_default("holodex.circuit_reset_secs", 30)?
            // Chzzk 기본값
            .set_default("chzzk.base_url", "https://api.chzzk.naver.com")?
            .set_default("chzzk.timeout_secs", 10)?
            .set_default("chzzk.circuit_failure_threshold", 5)?
            .set_default("chzzk.circuit_reset_secs", 30)?
            // Twitch 기본값
            .set_default("twitch.base_url", "https://api.twitch.tv/helix")?
            .set_default("twitch.auth_url", "https://id.twitch.tv/oauth2/token")?
            .set_default("twitch.client_id", "")?
            .set_default("twitch.client_secret", "")?
            .set_default("twitch.timeout_secs", 10)?
            .set_default("twitch.circuit_failure_threshold", 5)?
            .set_default("twitch.circuit_reset_secs", 60)?
            // Iris 기본값
            .set_default("iris.base_url", "http://iris:8080")?
            .set_default("iris.bot_token", "")?
            .set_default("iris.timeout_secs", 10)?
            .set_default("iris.circuit_failure_threshold", 5)?
            .set_default("iris.circuit_reset_secs", 30)?
            // DB 기본값
            .set_default("database.host", "holo-postgres")?
            .set_default("database.port", 5432)?
            .set_default("database.name", "hololive")?
            .set_default("database.user", "hololive_alarm")?
            .set_default("database.password", "")?
            .set_default("database.sslmode", "require")?
            .set_default("database.max_connections", 5)?
            // 헬스체크
            .set_default("health.port", 30011)?
            // 로깅
            .set_default("logging.level", "info")?
            // 텔레메트리 (OTEL)
            .set_default("telemetry.enabled", false)?
            .set_default("telemetry.service_name", "hololive-alarm")?
            .set_default("telemetry.service_version", env!("CARGO_PKG_VERSION"))?
            .set_default("telemetry.environment", "production")?
            .set_default("telemetry.otlp_endpoint", "otel-collector:4317")?
            .set_default("telemetry.otlp_insecure", false)?
            .set_default("telemetry.sample_rate", 1.0)?
            // 알람 동작
            .set_default("alarm.twitch_enabled", true)?
            .set_default("alarm.target_minutes", vec![5, 3, 1])?
            .set_default("alarm.chzzk_poll_secs", 120)?
            .set_default("alarm.twitch_poll_secs", 120)?
            .set_default("alarm.youtube_check_timeout_secs", 45)?
            .set_default("alarm.chzzk_check_timeout_secs", 30)?
            .set_default("alarm.twitch_check_timeout_secs", 30)?
            // TOML 파일 (없으면 무시)
            .add_source(config::File::from(path.as_ref().to_path_buf()).required(false))
            // 환경변수 오버라이드 (ALARM__섹션__키 형식)
            .add_source(
                config::Environment::with_prefix("ALARM")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?;

        let mut loaded: Self = settings.try_deserialize().map_err(ConfigLoadError::from)?;
        loaded.holodex.api_keys = super::external::normalize_api_keys(loaded.holodex.api_keys);
        loaded
            .validate()
            .map_err(|e| ConfigLoadError::Validation(format!("invalid alarm config: {e}")))?;
        super::validate_iris_base_url_policy(&loaded.iris.base_url)
            .map_err(ConfigLoadError::Validation)?;
        Ok(loaded)
    }
}
