use std::time::Duration;

use metrics::{counter, histogram};

use super::{AlarmScheduler, LoopRunResult};

/// Prometheus metric: 루프 1회 실행 duration(초)
const METRIC_LOOP_DURATION_SECONDS: &str = "alarm_scheduler_loop_duration_seconds";
/// Prometheus metric: 루프 오류 누적 카운터
const METRIC_LOOP_ERRORS_TOTAL: &str = "alarm_scheduler_loop_errors_total";

impl LoopRunResult {
    pub(super) fn as_str(self) -> &'static str {
        match self {
            Self::Ok => "ok",
            Self::Error => "error",
            Self::Timeout => "timeout",
        }
    }
}

impl AlarmScheduler {
    pub(super) fn record_loop_duration(
        &self,
        loop_name: &'static str,
        result: LoopRunResult,
        elapsed: Duration,
    ) {
        histogram!(
            METRIC_LOOP_DURATION_SECONDS,
            "loop" => loop_name,
            "result" => result.as_str()
        )
        .record(elapsed.as_secs_f64());
    }

    pub(super) fn record_loop_error(&self, loop_name: &'static str, error_type: &'static str) {
        counter!(
            METRIC_LOOP_ERRORS_TOTAL,
            "loop" => loop_name,
            "error_type" => error_type
        )
        .increment(1);
    }
}
