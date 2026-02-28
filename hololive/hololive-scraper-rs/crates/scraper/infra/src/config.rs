use secrecy::{ExposeSecret, SecretString};
use serde::Deserialize;
use std::path::Path;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ConfigLoadError {
    #[error("failed to build config source: {0}")]
    Build(#[from] config::ConfigError),
}

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    pub database: DatabaseConfig,
    pub proxy: ProxyConfig,
    pub scheduler: SchedulerConfig,
    pub scraper: ScraperConfig,
    pub logging: LoggingConfig,
    pub telemetry: TelemetryConfig,
    pub health: HealthConfig,
    #[serde(default)]
    pub feeds: Option<Vec<FeedConfig>>,
    #[serde(default)]
    pub maintenance: Option<MaintenanceConfigToml>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DatabaseConfig {
    pub host: String,
    pub port: u16,
    pub name: String,
    pub user: String,
    pub password: SecretString,
    pub sslmode: String,
    pub max_connections: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ProxyConfig {
    pub socks5_url: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SchedulerConfig {
    pub scrape_hour_kst: u8,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ScraperConfig {
    pub user_agent: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct LoggingConfig {
    pub level: String,
    pub file_enabled: bool,
    pub dir: String,
    pub file: String,
    pub combined_file: String,
    pub service: String,
    pub environment: String,
}

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

#[derive(Debug, Clone, Deserialize)]
pub struct HealthConfig {
    pub port: u16,
}

#[derive(Debug, Clone, Deserialize)]
pub struct FeedConfig {
    pub name: String,
    pub event_type: String,
    pub urls: Vec<String>,
    pub scrape_hour_kst: Option<u8>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct MaintenanceConfigToml {
    pub expired_hour_kst: Option<u8>,
    pub link_check_interval_hours: Option<u64>,
}

impl AppConfig {
    pub fn load() -> Result<Self, ConfigLoadError> {
        Self::load_from_path("config.toml")
    }

    pub fn load_from_path(path: impl AsRef<Path>) -> Result<Self, ConfigLoadError> {
        let settings = config::Config::builder()
            .set_default("database.host", "holo-postgres")?
            .set_default("database.port", 5432)?
            .set_default("database.name", "hololive")?
            .set_default("database.user", "hololive_scraper")?
            .set_default("database.password", "")?
            .set_default("database.sslmode", "require")?
            .set_default("database.max_connections", 5)?
            .set_default("proxy.socks5_url", "socks5://vpn-scraper-proxy:1080")?
            .set_default("scheduler.scrape_hour_kst", 6)?
            .set_default(
                "scraper.user_agent",
                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
            )?
            .set_default("logging.level", "info")?
            .set_default("logging.file_enabled", false)?
            .set_default("logging.dir", "logs")?
            .set_default("logging.file", "hololive-scraper.log")?
            .set_default("logging.combined_file", "combined.log")?
            .set_default("logging.service", "hololive-scraper-rs")?
            .set_default("logging.environment", "production")?
            .set_default("telemetry.enabled", false)?
            .set_default("telemetry.service_name", "hololive-scraper-rs")?
            .set_default("telemetry.service_version", env!("CARGO_PKG_VERSION"))?
            .set_default("telemetry.environment", "production")?
            .set_default("telemetry.otlp_endpoint", "otel-collector:4317")?
            .set_default("telemetry.otlp_insecure", true)?
            .set_default("telemetry.sample_rate", 1.0)?
            .set_default("health.port", 30010)?
            .add_source(config::File::from(path.as_ref().to_path_buf()).required(false))
            .add_source(
                config::Environment::with_prefix("SCRAPER")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?;

        settings.try_deserialize().map_err(ConfigLoadError::Build)
    }

    /// [[feeds]] 미설정 시 기존 기본 3개 피드 생성 (scheduler.scrape_hour_kst fallback)
    pub fn resolved_feeds(&self) -> Vec<FeedConfig> {
        if let Some(feeds) = &self.feeds
            && !feeds.is_empty()
        {
            return feeds.clone();
        }

        // fallback: 기존 기본 피드 3개를 scheduler.scrape_hour_kst 기반으로 생성
        vec![
            FeedConfig {
                name: "event".to_string(),
                event_type: "event".to_string(),
                urls: vec!["https://hololive.hololivepro.com/events/feed/".to_string()],
                scrape_hour_kst: Some(self.scheduler.scrape_hour_kst),
            },
            FeedConfig {
                name: "news".to_string(),
                event_type: "news".to_string(),
                urls: vec!["https://hololive.hololivepro.com/news/feed/".to_string()],
                scrape_hour_kst: Some(self.scheduler.scrape_hour_kst),
            },
            FeedConfig {
                name: "en-news".to_string(),
                event_type: "news".to_string(),
                urls: vec!["https://hololive.hololivepro.com/en/news/feed/".to_string()],
                scrape_hour_kst: Some(self.scheduler.scrape_hour_kst),
            },
        ]
    }
}

impl DatabaseConfig {
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

#[cfg(test)]
mod tests {
    use super::{AppConfig, DatabaseConfig};
    use secrecy::SecretString;
    use std::{
        fs,
        sync::{LazyLock, Mutex},
    };

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
                // SAFETY: tests are serialized via ENV_LOCK, so mutating process env here is safe.
                unsafe { std::env::remove_var(key) };
            }
        }
    }

    #[test]
    fn load_config_applies_scraper_env_overrides() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let env_guard = EnvVarGuard::new(vec![
            "SCRAPER__DATABASE__HOST",
            "SCRAPER__HEALTH__PORT",
            "SCRAPER__LOGGING__LEVEL",
            "SCRAPER__LOGGING__FILE_ENABLED",
            "SCRAPER__LOGGING__FILE",
            "SCRAPER__LOGGING__COMBINED_FILE",
            "SCRAPER__LOGGING__SERVICE",
            "SCRAPER__LOGGING__ENVIRONMENT",
            "SCRAPER__TELEMETRY__ENABLED",
            "SCRAPER__TELEMETRY__SERVICE_NAME",
            "SCRAPER__TELEMETRY__SERVICE_VERSION",
            "SCRAPER__TELEMETRY__ENVIRONMENT",
            "SCRAPER__TELEMETRY__OTLP_ENDPOINT",
            "SCRAPER__TELEMETRY__OTLP_INSECURE",
            "SCRAPER__TELEMETRY__SAMPLE_RATE",
        ]);

        let temp_config = std::env::temp_dir().join(format!(
            "hololive-scraper-config-{}.toml",
            std::process::id()
        ));
        fs::write(&temp_config, "").expect("temporary config should be writable");

        // SAFETY: tests are serialized via ENV_LOCK, so mutating process env here is safe.
        unsafe {
            std::env::set_var("SCRAPER__DATABASE__HOST", "override-db-host");
            std::env::set_var("SCRAPER__HEALTH__PORT", "30123");
            std::env::set_var("SCRAPER__LOGGING__LEVEL", "debug");
            std::env::set_var("SCRAPER__LOGGING__FILE_ENABLED", "false");
            std::env::set_var("SCRAPER__LOGGING__FILE", "custom-scraper.log");
            std::env::set_var("SCRAPER__LOGGING__COMBINED_FILE", "custom-combined.log");
            std::env::set_var("SCRAPER__LOGGING__SERVICE", "hololive-scraper-rs-test");
            std::env::set_var("SCRAPER__LOGGING__ENVIRONMENT", "staging");
            std::env::set_var("SCRAPER__TELEMETRY__ENABLED", "true");
            std::env::set_var(
                "SCRAPER__TELEMETRY__SERVICE_NAME",
                "hololive-scraper-rs-test",
            );
            std::env::set_var("SCRAPER__TELEMETRY__SERVICE_VERSION", "1.2.3-test");
            std::env::set_var("SCRAPER__TELEMETRY__ENVIRONMENT", "staging");
            std::env::set_var("SCRAPER__TELEMETRY__OTLP_ENDPOINT", "otel-collector:4317");
            std::env::set_var("SCRAPER__TELEMETRY__OTLP_INSECURE", "true");
            std::env::set_var("SCRAPER__TELEMETRY__SAMPLE_RATE", "0.3");
        }

        let loaded = AppConfig::load_from_path(&temp_config).expect("config should load");
        assert_eq!(loaded.database.host, "override-db-host");
        assert_eq!(loaded.health.port, 30123);
        assert_eq!(loaded.logging.level, "debug");
        assert!(!loaded.logging.file_enabled);
        assert_eq!(loaded.logging.file, "custom-scraper.log");
        assert_eq!(loaded.logging.combined_file, "custom-combined.log");
        assert_eq!(loaded.logging.service, "hololive-scraper-rs-test");
        assert_eq!(loaded.logging.environment, "staging");
        assert!(loaded.telemetry.enabled);
        assert_eq!(loaded.telemetry.service_name, "hololive-scraper-rs-test");
        assert_eq!(loaded.telemetry.service_version, "1.2.3-test");
        assert_eq!(loaded.telemetry.environment, "staging");
        assert_eq!(loaded.telemetry.otlp_endpoint, "otel-collector:4317");
        assert!(loaded.telemetry.otlp_insecure);
        assert!((loaded.telemetry.sample_rate - 0.3).abs() < f64::EPSILON);

        drop(env_guard);
        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn database_url_encodes_special_chars() {
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
        assert!(url.starts_with("postgres://"));
        assert!(url.contains("user%40name"));
        assert!(url.contains("p%40ss%2Fword"));
        assert!(url.contains("my+db"));
    }
}
