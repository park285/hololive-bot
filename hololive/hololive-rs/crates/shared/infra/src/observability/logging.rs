// 공통 observability 인프라: OTEL 런타임, tracing 초기화, 텔레메트리 설정 해석

use anyhow::{Context, Result};
use opentelemetry::{
    KeyValue, global,
    trace::{Span as _, Tracer as _, TracerProvider as _},
};
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::{
    Resource,
    propagation::TraceContextPropagator,
    trace::{Sampler, SdkTracerProvider},
};
use tracing::{error, info};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use super::layers::structured_layer;

// ── config 인터페이스 (앱별 config 타입 의존 차단) ──

pub struct TracingInitConfig<'a> {
    pub level: &'a str,
}

pub struct OtelInitConfig<'a> {
    pub enabled: bool,
    pub service_name: &'a str,
    pub service_version: &'a str,
    pub environment: &'a str,
    pub otlp_endpoint: &'a str,
    pub otlp_insecure: bool,
    pub sample_rate: f64,
    pub startup_span_name: &'a str,
}

// ── OTEL 런타임 ──

#[derive(Debug)]
struct OTelRuntime {
    provider: SdkTracerProvider,
    service_name: String,
    endpoint: String,
    sample_rate: f64,
}

#[derive(Debug)]
pub struct TracingRuntime {
    otel: Option<OTelRuntime>,
}

impl Drop for TracingRuntime {
    fn drop(&mut self) {
        if let Some(otel) = self.otel.take()
            && let Err(err) = otel.provider.shutdown()
        {
            error!(error = %err, "otel provider shutdown 실패");
        }
    }
}

// ── 환경변수 파싱 헬퍼 ──

pub fn parse_bool_env(name: &str) -> Option<bool> {
    let raw = std::env::var(name).ok()?;
    match raw.trim().to_ascii_lowercase().as_str() {
        "1" | "true" | "yes" | "on" => Some(true),
        "0" | "false" | "no" | "off" => Some(false),
        _ => None,
    }
}

pub fn parse_f64_env(name: &str) -> Option<f64> {
    std::env::var(name)
        .ok()
        .and_then(|value| value.trim().parse::<f64>().ok())
}

// ── 공유 텔레메트리 설정 구조체 (alarm/scraper 공통) ──

/// alarm/scraper 양측이 공유하는 OTEL 텔레메트리 설정
#[derive(Debug, Clone, serde::Deserialize)]
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
            service_name: String::new(),
            service_version: env!("CARGO_PKG_VERSION").to_owned(),
            environment: "production".to_owned(),
            otlp_endpoint: "otel-collector:4317".to_owned(),
            otlp_insecure: false,
            sample_rate: 1.0,
        }
    }
}

/// OTEL 환경변수 오버라이드 및 빈 필드 기본값 적용.
///
/// - `default_service_name`: `service_name`이 비어있을 때 사용할 기본값
/// - `default_environment`: `environment`가 비어있을 때 사용할 기본값 (None이면 "production")
pub fn resolve_otel_env_overrides(
    base: &TelemetryConfig,
    default_service_name: &str,
    default_environment: Option<&str>,
) -> TelemetryConfig {
    let mut cfg = base.clone();

    // 환경변수 오버라이드 (비어있는 값은 무시)
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

    // 빈 문자열 폴백: 호출자가 제공한 기본값 적용
    if cfg.service_name.trim().is_empty() {
        default_service_name.clone_into(&mut cfg.service_name);
    }
    if cfg.service_version.trim().is_empty() {
        env!("CARGO_PKG_VERSION").clone_into(&mut cfg.service_version);
    }
    if cfg.environment.trim().is_empty() {
        default_environment
            .unwrap_or("production")
            .clone_into(&mut cfg.environment);
    }
    if cfg.otlp_endpoint.trim().is_empty() {
        "otel-collector:4317".clone_into(&mut cfg.otlp_endpoint);
    }
    cfg.sample_rate = cfg.sample_rate.clamp(0.0, 1.0);

    cfg
}

