use std::fmt;

use anyhow::{Context, Result};
use tracing::{Event, Subscriber};
use tracing_subscriber::{
    fmt::{
        FmtContext, FormatEvent, FormatFields,
        format::Writer,
        time::{FormatTime, OffsetTime},
    },
    layer::Layer,
    registry::LookupSpan,
};

const KST_OFFSET_HOURS: i8 = 9;
const KST_TIME_FORMAT: &[time::format_description::FormatItem<'static>] = time::macros::format_description!(
    "[year]-[month]-[day]T[hour]:[minute]:[second][offset_hour sign:mandatory]:[offset_minute]"
);

/// 사람이 읽기 쉬운(Pretty) 구조화 출력 레이어 (KST 타임스탬프 포함).
///
/// 기존 `init_unified_tracing()`의 stdout 포맷을 그대로 재사용한다.
pub fn structured_layer<S>() -> Result<impl Layer<S> + Send + Sync>
where
    S: Subscriber + for<'span> LookupSpan<'span>,
{
    let stdout_timer = build_kst_timer().context("stdout KST 타이머 초기화 실패")?;
    Ok(tracing_subscriber::fmt::layer()
        .event_format(UnifiedLogFormatter::new(stdout_timer))
        .with_ansi(false)
        .with_writer(std::io::stdout))
}

/// JSON 출력 레이어.
///
/// 현재 shared-infra는 기본 정책이 Pretty(stdout) + OTEL 옵션이므로,
/// 외부에서 JSON 로깅을 선택하고 싶을 때 사용할 수 있도록 builder만 제공한다.
pub fn json_layer<S>() -> Result<impl Layer<S> + Send + Sync>
where
    S: Subscriber + for<'span> LookupSpan<'span>,
{
    let stdout_timer = build_kst_timer().context("stdout KST 타이머 초기화 실패")?;
    Ok(tracing_subscriber::fmt::layer()
        .json()
        .with_timer(stdout_timer)
        .with_ansi(false)
        .with_writer(std::io::stdout))
}

fn build_kst_timer() -> Result<OffsetTime<&'static [time::format_description::FormatItem<'static>]>>
{
    let kst_offset =
        time::UtcOffset::from_hms(KST_OFFSET_HOURS, 0, 0).context("KST 오프셋 초기화 실패")?;
    Ok(OffsetTime::new(kst_offset, KST_TIME_FORMAT))
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
