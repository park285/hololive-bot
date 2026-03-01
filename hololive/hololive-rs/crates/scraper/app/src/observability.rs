// scraper-app observability: 앱 전용 설정 해석 + 공통 tracing/otel 초기화 위임

use anyhow::Result;
use scraper_infra::config::{LoggingConfig, TelemetryConfig};
use shared_infra::observability::{
    OtelInitConfig, TracingInitConfig, TracingRuntime, init_unified_tracing, parse_bool_env,
    parse_f64_env,
};

pub fn init_tracing(config: &LoggingConfig, telemetry: &TelemetryConfig) -> Result<TracingRuntime> {
    let tracing_config = TracingInitConfig {
        level: &config.level,
        file_enabled: config.file_enabled,
        dir: &config.dir,
        file: &config.file,
        combined_file: &config.combined_file,
    };

    let endpoint = shared_infra::observability::normalize_otlp_endpoint(
        &telemetry.otlp_endpoint,
        telemetry.otlp_insecure,
    );
    let otel_config = OtelInitConfig {
        enabled: telemetry.enabled,
        service_name: &telemetry.service_name,
        service_version: &telemetry.service_version,
        environment: &telemetry.environment,
        otlp_endpoint: &endpoint,
        otlp_insecure: telemetry.otlp_insecure,
        sample_rate: telemetry.sample_rate,
        startup_span_name: "scraper.startup",
    };

    init_unified_tracing(&tracing_config, &otel_config)
}

pub fn resolve_telemetry_config(
    base: &TelemetryConfig,
    logging: &LoggingConfig,
) -> TelemetryConfig {
    let mut cfg = base.clone();

    if cfg.service_name.trim().is_empty() {
        cfg.service_name = logging.service.clone();
    }
    if cfg.environment.trim().is_empty() {
        cfg.environment = logging.environment.clone();
    }

    if let Some(enabled) = parse_bool_env("OTEL_ENABLED") {
        cfg.enabled = enabled;
    }
    if let Ok(value) = std::env::var("OTEL_SERVICE_NAME")
        && !value.trim().is_empty()
    {
        cfg.service_name = value;
    }
    if let Ok(value) = std::env::var("OTEL_SERVICE_VERSION")
        && !value.trim().is_empty()
    {
        cfg.service_version = value;
    }
    if let Ok(value) = std::env::var("OTEL_ENVIRONMENT")
        && !value.trim().is_empty()
    {
        cfg.environment = value;
    }
    if let Ok(value) = std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT")
        && !value.trim().is_empty()
    {
        cfg.otlp_endpoint = value;
    }
    if let Some(insecure) = parse_bool_env("OTEL_EXPORTER_OTLP_INSECURE") {
        cfg.otlp_insecure = insecure;
    }
    if let Some(sample_rate) = parse_f64_env("OTEL_SAMPLE_RATE") {
        cfg.sample_rate = sample_rate;
    }

    if cfg.service_name.trim().is_empty() {
        cfg.service_name = "hololive-rs".to_string();
    }
    if cfg.service_version.trim().is_empty() {
        cfg.service_version = env!("CARGO_PKG_VERSION").to_string();
    }
    if cfg.environment.trim().is_empty() {
        cfg.environment = "production".to_string();
    }
    if cfg.otlp_endpoint.trim().is_empty() {
        cfg.otlp_endpoint = "otel-collector:4317".to_string();
    }
    cfg.sample_rate = cfg.sample_rate.clamp(0.0, 1.0);

    cfg
}
