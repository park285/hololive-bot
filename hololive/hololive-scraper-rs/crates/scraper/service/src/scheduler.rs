use chrono::{DateTime, Datelike, FixedOffset, Utc};
use cron::Schedule;
use std::{str::FromStr, time::Duration};

const KST_OFFSET_SECS: i32 = 9 * 60 * 60;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum ScrapeTriggerType {
    Regular,
    Retry,
}

pub(crate) fn kst_offset() -> FixedOffset {
    FixedOffset::east_opt(KST_OFFSET_SECS).expect("KST fixed offset should be valid")
}

pub(crate) fn format_kst(value: DateTime<Utc>) -> String {
    value
        .with_timezone(&kst_offset())
        .format("%Y-%m-%d %H:%M:%S %:z")
        .to_string()
}

pub(crate) fn calculate_next_regular_run_for_hour(
    now: DateTime<Utc>,
    scrape_hour_kst: u8,
) -> DateTime<Utc> {
    let target_hour = scrape_hour_kst.min(23) as u32;
    let now_kst = now.with_timezone(&kst_offset());
    let daily_kst_expr = format!("0 0 {target_hour} * * *");
    let schedule = Schedule::from_str(&daily_kst_expr).expect("daily KST cron expression");
    let next_kst = schedule
        .after(&now_kst)
        .next()
        .expect("daily cron should always produce next datetime");
    next_kst.with_timezone(&Utc)
}

pub(crate) fn build_retry_runs_from_delays(
    base_run: DateTime<Utc>,
    failed_at: DateTime<Utc>,
    retry_delays: &[Duration],
) -> Vec<DateTime<Utc>> {
    let kst = kst_offset();
    let base_kst = base_run.with_timezone(&kst);
    let failed_kst = failed_at.with_timezone(&kst);

    retry_delays
        .iter()
        .filter_map(|delay| {
            if delay.is_zero() {
                return None;
            }

            let chrono_delay = chrono::Duration::from_std(*delay).ok()?;
            let candidate = base_kst + chrono_delay;

            if candidate.year() != base_kst.year() || candidate.ordinal() != base_kst.ordinal() {
                return None;
            }
            if candidate <= failed_kst {
                return None;
            }

            Some(candidate.with_timezone(&Utc))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::{build_retry_runs_from_delays, calculate_next_regular_run_for_hour};
    use chrono::{TimeZone, Utc};
    use std::collections::VecDeque;
    use std::time::Duration;

    #[test]
    fn calculate_next_regular_run_uses_same_day_when_before_hour() {
        let now = Utc
            .with_ymd_and_hms(2026, 2, 23, 20, 0, 0)
            .single()
            .expect("valid datetime"); // KST 05:00

        let next = calculate_next_regular_run_for_hour(now, 6);
        assert_eq!(next.to_rfc3339(), "2026-02-23T21:00:00+00:00"); // KST 06:00
    }

    #[test]
    fn calculate_next_regular_run_rolls_to_next_day_when_past_hour() {
        let now = Utc
            .with_ymd_and_hms(2026, 2, 23, 22, 0, 0)
            .single()
            .expect("valid datetime"); // KST 07:00

        let next = calculate_next_regular_run_for_hour(now, 6);
        assert_eq!(next.to_rfc3339(), "2026-02-24T21:00:00+00:00"); // next day KST 06:00
    }

    #[test]
    fn calculate_next_regular_run_rolls_to_next_day_when_exact_hour() {
        let now = Utc
            .with_ymd_and_hms(2026, 2, 23, 21, 0, 0)
            .single()
            .expect("valid datetime"); // KST 06:00 exact

        let next = calculate_next_regular_run_for_hour(now, 6);
        assert_eq!(next.to_rfc3339(), "2026-02-24T21:00:00+00:00"); // next day KST 06:00
    }

    #[test]
    fn build_retry_runs_uses_same_day_future_window() {
        let base = Utc
            .with_ymd_and_hms(2026, 2, 23, 21, 0, 0)
            .single()
            .expect("valid datetime"); // KST 06:00
        let failed = Utc
            .with_ymd_and_hms(2026, 2, 23, 21, 1, 0)
            .single()
            .expect("valid datetime");

        let runs = build_retry_runs_from_delays(
            base,
            failed,
            &[
                Duration::from_secs(30 * 60),
                Duration::from_secs(2 * 60 * 60),
                Duration::from_secs(6 * 60 * 60),
            ],
        );

        let actual = runs
            .iter()
            .map(|run| run.to_rfc3339())
            .collect::<Vec<String>>();

        assert_eq!(
            actual,
            vec![
                "2026-02-23T21:30:00+00:00",
                "2026-02-23T23:00:00+00:00",
                "2026-02-24T03:00:00+00:00",
            ]
        );
    }

    #[test]
    fn build_retry_runs_drops_cross_day_or_past_candidates() {
        let base = Utc
            .with_ymd_and_hms(2026, 2, 23, 14, 30, 0)
            .single()
            .expect("valid datetime"); // KST 23:30
        let failed = Utc
            .with_ymd_and_hms(2026, 2, 23, 14, 45, 0)
            .single()
            .expect("valid datetime");

        let runs = build_retry_runs_from_delays(
            base,
            failed,
            &[
                Duration::from_secs(20 * 60),
                Duration::from_secs(40 * 60),
                Duration::from_secs(2 * 60 * 60),
            ],
        );

        let actual = runs
            .iter()
            .map(|run| run.to_rfc3339())
            .collect::<Vec<String>>();

        assert_eq!(actual, vec!["2026-02-23T14:50:00+00:00"]); // KST 23:50 only
    }

    #[test]
    fn retry_queue_vecdeque_keeps_fifo_order() {
        let first = Utc
            .with_ymd_and_hms(2026, 2, 23, 21, 30, 0)
            .single()
            .expect("valid datetime");
        let second = Utc
            .with_ymd_and_hms(2026, 2, 23, 23, 0, 0)
            .single()
            .expect("valid datetime");

        let mut queue = VecDeque::from(vec![first, second]);
        assert_eq!(queue.front().copied(), Some(first));
        assert_eq!(queue.pop_front(), Some(first));
        assert_eq!(queue.pop_front(), Some(second));
        assert_eq!(queue.pop_front(), None);
    }
}
