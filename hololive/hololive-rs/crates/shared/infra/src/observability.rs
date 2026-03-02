// 공통 observability 인프라: 로그 포매터, OTEL 런타임, tracing 초기화, 텔레메트리 설정 해석

use std::fmt;

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
use tracing::{Event, Subscriber, error, info};
use tracing_subscriber::{
    fmt::{
        FmtContext, FormatEvent, FormatFields,
        format::Writer,
        time::{FormatTime, OffsetTime},
    },
    layer::SubscriberExt,
    registry::LookupSpan,
    util::SubscriberInitExt,
};

const KST_OFFSET_HOURS: i8 = 9;
const KST_TIME_FORMAT: &[time::format_description::FormatItem<'static>] = time::macros::format_description!(
    "[year]-[month]-[day]T[hour]:[minute]:[second][offset_hour sign:mandatory]:[offset_minute]"
);

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

// ── 로그 포매터 ──

#[derive(Clone)]
struct UnifiedLogFormatter<T> {
    timer: T,
}

impl<T> UnifiedLogFormatter<T> {
    fn new(timer: T) -> Self {
        Self { timer }
    }
}

impl<S, N, T> FormatEvent<S, N> for UnifiedLogFormatter<T>
where
    S: Subscriber + for<'span> LookupSpan<'span>,
    N: for<'writer> FormatFields<'writer> + 'static,
    T: FormatTime + Clone + Send + Sync + 'static,
{
    fn format_event(
        &self,
        _ctx: &FmtContext<'_, S, N>,
        mut writer: Writer<'_>,
        event: &Event<'_>,
    ) -> fmt::Result {
        self.timer.format_time(&mut writer)?;

        let metadata = event.metadata();
        write!(writer, " {} ", short_level(*metadata.level()))?;

        if let Some(file) = metadata.file() {
            if let Some(line) = metadata.line() {
                write!(writer, "{file}:{line} ")?;
            } else {
                write!(writer, "{file} ")?;
            }
        } else {
            write!(writer, "{} ", metadata.target())?;
        }

        let mut visitor = EventFieldVisitor::default();
        event.record(&mut visitor);
        visitor.write(&mut writer)?;
        writeln!(writer)
    }
}

#[derive(Default)]
struct EventFieldVisitor {
    message: Option<String>,
    fields: Vec<(String, String)>,
}

impl EventFieldVisitor {
    fn record_value(&mut self, name: &str, value: String) {
        if name == "message" {
            self.message = Some(value);
            return;
        }
        self.fields.push((name.to_owned(), value));
    }

    #[allow(clippy::useless_let_if_seq)]
    fn write(&self, writer: &mut Writer<'_>) -> fmt::Result {
        let mut wrote = false;
        if let Some(message) = &self.message {
            write!(writer, "{message}")?;
            wrote = true;
        }

        for (key, value) in &self.fields {
            if wrote {
                write!(writer, " ")?;
            }
            write!(writer, "{key}={value}")?;
            wrote = true;
        }
        Ok(())
    }
}

impl tracing::field::Visit for EventFieldVisitor {
    fn record_bool(&mut self, field: &tracing::field::Field, value: bool) {
        self.record_value(field.name(), value.to_string());
    }

    fn record_i64(&mut self, field: &tracing::field::Field, value: i64) {
        self.record_value(field.name(), value.to_string());
    }

    fn record_u64(&mut self, field: &tracing::field::Field, value: u64) {
        self.record_value(field.name(), value.to_string());
    }

    fn record_f64(&mut self, field: &tracing::field::Field, value: f64) {
        self.record_value(field.name(), value.to_string());
    }

    fn record_str(&mut self, field: &tracing::field::Field, value: &str) {
        self.record_value(field.name(), format_string_value(value));
    }

    fn record_debug(&mut self, field: &tracing::field::Field, value: &dyn fmt::Debug) {
        self.record_value(field.name(), format!("{value:?}"));
    }
}

fn format_string_value(value: &str) -> String {
    if value.is_empty() || value.contains(char::is_whitespace) || value.contains('=') {
        format!("{value:?}")
    } else {
        value.to_owned()
    }
}

fn short_level(level: tracing::Level) -> &'static str {
    match level {
        tracing::Level::TRACE => "TRC",
        tracing::Level::DEBUG => "DBG",
        tracing::Level::INFO => "INF",
        tracing::Level::WARN => "WRN",
        tracing::Level::ERROR => "ERR",
    }
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

// ── KST 타이머 ──

fn build_kst_timer() -> Result<OffsetTime<&'static [time::format_description::FormatItem<'static>]>>
{
    let kst_offset =
        time::UtcOffset::from_hms(KST_OFFSET_HOURS, 0, 0).context("KST 오프셋 초기화 실패")?;
    Ok(OffsetTime::new(kst_offset, KST_TIME_FORMAT))
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

    let stdout_timer = build_kst_timer().context("stdout KST 타이머 초기화 실패")?;
    let stdout_layer = tracing_subscriber::fmt::layer()
        .event_format(UnifiedLogFormatter::new(stdout_timer))
        .with_ansi(false)
        .with_writer(std::io::stdout);

    let otel_runtime = init_otel_runtime(otel_config)?;
    let otel_layer = otel_runtime.as_ref().map(|runtime| {
        tracing_opentelemetry::layer()
            .with_tracer(runtime.provider.tracer(runtime.service_name.clone()))
    });

    tracing_subscriber::registry()
        .with(env_filter)
        .with(stdout_layer)
        .with(otel_layer)
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
