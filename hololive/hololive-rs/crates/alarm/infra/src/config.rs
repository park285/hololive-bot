use std::{collections::HashSet, net::IpAddr, path::Path};

use secrecy::{ExposeSecret, SecretString};
use serde::{Deserialize, Deserializer};
use thiserror::Error;
use validator::Validate;

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

/// OpenTelemetry 트레이싱 설정
#[derive(Debug, Clone, Deserialize)]
pub struct TelemetryConfig {
    pub enabled: bool,
    pub service_name: String,
    pub service_version: String,
    pub environment: String,
    pub otlp_endpoint: String,
    pub otlp_insecure: bool,
    pub sample_rate: f64,
}

impl Default for TelemetryConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            service_name: "hololive-alarm".into(),
            service_version: env!("CARGO_PKG_VERSION").into(),
            environment: "production".into(),
            otlp_endpoint: "otel-collector:4317".into(),
            otlp_insecure: false,
            sample_rate: 1.0,
        }
    }
}

/// Valkey(Redis) 연결 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct ValkeyConfig {
    /// TCP: "redis://host:port", Unix: "redis+unix:///path/to/sock"
    pub url: String,
    #[validate(range(min = 1))]
    pub pool_size: u32,
}

/// Holodex API 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct HolodexConfig {
    pub base_url: String,
    /// API 키 로테이션용 복수 키 목록
    #[serde(deserialize_with = "deserialize_api_keys")]
    pub api_keys: Vec<String>,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    /// 요청 간 최소 간격(밀리초)
    #[validate(range(min = 1))]
    pub rate_limit_ms: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

fn deserialize_api_keys<'de, D>(deserializer: D) -> Result<Vec<String>, D::Error>
where
    D: Deserializer<'de>,
{
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum ApiKeysInput {
        List(Vec<String>),
        Text(String),
    }

    match ApiKeysInput::deserialize(deserializer)? {
        ApiKeysInput::List(keys) => Ok(keys),
        ApiKeysInput::Text(raw) => parse_api_keys_text(&raw).map_err(serde::de::Error::custom),
    }
}

fn parse_api_keys_text(raw: &str) -> Result<Vec<String>, String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }

    if trimmed.starts_with('[') {
        return serde_json::from_str::<Vec<String>>(trimmed)
            .map_err(|e| format!("invalid holodex.api_keys JSON array: {e}"));
    }

    Ok(trimmed
        .split(',')
        .map(|token| token.trim().trim_matches('"').trim_matches('\''))
        .filter(|token| !token.is_empty())
        .map(std::string::ToString::to_string)
        .collect())
}

/// Chzzk API 설정 (서킷 브레이커 포함)
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct ChzzkConfig {
    pub base_url: String,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

/// Twitch API 설정 (OAuth2 + 서킷 브레이커)
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct TwitchConfig {
    pub base_url: String,
    pub auth_url: String,
    pub client_id: String,
    pub client_secret: SecretString,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

/// Iris 메시지 발송 클라이언트 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct IrisConfig {
    pub base_url: String,
    pub bot_token: SecretString,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

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

/// HTTP 헬스체크 포트 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct HealthConfig {
    #[validate(range(min = 1, max = 65535))]
    pub port: u16,
}

/// 로깅 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct LoggingConfig {
    pub level: String,
    pub file_enabled: bool,
    pub dir: String,
    pub file: String,
    pub combined_file: String,
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
            .set_default("logging.file_enabled", false)?
            .set_default("logging.dir", "logs")?
            .set_default("logging.file", "hololive-alarm.log")?
            .set_default("logging.combined_file", "combined.log")?
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
        loaded.holodex.api_keys = resolve_holodex_api_keys(loaded.holodex.api_keys);
        loaded
            .validate()
            .map_err(|e| ConfigLoadError::Validation(format!("invalid alarm config: {e}")))?;
        validate_iris_base_url_policy(&loaded.iris.base_url)
            .map_err(ConfigLoadError::Validation)?;
        Ok(loaded)
    }
}

