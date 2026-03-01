use std::path::Path;

use anyhow::{Context, Result};
use serde::Deserialize;
use shared_infra::{
    config::{IrisConfig, ValkeyConfig},
    db::DbConfig,
    logging::LoggingConfig,
    telemetry::TelemetryConfig,
};

#[derive(Debug, Clone, Deserialize)]
pub(crate) struct DispatcherAppConfig {
    #[serde(default)]
    pub valkey: ValkeyConfig,
    #[serde(default)]
    pub db: DbConfig,
    #[serde(default)]
    pub iris: IrisConfig,
    #[serde(default)]
    pub logging: LoggingConfig,
    #[serde(default)]
    pub telemetry: TelemetryConfig,
    #[serde(default)]
    pub health: HealthConfig,
    #[serde(default)]
    pub dispatcher: DispatcherConfig,
}

#[derive(Debug, Clone, Deserialize)]
pub(crate) struct HealthConfig {
    pub host: String,
    pub port: u16,
}

impl Default for HealthConfig {
    fn default() -> Self {
        Self {
            host: "0.0.0.0".to_string(),
            port: 30020,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub(crate) struct DispatcherConfig {
    pub queue_key: String,
    pub max_batch: usize,
    pub reconnect_backoff_ms: u64,
}

impl Default for DispatcherConfig {
    fn default() -> Self {
        Self {
            queue_key: "alarm:dispatch:queue".to_string(),
            max_batch: 50,
            reconnect_backoff_ms: 1_000,
        }
    }
}

pub(crate) fn load_dispatcher_config(path: &Path) -> Result<DispatcherAppConfig> {
    let settings = config::Config::builder()
        .add_source(config::File::from(path.to_path_buf()).required(false))
        .add_source(
            config::Environment::with_prefix("DISPATCHER")
                .separator("__")
                .try_parsing(true),
        )
        .build()
        .context("build dispatcher config")?;

    settings
        .try_deserialize::<DispatcherAppConfig>()
        .context("deserialize dispatcher config")
}
