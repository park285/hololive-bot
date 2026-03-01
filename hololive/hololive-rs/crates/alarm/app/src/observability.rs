// alarm-app observability: 앱 전용 설정 해석 + 공통 tracing/otel 초기화 위임

use alarm_infra::config::{LoggingConfig, TelemetryConfig};
use anyhow::Result;
use metrics_exporter_prometheus::{PrometheusBuilder, PrometheusHandle};
use shared_infra::observability::{
    OtelInitConfig, TracingInitConfig, TracingRuntime, init_unified_tracing,
    normalize_otlp_endpoint, parse_bool_env, parse_f64_env,
};

/// stdout 기본 + file logging 선택적 출력 (KST 기준), OTEL 선택적 활성화
pub fn init_tracing(config: &LoggingConfig, telemetry: &TelemetryConfig) -> Result<TracingRuntime> {
    let tracing_config = TracingInitConfig {
        level: &config.level,
        file_enabled: config.file_enabled,
        dir: &config.dir,
        file: &config.file,
        combined_file: &config.combined_file,
    };

    let endpoint = normalize_otlp_endpoint(&telemetry.otlp_endpoint, telemetry.otlp_insecure);
    let otel_config = OtelInitConfig {
        enabled: telemetry.enabled,
        service_name: &telemetry.service_name,
        service_version: &telemetry.service_version,
        environment: &telemetry.environment,
        otlp_endpoint: &endpoint,
        otlp_insecure: telemetry.otlp_insecure,
        sample_rate: telemetry.sample_rate,
        startup_span_name: "alarm.startup",
    };

    init_unified_tracing(&tracing_config, &otel_config)
}

/// 환경변수로 telemetry 설정 오버라이드 (alarm 전용: logging 참조 없음)
pub fn resolve_telemetry_config(base: &TelemetryConfig) -> TelemetryConfig {
    let mut cfg = base.clone();

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

    // 빈 문자열 폴백: alarm 서비스 고정값
    if cfg.service_name.trim().is_empty() {
        cfg.service_name = "hololive-alarm".to_string();
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

/// Prometheus metrics recorder 초기화
pub fn init_metrics() -> Result<PrometheusHandle> {
    PrometheusBuilder::new()
        .install_recorder()
        .map_err(|e| anyhow::anyhow!("prometheus metrics recorder 초기화 실패: {e}"))
}
