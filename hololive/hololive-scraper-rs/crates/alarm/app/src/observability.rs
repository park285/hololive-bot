use alarm_infra::config::{LoggingConfig, TelemetryConfig};
use anyhow::{Context, Result};
use metrics_exporter_prometheus::{PrometheusBuilder, PrometheusHandle};
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
use std::{fmt, fs, path::PathBuf};
use tracing::{Event, Subscriber, error, info};
use tracing_appender::non_blocking::WorkerGuard;
use tracing_subscriber::{
    fmt::{
        FmtContext, FormatEvent, FormatFields,
        format::Writer,
        time::{FormatTime, OffsetTime},
        writer::{BoxMakeWriter, MakeWriterExt},
    },
    layer::SubscriberExt,
    registry::LookupSpan,
    util::SubscriberInitExt,
};

const KST_OFFSET_HOURS: i8 = 9;
const KST_TIME_FORMAT: &[time::format_description::FormatItem<'static>] = time::macros::format_description!(
    "[year]-[month]-[day]T[hour]:[minute]:[second][offset_hour sign:mandatory]:[offset_minute]"
);

#[derive(Debug)]
struct OTelRuntime {
    provider: SdkTracerProvider,
    service_name: String,
    endpoint: String,
    sample_rate: f64,
}

#[derive(Debug)]
pub struct TracingRuntime {
    _guards: Vec<WorkerGuard>,
    otel: Option<OTelRuntime>,
}

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
        write!(writer, " {} ", short_level(metadata.level()))?;

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
        self.fields.push((name.to_string(), value));
    }

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
        value.to_string()
    }
}

fn short_level(level: &tracing::Level) -> &'static str {
    match *level {
        tracing::Level::TRACE => "TRC",
        tracing::Level::DEBUG => "DBG",
        tracing::Level::INFO => "INF",
        tracing::Level::WARN => "WRN",
        tracing::Level::ERROR => "ERR",
    }
}

impl Drop for TracingRuntime {
    fn drop(&mut self) {
        if let Some(otel) = self.otel.take()
            && let Err(err) = otel.provider.shutdown()
        {
            error!(error = %err, "otel provider shutdown failed");
        }
    }
}

/// stdout 기본 + file logging 선택적 출력 (KST 기준), OTEL 선택적 활성화
pub fn init_tracing(config: &LoggingConfig, telemetry: &TelemetryConfig) -> Result<TracingRuntime> {
    let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new(config.level.clone()));

    let mut guards = Vec::new();
    let stdout_timer = build_kst_timer().context("stdout KST 타이머 초기화 실패")?;
    let stdout_layer = tracing_subscriber::fmt::layer()
        .event_format(UnifiedLogFormatter::new(stdout_timer))
        .with_ansi(false)
        .with_writer(std::io::stdout);

    let (file_writer, file_logging_paths): (BoxMakeWriter, Option<(PathBuf, PathBuf)>) = if config
        .file_enabled
    {
        fs::create_dir_all(&config.dir)
            .with_context(|| format!("로그 디렉터리 생성 실패: {}", config.dir))?;

        let service_appender = tracing_appender::rolling::never(&config.dir, &config.file);
        let (service_writer, service_guard) = tracing_appender::non_blocking(service_appender);
        guards.push(service_guard);

        let combined_file = config.combined_file.trim();
        let combined_enabled = !combined_file.is_empty() && combined_file != config.file.trim();
        let file_writer: BoxMakeWriter = if combined_enabled {
            let combined_appender = tracing_appender::rolling::never(&config.dir, combined_file);
            let (combined_writer, combined_guard) =
                tracing_appender::non_blocking(combined_appender);
            guards.push(combined_guard);
            BoxMakeWriter::new(service_writer.and(combined_writer))
        } else {
            BoxMakeWriter::new(service_writer)
        };

        let service_log_path = PathBuf::from(&config.dir).join(&config.file);
        let combined_log_name = if combined_enabled {
            combined_file.to_string()
        } else {
            config.file.clone()
        };
        let combined_log_path = PathBuf::from(&config.dir).join(combined_log_name);
        (file_writer, Some((service_log_path, combined_log_path)))
    } else {
        (BoxMakeWriter::new(std::io::sink), None)
    };

    let file_timer = build_kst_timer().context("file KST 타이머 초기화 실패")?;
    let file_layer = tracing_subscriber::fmt::layer()
        .event_format(UnifiedLogFormatter::new(file_timer))
        .with_ansi(false)
        .with_writer(file_writer);

    let otel_runtime = init_otel_runtime(telemetry)?;
    let otel_layer = otel_runtime.as_ref().map(|runtime| {
        tracing_opentelemetry::layer()
            .with_tracer(runtime.provider.tracer(runtime.service_name.clone()))
    });

    tracing_subscriber::registry()
        .with(env_filter)
        .with(stdout_layer)
        .with(file_layer)
        .with(otel_layer)
        .try_init()
        .context("tracing subscriber 초기화 실패")?;

    if let Some((service_log_path, combined_log_path)) = file_logging_paths {
        info!(
            path = %service_log_path.display(),
            combined = %combined_log_path.display(),
            stdout_only = false,
            otel_correlation = otel_runtime.is_some(),
            "file_logging_enabled"
        );
    } else {
        info!(
            stdout_only = true,
            otel_correlation = otel_runtime.is_some(),
            "file_logging_enabled"
        );
    }

    if let Some(runtime) = &otel_runtime {
        info!(
            service = %runtime.service_name,
            endpoint = %runtime.endpoint,
            sample_rate = runtime.sample_rate,
            "otel_enabled"
        );
    }

    Ok(TracingRuntime {
        _guards: guards,
        otel: otel_runtime,
    })
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

