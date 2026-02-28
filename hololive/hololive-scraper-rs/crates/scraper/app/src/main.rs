mod bootstrap;
mod cli;
mod observability;
mod shutdown;
mod state;

use anyhow::{Context, Result};
use axum::{Router, routing::get};
use chrono::Utc;
use clap::Parser;
use scraper_infra::config::AppConfig;
use std::{net::SocketAddr, sync::Arc};
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;
use tower_http::trace::TraceLayer;
use tracing::info;

use crate::{
    bootstrap::{
        initialize_runtime, join_db_monitor, join_feed_schedulers, join_maintenance_scheduler,
        run_once,
    },
    cli::Cli,
    observability::{init_tracing, resolve_telemetry_config},
    shutdown::wait_for_shutdown,
    state::{AppState, health, ready},
};

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    let config = AppConfig::load_from_path(&cli.config)
        .with_context(|| format!("failed to load config from path: {}", cli.config.display()))?;

    let telemetry = resolve_telemetry_config(&config.telemetry, &config.logging);
    let _tracing_runtime = init_tracing(&config.logging, &telemetry)?;

    if cli.run_once {
        run_once(&config).await?;
        return Ok(());
    }

    let shutdown_token = CancellationToken::new();
    let runtime = initialize_runtime(&config, &shutdown_token).await;

    let shared_state = Arc::new(AppState {
        version: env!("CARGO_PKG_VERSION"),
        db_connected: Arc::clone(&runtime.db_connected),
        feed_schedulers_active: runtime.feed_schedulers_active,
        started_at: Utc::now(),
    });

    let app = Router::new()
        .route("/health", get(health))
        .route("/ready", get(ready))
        .with_state(shared_state)
        .layer(TraceLayer::new_for_http());

    let addr = SocketAddr::from(([0, 0, 0, 0], config.health.port));
    let listener = TcpListener::bind(addr)
        .await
        .with_context(|| format!("failed to bind health server on {}", addr))?;

    info!(address = %addr, "scraper-app started");

    axum::serve(listener, app)
        .with_graceful_shutdown(wait_for_shutdown())
        .await
        .context("health server terminated with error")?;

    shutdown_token.cancel();
    join_feed_schedulers(runtime.feed_handles).await;
    join_maintenance_scheduler(runtime.maintenance_handle).await;
    join_db_monitor(runtime.db_monitor_handle).await;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{
        cli::Cli, observability::normalize_otlp_endpoint, observability::resolve_telemetry_config,
    };
    use clap::Parser;
    use scraper_infra::config::{LoggingConfig, TelemetryConfig};
    use std::{
        path::PathBuf,
        sync::{LazyLock, Mutex},
    };

    static ENV_LOCK: LazyLock<Mutex<()>> = LazyLock::new(|| Mutex::new(()));

    #[test]
    fn cli_defaults_to_normal_mode() {
        let cli = Cli::try_parse_from(["scraper-app"]).expect("cli should parse");
        assert_eq!(cli.config, PathBuf::from("config.toml"));
        assert!(!cli.run_once);
    }

    #[test]
    fn cli_accepts_run_once_flag() {
        let cli = Cli::try_parse_from(["scraper-app", "--run-once"]).expect("cli should parse");
        assert!(cli.run_once);
    }

    #[test]
    fn normalize_otlp_endpoint_applies_scheme_by_insecure_flag() {
        assert_eq!(
            normalize_otlp_endpoint("otel-collector:4317", true),
            "http://otel-collector:4317"
        );
        assert_eq!(
            normalize_otlp_endpoint("otel-collector:4317", false),
            "https://otel-collector:4317"
        );
        assert_eq!(
            normalize_otlp_endpoint("https://collector:4317", true),
            "https://collector:4317"
        );
    }

    #[test]
    fn resolve_telemetry_config_prefers_otel_env() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        // SAFETY: tests are serialized via ENV_LOCK.
        unsafe {
            std::env::set_var("OTEL_ENABLED", "true");
            std::env::set_var("OTEL_SERVICE_NAME", "scraper-env-service");
            std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317");
            std::env::set_var("OTEL_SAMPLE_RATE", "0.25");
        }

        let base = TelemetryConfig {
            enabled: false,
            service_name: "base-service".to_string(),
            service_version: "0.0.1".to_string(),
            environment: "production".to_string(),
            otlp_endpoint: "otel-collector:4317".to_string(),
            otlp_insecure: true,
            sample_rate: 1.0,
        };
        let logging = LoggingConfig {
            level: "info".to_string(),
            file_enabled: false,
            dir: "logs".to_string(),
            file: "hololive-scraper.log".to_string(),
            combined_file: "combined.log".to_string(),
            service: "hololive-scraper-rs".to_string(),
            environment: "production".to_string(),
        };

        let resolved = resolve_telemetry_config(&base, &logging);
        assert!(resolved.enabled);
        assert_eq!(resolved.service_name, "scraper-env-service");
        assert_eq!(resolved.otlp_endpoint, "otel-collector:4317");
        assert!((resolved.sample_rate - 0.25).abs() < f64::EPSILON);

        // SAFETY: tests are serialized via ENV_LOCK.
        unsafe {
            std::env::remove_var("OTEL_ENABLED");
            std::env::remove_var("OTEL_SERVICE_NAME");
            std::env::remove_var("OTEL_EXPORTER_OTLP_ENDPOINT");
            std::env::remove_var("OTEL_SAMPLE_RATE");
        }
    }
}
