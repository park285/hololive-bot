//! 키 생성 시 사용하는 공통 헬퍼.

use chrono::{DateTime, Duration, DurationRound, Utc};

pub fn normalize_scheduled_minute(start_scheduled: DateTime<Utc>) -> DateTime<Utc> {
    truncate_to_1min(start_scheduled)
}

pub fn normalize_key(s: &str) -> String {
    let lowered = s.trim().to_lowercase();
    if lowered.is_empty() {
        return String::new();
    }

    lowered
        .chars()
        .filter(|&c| {
            !matches!(
                c,
                ' ' | '-'
                    | '_'
                    | '.'
                    | '!'
                    | '☆'
                    | '・'
                    | '\u{2018}'
                    | '\u{2019}'
                    | '\''
                    | 'ー'
                    | '—'
            )
        })
        .collect()
}

pub fn truncate_to_10min(dt: DateTime<Utc>) -> DateTime<Utc> {
    dt.duration_trunc(Duration::minutes(10)).unwrap_or(dt)
}

pub fn truncate_to_1min(dt: DateTime<Utc>) -> DateTime<Utc> {
    dt.duration_trunc(Duration::minutes(1)).unwrap_or(dt)
}