fn resolve_holodex_api_keys(configured_keys: Vec<String>) -> Vec<String> {
    normalize_api_keys(configured_keys)
}

fn normalize_api_keys(keys: Vec<String>) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut normalized = Vec::new();
    for key in keys {
        let trimmed = key.trim();
        if trimmed.is_empty() {
            continue;
        }
        if seen.insert(trimmed.to_string()) {
            normalized.push(trimmed.to_string());
        }
    }
    normalized
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

/// Iris base_url 보안 정책:
/// - https는 항상 허용
/// - http는 내부망/로컬 주소만 허용 (공개망 HTTP 차단)
pub fn validate_iris_base_url_policy(base_url: &str) -> Result<(), String> {
    let parsed = url::Url::parse(base_url).map_err(|e| format!("iris.base_url 파싱 실패: {e}"))?;

    match parsed.scheme() {
        "https" => Ok(()),
        "http" => validate_http_host_is_private_or_internal(&parsed),
        scheme => Err(format!(
            "iris.base_url scheme '{scheme}' is not allowed (http/https only)"
        )),
    }
}

fn validate_http_host_is_private_or_internal(parsed: &url::Url) -> Result<(), String> {
    let Some(host) = parsed.host_str() else {
        return Err("iris.base_url host is missing".to_string());
    };

    // IP 호스트: 사설/루프백/링크로컬만 허용
    if let Ok(ip) = host.parse::<IpAddr>() {
        if is_private_or_local_ip(ip) {
            return Ok(());
        }
        return Err(format!(
            "public HTTP endpoint is blocked for iris.base_url: {host}"
        ));
    }

    let normalized = host.trim_end_matches('.').to_ascii_lowercase();
    if normalized == "localhost" || normalized.ends_with(".localhost") {
        return Ok(());
    }

    // dot 없는 단일 라벨(hostname)은 내부 서비스 DNS로 간주
    if !normalized.contains('.') {
        return Ok(());
    }

    // 내부 DNS suffix 허용
    if normalized.ends_with(".local")
        || normalized.ends_with(".internal")
        || normalized.ends_with(".svc")
        || normalized.ends_with(".cluster.local")
    {
        return Ok(());
    }

    Err(format!(
        "public HTTP endpoint is blocked for iris.base_url: {host}"
    ))
}

