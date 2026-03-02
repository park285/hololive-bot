use std::path::PathBuf;

use secrecy::SecretString;
use serde::Deserialize;
use shared_core::error::SharedError;
use validator::Validate;

use crate::{db::DbConfig, logging::LoggingConfig, telemetry::TelemetryConfig};

#[derive(Debug, Clone, Deserialize, Validate)]
pub struct AppConfig {
    #[validate(nested)]
    pub db: DbConfig,
    #[validate(nested)]
    pub valkey: ValkeyConfig,
    #[validate(nested)]
    pub iris: IrisConfig,
    #[validate(nested)]
    pub telemetry: TelemetryConfig,
    #[validate(nested)]
    pub logging: LoggingConfig,
    #[validate(nested)]
    pub server: ServerConfig,
}

#[derive(Debug, Clone, Deserialize, Validate)]
pub struct ValkeyConfig {
    #[validate(length(min = 1))]
    pub url: String,
    #[validate(range(min = 1))]
    pub pool_size: u32,
}

impl Default for ValkeyConfig {
    fn default() -> Self {
        Self {
            url: "redis://valkey:6379".to_owned(),
            pool_size: 4,
        }
    }
}

#[derive(Debug, Clone, Deserialize, Validate)]
pub struct IrisConfig {
    #[validate(length(min = 1))]
    pub base_url: String,
    pub bot_token: SecretString,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
}

impl Default for IrisConfig {
    fn default() -> Self {
        Self {
            base_url: "http://iris:8080".to_owned(),
            bot_token: SecretString::from(""),
            timeout_secs: 10,
        }
    }
}

#[derive(Debug, Clone, Deserialize, Validate)]
pub struct ServerConfig {
    pub host: String,
    #[validate(range(min = 1, max = 65535))]
    pub port: u16,
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            host: "0.0.0.0".to_owned(),
            port: 3000,
        }
    }
}

#[allow(clippy::too_many_lines)]
pub fn load_config(app_name: &str) -> Result<AppConfig, SharedError> {
    macro_rules! set_default {
        ($builder:expr, $key:literal, $value:expr) => {
            $builder
                .set_default($key, $value)
                .map_err(|e| SharedError::Config(format!("build config defaults: {e}")))?
        };
    }

    let env_prefix = normalize_env_prefix(app_name);
    let app_file = PathBuf::from(format!("{app_name}.toml"));
    let db_defaults = DbConfig::default();
    let valkey_defaults = ValkeyConfig::default();
    let iris_defaults = IrisConfig::default();
    let telemetry_defaults = TelemetryConfig::default();
    let logging_defaults = LoggingConfig::default();
    let server_defaults = ServerConfig::default();

    let builder = config::Config::builder();
    let builder = set_default!(builder, "db.host", db_defaults.host.clone());
    let builder = set_default!(builder, "db.port", db_defaults.port);
    let builder = set_default!(builder, "db.user", db_defaults.user.clone());
    let builder = set_default!(builder, "db.password", "");
    let builder = set_default!(builder, "db.database", db_defaults.database.clone());
    let builder = set_default!(builder, "db.max_connections", db_defaults.max_connections);
    let builder = set_default!(builder, "db.min_connections", db_defaults.min_connections);
    let builder = set_default!(
        builder,
        "db.connect_timeout_secs",
        db_defaults.connect_timeout_secs
    );
    let builder = set_default!(
        builder,
        "db.idle_timeout_secs",
        db_defaults.idle_timeout_secs
    );
    let builder = set_default!(
        builder,
        "db.max_lifetime_secs",
        db_defaults.max_lifetime_secs
    );
    let builder = set_default!(builder, "valkey.url", valkey_defaults.url.clone());
    let builder = set_default!(builder, "valkey.pool_size", valkey_defaults.pool_size);
    let builder = set_default!(builder, "iris.base_url", iris_defaults.base_url.clone());
    let builder = set_default!(builder, "iris.bot_token", "");
    let builder = set_default!(builder, "iris.timeout_secs", iris_defaults.timeout_secs);
    let builder = set_default!(builder, "telemetry.enabled", telemetry_defaults.enabled);
    let builder = set_default!(
        builder,
        "telemetry.endpoint",
        telemetry_defaults.endpoint.clone()
    );
    let builder = set_default!(
        builder,
        "telemetry.service_name",
        telemetry_defaults.service_name.clone()
    );
    let builder = set_default!(
        builder,
        "telemetry.sample_ratio",
        telemetry_defaults.sample_ratio
    );
    let builder = set_default!(builder, "logging.level", logging_defaults.level.clone());
    let builder = set_default!(builder, "logging.format", logging_defaults.format.as_str());
    let builder = set_default!(builder, "server.host", server_defaults.host.clone());
    let builder = set_default!(builder, "server.port", server_defaults.port);

    let settings = builder
        .add_source(config::File::from(PathBuf::from("config.toml")).required(false))
        .add_source(config::File::from(app_file).required(false))
        .add_source(
            config::Environment::with_prefix(&env_prefix)
                .separator("__")
                .try_parsing(true),
        )
        .build()
        .map_err(|e| SharedError::Config(format!("build config: {e}")))?;

    let loaded: AppConfig = settings
        .try_deserialize()
        .map_err(|e| SharedError::Config(format!("deserialize config: {e}")))?;

    loaded
        .validate()
        .map_err(|e| SharedError::Config(format!("invalid config: {e}")))?;

    Ok(loaded)
}

fn normalize_env_prefix(app_name: &str) -> String {
    let mut normalized = String::with_capacity(app_name.len());
    for ch in app_name.chars() {
        if ch.is_ascii_alphanumeric() {
            normalized.push(ch.to_ascii_uppercase());
        } else {
            normalized.push('_');
        }
    }
    normalized
}
