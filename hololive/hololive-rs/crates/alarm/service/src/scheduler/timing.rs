use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// 현재 epoch millisecond
pub(super) fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis())
        .unwrap_or(0)
        .min(u128::from(u64::MAX)) as u64
}

pub(super) fn duration_to_ms(duration: Duration) -> u64 {
    duration.as_millis().min(u128::from(u64::MAX)) as u64
}