fn is_private_or_local_ip(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => {
            v4.is_private() || v4.is_loopback() || v4.is_link_local() || v4.is_unspecified()
        }
        IpAddr::V6(v6) => {
            v6.is_loopback()
                || v6.is_unique_local()
                || v6.is_unicast_link_local()
                || v6.is_unspecified()
        }
    }
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        sync::{LazyLock, Mutex},
    };

    use secrecy::{ExposeSecret, SecretString};

    use super::{AlarmAppConfig, ConfigLoadError, validate_iris_base_url_policy};

    static ENV_LOCK: LazyLock<Mutex<()>> = LazyLock::new(|| Mutex::new(()));

    struct EnvVarGuard {
        keys: Vec<String>,
    }

    impl EnvVarGuard {
        fn new(keys: Vec<&str>) -> Self {
            Self {
                keys: keys
                    .into_iter()
                    .map(std::string::ToString::to_string)
                    .collect(),
            }
        }
    }

    impl Drop for EnvVarGuard {
        fn drop(&mut self) {
            for key in &self.keys {
                // SAFETY: ENV_LOCK으로 직렬화하여 안전하게 환경변수 제거
                unsafe { std::env::remove_var(key) };
            }
        }
    }

    #[test]
    fn load_config_applies_alarm_env_overrides() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let _env_guard = EnvVarGuard::new(vec![
            "ALARM__DATABASE__HOST",
            "ALARM__HEALTH__PORT",
            "ALARM__LOGGING__LEVEL",
            "ALARM__LOGGING__FILE_ENABLED",
            "ALARM__LOGGING__COMBINED_FILE",
            "ALARM__VALKEY__POOL_SIZE",
            "ALARM__ALARM__YOUTUBE_CHECK_TIMEOUT_SECS",
            "ALARM__IRIS__CIRCUIT_FAILURE_THRESHOLD",
            "ALARM__ALARM__TWITCH_ENABLED",
            "ALARM__DATABASE__PASSWORD",
            "ALARM__HOLODEX__API_KEYS",
        ]);

        let temp_config =
            std::env::temp_dir().join(format!("alarm-config-{}.toml", std::process::id()));
        fs::write(&temp_config, "").expect("임시 설정 파일 작성 가능해야 함");

        // SAFETY: ENV_LOCK으로 직렬화하여 안전하게 환경변수 설정
        unsafe {
            std::env::set_var("ALARM__DATABASE__HOST", "override-db-host");
            std::env::set_var("ALARM__HEALTH__PORT", "30099");
            std::env::set_var("ALARM__LOGGING__LEVEL", "debug");
            std::env::set_var("ALARM__LOGGING__FILE_ENABLED", "false");
            std::env::set_var("ALARM__LOGGING__COMBINED_FILE", "custom-combined.log");
            std::env::set_var("ALARM__VALKEY__POOL_SIZE", "8");
            std::env::set_var("ALARM__ALARM__YOUTUBE_CHECK_TIMEOUT_SECS", "50");
            std::env::set_var("ALARM__IRIS__CIRCUIT_FAILURE_THRESHOLD", "7");
            std::env::set_var("ALARM__ALARM__TWITCH_ENABLED", "false");
            std::env::set_var("ALARM__DATABASE__PASSWORD", "super-secret-password");
            std::env::set_var("ALARM__HOLODEX__API_KEYS", "[\"key-a\",\"key-b\"]");
        }

        let loaded = AlarmAppConfig::load_from_path(&temp_config).expect("설정 로드 성공해야 함");

        assert_eq!(loaded.database.host, "override-db-host");
        assert_eq!(loaded.health.port, 30099);
        assert_eq!(loaded.logging.level, "debug");
        assert!(!loaded.logging.file_enabled);
        assert_eq!(loaded.logging.combined_file, "custom-combined.log");
        assert!(!loaded.alarm.twitch_enabled);
        assert_eq!(loaded.valkey.pool_size, 8);
        assert_eq!(loaded.alarm.youtube_check_timeout_secs, 50);
        assert_eq!(loaded.iris.circuit_failure_threshold, 7);
        assert_eq!(loaded.holodex.api_keys, vec!["key-a", "key-b"]);
        assert_eq!(
            loaded.database.password.expose_secret(),
            "super-secret-password"
        );

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn load_config_defaults_applied() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");

        let temp_config =
            std::env::temp_dir().join(format!("alarm-config-defaults-{}.toml", std::process::id()));
        fs::write(&temp_config, "").expect("임시 설정 파일 작성 가능해야 함");

        let loaded =
            AlarmAppConfig::load_from_path(&temp_config).expect("기본값으로 로드 성공해야 함");

        assert_eq!(loaded.health.port, 30011);
        assert_eq!(loaded.valkey.pool_size, 4);
        assert!(!loaded.logging.file_enabled);
        assert_eq!(loaded.holodex.timeout_secs, 30);
        assert_eq!(loaded.holodex.circuit_failure_threshold, 5);
        assert_eq!(loaded.iris.circuit_reset_secs, 30);
        assert!(loaded.alarm.twitch_enabled);
        assert_eq!(loaded.alarm.target_minutes, vec![5, 3, 1]);
        assert_eq!(loaded.alarm.youtube_check_timeout_secs, 45);
        assert_eq!(loaded.alarm.chzzk_check_timeout_secs, 30);
        assert_eq!(loaded.alarm.twitch_check_timeout_secs, 30);

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn load_config_holodex_legacy_env_is_ignored() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let _env_guard = EnvVarGuard::new(vec!["HOLODEX_API_KEY_1", "HOLODEX_API_KEY_2"]);

        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-holodex-legacy-ignored-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[holodex]
