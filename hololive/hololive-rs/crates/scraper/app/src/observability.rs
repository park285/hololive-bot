// scraper-app observability: 앱 전용 설정 해석 + 공통 tracing/otel 초기화 위임

use anyhow::Result;
use scraper_infra::config::{LoggingConfig, TelemetryConfig};
use shared_infra::observability::{
    OtelInitConfig, TracingInitConfig, TracingRuntime, init_unified_tracing,
    normalize_otlp_endpoint, resolve_otel_env_overrides,
};

pub fn init_tracing(config: &LoggingConfig, telemetry: &TelemetryConfig) -> Result<TracingRuntime> {
    let tracing_config = TracingInitConfig {
        level: &config.level,
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
        startup_span_name: "scraper.startup",
    };

    init_unified_tracing(&tracing_config, &otel_config)
}

/// 환경변수로 telemetry 설정 오버라이드.
/// logging.service / logging.environment를 service_name/environment 초기값으로 사용한 뒤
/// OTEL_ 환경변수를 적용합니다.
pub fn resolve_telemetry_config(
    base: &TelemetryConfig,
    logging: &LoggingConfig,
) -> TelemetryConfig {
    // logging 필드를 telemetry 초기값으로 승계 (비어있는 경우만)
    let mut seeded = base.clone();
    if seeded.service_name.trim().is_empty() {
        seeded.service_name = logging.service.clone();
    }
    if seeded.environment.trim().is_empty() {
        seeded.environment = logging.environment.clone();
    }

    // 공통 환경변수 오버라이드 적용: 최종 폴백은 logging에서 승계한 값
    resolve_otel_env_overrides(&seeded, &logging.service, Some(&logging.environment))
}
