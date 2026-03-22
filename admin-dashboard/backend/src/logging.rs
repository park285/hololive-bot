use std::fmt;
use std::path::Path;

use chrono::Local;
use tracing::field::{Field, Visit};
use tracing::{Event, Level, Subscriber};
use tracing_appender::non_blocking::WorkerGuard;
use tracing_subscriber::EnvFilter;
use tracing_subscriber::fmt::format::Writer;
use tracing_subscriber::fmt::{FmtContext, FormatEvent, FormatFields, layer};
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::registry::LookupSpan;
use tracing_subscriber::util::SubscriberInitExt;

pub fn init_tracing(cfg: &crate::config::Config) -> Vec<WorkerGuard> {
    let env_filter =
        EnvFilter::try_from_env("LOG_LEVEL").unwrap_or_else(|_| EnvFilter::new("info"));
    let stdout_layer = layer().event_format(LegacyLogFormat).with_ansi(false);

    let mut guards = Vec::new();
    let registry = tracing_subscriber::registry()
        .with(env_filter)
        .with(stdout_layer);

    if std::fs::create_dir_all(&cfg.log_dir).is_ok() {
        let file_appender = tracing_appender::rolling::never(&cfg.log_dir, "admin.log");
        let (file_writer, guard) = tracing_appender::non_blocking(file_appender);
        guards.push(guard);

        registry
            .with(
                layer()
                    .event_format(LegacyLogFormat)
                    .with_ansi(false)
                    .with_writer(file_writer),
            )
            .init();
    } else {
        registry.init();
    }

    guards
}

#[derive(Debug, Clone, Copy)]
struct LegacyLogFormat;

impl<S, N> FormatEvent<S, N> for LegacyLogFormat
where
    S: Subscriber + for<'a> LookupSpan<'a>,
    N: for<'writer> FormatFields<'writer> + 'static,
{
    fn format_event(
        &self,
        _ctx: &FmtContext<'_, S, N>,
        mut writer: Writer<'_>,
        event: &Event<'_>,
    ) -> fmt::Result {
        write!(writer, "{}", Local::now().format("%Y-%m-%dT%H:%M:%S%:z"))?;
        write!(writer, " {} ", short_level(*event.metadata().level()))?;
        write!(writer, "{} ", source_label(event.metadata()))?;

        let mut visitor = FieldCollector::default();
        event.record(&mut visitor);

        if let Some(message) = visitor.message {
            write!(writer, "{message}")?;
        }

        for field in visitor.fields {
            if field.is_empty() {
                continue;
            }
            if !field.starts_with("message=") {
                write!(writer, " {field}")?;
            }
        }

        writeln!(writer)
    }
}

const fn short_level(level: Level) -> &'static str {
    match level {
        Level::TRACE => "TRC",
        Level::DEBUG => "DBG",
        Level::INFO => "INF",
        Level::WARN => "WRN",
        Level::ERROR => "ERR",
    }
}

fn source_label(metadata: &tracing::Metadata<'_>) -> String {
    match (metadata.file(), metadata.line()) {
        (Some(file), Some(line)) => {
            let file_name = Path::new(file)
                .file_name()
                .and_then(|name| name.to_str())
                .unwrap_or(file);
            format!("{file_name}:{line}")
        }
        _ => metadata.target().to_string(),
    }
}

#[derive(Debug, Default)]
struct FieldCollector {
    message: Option<String>,
    fields: Vec<String>,
}

impl Visit for FieldCollector {
    fn record_str(&mut self, field: &Field, value: &str) {
        if field.name() == "message" {
            self.message = Some(value.to_string());
        } else {
            self.fields.push(format!("{}={value}", field.name()));
        }
    }

    fn record_debug(&mut self, field: &Field, value: &dyn fmt::Debug) {
        let rendered = format!("{value:?}");
        if field.name() == "message" {
            self.message = Some(rendered.trim_matches('"').to_string());
        } else {
            self.fields
                .push(format!("{}={}", field.name(), rendered.trim_matches('"')));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tracing::Level;

    #[test]
    fn test_short_level_mapping() {
        assert_eq!(short_level(Level::INFO), "INF");
        assert_eq!(short_level(Level::WARN), "WRN");
        assert_eq!(short_level(Level::ERROR), "ERR");
    }

    #[test]
    fn test_source_label_uses_file_name_and_line() {
        let metadata = tracing::Metadata::new(
            "test",
            "admin_dashboard::logging",
            Level::INFO,
            Some("src/main.rs"),
            Some(42),
            Some("admin_dashboard"),
            tracing::field::FieldSet::new(&[], tracing::callsite::Identifier(&DUMMY_CALLSITE)),
            tracing::metadata::Kind::EVENT,
        );
        assert_eq!(source_label(&metadata), "main.rs:42");
    }

    struct DummyCallsite;
    static DUMMY_CALLSITE: DummyCallsite = DummyCallsite;

    impl tracing::callsite::Callsite for DummyCallsite {
        fn set_interest(&self, _: tracing::subscriber::Interest) {}
        fn metadata(&self) -> &tracing::Metadata<'_> {
            unreachable!("metadata is not accessed in this test")
        }
    }
}