api_keys = []
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        // SAFETY: ENV_LOCK으로 직렬화하여 안전하게 환경변수 설정
        unsafe {
            std::env::set_var("HOLODEX_API_KEY_1", "key-a");
            std::env::set_var("HOLODEX_API_KEY_2", "key-b");
        }

        let loaded = AlarmAppConfig::load_from_path(&temp_config).expect("설정 로드 성공해야 함");
        assert!(loaded.holodex.api_keys.is_empty());

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn database_url_encodes_special_chars() {
        use super::DatabaseConfig;

        let cfg = DatabaseConfig {
            host: "db-host".into(),
            port: 5432,
            name: "my db".into(),
            user: "user@name".into(),
            password: SecretString::from("p@ss/word".to_string()),
            sslmode: "disable".into(),
            max_connections: 5,
        };
        let url = cfg.database_url();
        // 특수문자가 URL 인코딩되었는지 확인
        assert!(url.starts_with("postgres://"));
        assert!(url.contains("p%40ss"));
        assert!(url.contains("my+db"));
    }

    #[test]
    fn iris_base_url_policy_allows_internal_http_and_public_https() {
        assert!(validate_iris_base_url_policy("http://iris:8080").is_ok());
        assert!(validate_iris_base_url_policy("http://10.0.0.10:8080").is_ok());
        assert!(validate_iris_base_url_policy("https://api.example.com").is_ok());
    }

    #[test]
    fn iris_base_url_policy_blocks_public_http() {
        let err =
            validate_iris_base_url_policy("http://api.example.com").expect_err("must be blocked");
        assert!(err.contains("public HTTP endpoint is blocked"));
    }

    #[test]
    fn load_config_rejects_public_http_iris_base_url() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-iris-public-http-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[iris]
base_url = "http://api.example.com"
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        let loaded = AlarmAppConfig::load_from_path(&temp_config);
        assert!(
            matches!(loaded, Err(ConfigLoadError::Validation(msg)) if msg.contains("public HTTP endpoint is blocked")),
            "공개망 HTTP는 설정 단계에서 차단되어야 함"
        );

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn load_config_rejects_zero_poll_with_validator() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-validator-zero-poll-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[alarm]
chzzk_poll_secs = 0
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        let loaded = AlarmAppConfig::load_from_path(&temp_config);
        assert!(
            matches!(loaded, Err(ConfigLoadError::Validation(msg)) if msg.contains("chzzk_poll_secs")),
            "poll 값 0은 validator에서 차단되어야 함"
        );

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn secret_fields_are_redacted_in_debug_output() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-secret-redaction-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[iris]
bot_token = "iris-secret-token"

[twitch]
client_secret = "twitch-secret-token"

[database]
password = "db-secret-password"
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        let loaded = AlarmAppConfig::load_from_path(&temp_config).expect("설정 로드 성공해야 함");
        assert_eq!(loaded.iris.bot_token.expose_secret(), "iris-secret-token");
        assert_eq!(
            loaded.twitch.client_secret.expose_secret(),
            "twitch-secret-token"
        );
        assert_eq!(
            loaded.database.password.expose_secret(),
            "db-secret-password"
        );

        let debug_iris = format!("{:?}", loaded.iris.bot_token);
        let debug_twitch = format!("{:?}", loaded.twitch.client_secret);
        let debug_db = format!("{:?}", loaded.database.password);
        assert!(!debug_iris.contains("iris-secret-token"));
        assert!(!debug_twitch.contains("twitch-secret-token"));
        assert!(!debug_db.contains("db-secret-password"));

        let _ = fs::remove_file(&temp_config);
    }
}
