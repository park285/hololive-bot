use std::sync::{Mutex, OnceLock};

use opentelemetry::{KeyValue, global};
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::{
    Resource,
    propagation::TraceContextPropagator,
    trace::{Sampler, SdkTracerProvider},
};
use serde::Deserialize;
use shared_core::error::SharedError;
use tracing::warn;
use validator::Validate;

#[derive(Debug, Clone, Deserialize, Validate)]
pub struct TelemetryConfig {
    pub enabled: bool,
    #[validate(length(min = 1))]
    pub endpoint: String,
    #[validate(length(min = 1))]
    pub service_name: String,
    #[validate(range(min = 0.0, max = 1.0))]
    pub sample_ratio: f64,
}

impl Default for TelemetryConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            endpoint: "otel-collector:4317".to_owned(),
            service_name: "hololive-shared".to_owned(),
            sample_ratio: 1.0,
        }
    }
}

static TELEMETRY_PROVIDER: OnceLock<Mutex<Option<SdkTracerProvider>>> = OnceLock::new();

pub fn init_telemetry(config: &TelemetryConfig) -> Result<(), SharedError> {
    if !config.enabled {
        return Ok(());
    }

    let endpoint = normalize_endpoint(&config.endpoint);
    let exporter = opentelemetry_otlp::SpanExporter::builder()
        .with_tonic()
        .with_endpoint(endpoint)
        .build()
        .map_err(|e| SharedError::Config(format!("build otlp exporter: {e}")))?;

    let sample_ratio = config.sample_ratio.clamp(0.0, 1.0);

    let resource = Resource::builder_empty()
        .with_attributes(vec![KeyValue::new(
            "service.name",
            config.service_name.clone(),
        )])
        .build();

    let provider = SdkTracerProvider::builder()
        .with_resource(resource)
        .with_sampler(Sampler::TraceIdRatioBased(sample_ratio))
        .with_batch_exporter(exporter)
        .build();

    global::set_text_map_propagator(TraceContextPropagator::new());
    global::set_tracer_provider(provider.clone());

    let slot = TELEMETRY_PROVIDER.get_or_init(|| Mutex::new(None));
    let mut guard = slot
        .lock()
        .map_err(|_| SharedError::Config("telemetry provider lock poisoned".to_owned()))?;

    if let Some(previous) = guard.take()
        && let Err(err) = previous.shutdown()
    {
        warn!(error = %err, "shutdown previous telemetry provider failed");
    }

    *guard = Some(provider);
    Ok(())
}

pub fn shutdown_telemetry() {
    let Some(slot) = TELEMETRY_PROVIDER.get() else {
        return;
    };

    let Ok(mut guard) = slot.lock() else {
        return;
    };

    if let Some(provider) = guard.take()
        && let Err(err) = provider.shutdown()
    {
        warn!(error = %err, "shutdown telemetry provider failed");
    }
}

fn normalize_endpoint(endpoint: &str) -> String {
    let trimmed = endpoint.trim();
    if trimmed.starts_with("http://") || trimmed.starts_with("https://") {
        return trimmed.to_owned();
    }
    format!("http://{trimmed}")
}