// ── OTLP 엔드포인트 정규화 ──

pub fn normalize_otlp_endpoint(endpoint: &str, insecure: bool) -> String {
    let trimmed = endpoint.trim();
    if trimmed.starts_with("http://") || trimmed.starts_with("https://") {
        return trimmed.to_owned();
    }

    let scheme = if insecure { "http" } else { "https" };
    format!("{scheme}://{trimmed}")
}

// ── OTEL 런타임 초기화 ──

fn init_otel_runtime(config: &OtelInitConfig<'_>) -> Result<Option<OTelRuntime>> {
    if !config.enabled {
        return Ok(None);
    }

    let endpoint = normalize_otlp_endpoint(config.otlp_endpoint, config.otlp_insecure);
    let exporter = opentelemetry_otlp::SpanExporter::builder()
        .with_tonic()
        .with_endpoint(endpoint.clone())
        .build()
        .context("OTLP span exporter 빌드 실패")?;

    let sample_rate = config.sample_rate.clamp(0.0, 1.0);
    let resource = Resource::builder_empty()
        .with_attributes(vec![
            KeyValue::new("service.name", config.service_name.to_owned()),
            KeyValue::new("service.version", config.service_version.to_owned()),
            KeyValue::new("deployment.environment", config.environment.to_owned()),
        ])
        .build();

    let provider = SdkTracerProvider::builder()
        .with_resource(resource)
        .with_sampler(Sampler::TraceIdRatioBased(sample_rate))
        .with_batch_exporter(exporter)
        .build();

    global::set_text_map_propagator(TraceContextPropagator::new());
    global::set_tracer_provider(provider.clone());

    // startup span flush (exporter 연결 확인)
    let tracer = provider.tracer(config.service_name.to_owned());
    let span_name = config.startup_span_name.to_owned();
    let mut startup_span = tracer.start(span_name);
    startup_span.end();
    if let Err(err) = provider.force_flush() {
        error!(error = %err, "otel span force_flush 실패");
    }

    Ok(Some(OTelRuntime {
        provider,
        service_name: config.service_name.to_owned(),
        endpoint,
        sample_rate,
    }))
}

// ── 통합 tracing 초기화 (stdout 전용, file logging 미지원) ──

pub fn init_unified_tracing(
    tracing_config: &TracingInitConfig<'_>,
    otel_config: &OtelInitConfig<'_>,
) -> Result<TracingRuntime> {
    let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new(tracing_config.level));

    let stdout_layer = structured_layer()?;

    let otel_runtime = init_otel_runtime(otel_config)?;
    let otel_layer = otel_runtime.as_ref().map(|runtime| {
        tracing_opentelemetry::layer()
            .with_tracer(runtime.provider.tracer(runtime.service_name.clone()))
    });

    // NOTE: `structured_layer()`의 제네릭이 호출 지점에서 `Registry`로 단일화되면
    // `.with(env_filter).with(stdout_layer)` 형태에서 layer type mismatch가 발생한다.
    // stdout → otel → filter 순으로 조립하면 stdout layer는 `Registry`에만 붙고,
    // `EnvFilter`는 최상단에서 전체 파이프라인(stdout/otel)을 필터링한다.
    tracing_subscriber::registry()
        .with(stdout_layer)
        .with(otel_layer)
        .with(env_filter)
        .try_init()
        .context("tracing subscriber 초기화 실패")?;

    // stdout 전용 로깅 정책 (Fluent Bit → Loki)
    info!(
        stdout_only = true,
        otel_correlation = otel_runtime.is_some(),
        "file_logging_enabled"
    );

    if let Some(runtime) = &otel_runtime {
        info!(
            service = %runtime.service_name,
            endpoint = %runtime.endpoint,
            sample_rate = runtime.sample_rate,
            "otel_enabled"
        );
    }

    Ok(TracingRuntime { otel: otel_runtime })
}