fn parse_bool_env(name: &str) -> Option<bool> {
    let raw = std::env::var(name).ok()?;
    match raw.trim().to_ascii_lowercase().as_str() {
        "1" | "true" | "yes" | "on" => Some(true),
        "0" | "false" | "no" | "off" => Some(false),
        _ => None,
    }
}

fn parse_f64_env(name: &str) -> Option<f64> {
    std::env::var(name)
        .ok()
        .and_then(|value| value.trim().parse::<f64>().ok())
}

fn init_otel_runtime(config: &TelemetryConfig) -> Result<Option<OTelRuntime>> {
    if !config.enabled {
        return Ok(None);
    }

    let endpoint = normalize_otlp_endpoint(&config.otlp_endpoint, config.otlp_insecure);
    let exporter = opentelemetry_otlp::SpanExporter::builder()
        .with_tonic()
        .with_endpoint(endpoint.clone())
        .build()
        .context("OTLP span exporter 빌드 실패")?;

    let sample_rate = config.sample_rate.clamp(0.0, 1.0);
    let resource = Resource::builder_empty()
        .with_attributes(vec![
            KeyValue::new("service.name", config.service_name.clone()),
            KeyValue::new("service.version", config.service_version.clone()),
            KeyValue::new("deployment.environment", config.environment.clone()),
        ])
        .build();

    let provider = SdkTracerProvider::builder()
        .with_resource(resource)
        .with_sampler(Sampler::TraceIdRatioBased(sample_rate))
        .with_batch_exporter(exporter)
        .build();

    global::set_text_map_propagator(TraceContextPropagator::new());
    global::set_tracer_provider(provider.clone());

    // 시작 span 즉시 flush (exporter 연결 확인)
    let tracer = provider.tracer(config.service_name.clone());
    let mut startup_span = tracer.start("alarm.startup");
    startup_span.end();
    if let Err(err) = provider.force_flush() {
        error!(error = %err, "otel span force_flush failed");
    }

    Ok(Some(OTelRuntime {
        provider,
        service_name: config.service_name.clone(),
        endpoint,
        sample_rate,
    }))
}

/// OTLP 엔드포인트에 scheme 미지정 시 http/https 추가
pub fn normalize_otlp_endpoint(endpoint: &str, insecure: bool) -> String {
    let trimmed = endpoint.trim();
    if trimmed.starts_with("http://") || trimmed.starts_with("https://") {
        return trimmed.to_string();
    }

    let scheme = if insecure { "http" } else { "https" };
    format!("{scheme}://{trimmed}")
}

/// Prometheus metrics recorder 초기화
pub fn init_metrics() -> Result<PrometheusHandle> {
    PrometheusBuilder::new()
        .install_recorder()
        .context("prometheus metrics recorder 초기화 실패")
}

fn build_kst_timer() -> Result<OffsetTime<&'static [time::format_description::FormatItem<'static>]>>
{
    let kst_offset =
        time::UtcOffset::from_hms(KST_OFFSET_HOURS, 0, 0).context("KST 오프셋 초기화 실패")?;
    Ok(OffsetTime::new(kst_offset, KST_TIME_FORMAT))
}
