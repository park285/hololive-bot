use alarm_core::constants::{
    NO_UPCOMING_INTERVAL, RECENTLY_NOTIFIED_WINDOW, TIER1_INTERVAL, TIER1_WINDOW, TIER2_INTERVAL,
    TIER2_WINDOW, TIER3_INTERVAL, TIER3_WINDOW, TIER4_INTERVAL,
};
use chrono::{DateTime, Utc};

pub(super) fn compute_next_check_at(
    nearest_start: Option<DateTime<Utc>>,
    last_notified_at: Option<DateTime<Utc>>,
) -> DateTime<Utc> {
    let now = Utc::now();

    let Some(start) = nearest_start else {
        // 예정 없음: 최근 알림 발송 채널은 고빈도 폴링 유지
        if let Some(notified) = last_notified_at {
            let elapsed = now - notified;
            let window = chrono::Duration::from_std(RECENTLY_NOTIFIED_WINDOW).unwrap();
            if elapsed <= window {
                return now + chrono::Duration::from_std(TIER2_INTERVAL).unwrap();
            }
        }
        return now + chrono::Duration::from_std(NO_UPCOMING_INTERVAL).unwrap();
    };

    let time_to_start = start - now;

    // 이미 지남 또는 현재 → Tier1
    if time_to_start <= chrono::Duration::zero() {
        return now + chrono::Duration::from_std(TIER1_INTERVAL).unwrap();
    }

    let tier1 = chrono::Duration::from_std(TIER1_WINDOW).unwrap();
    let tier2 = chrono::Duration::from_std(TIER2_WINDOW).unwrap();
    let tier3 = chrono::Duration::from_std(TIER3_WINDOW).unwrap();

    if time_to_start <= tier1 {
        now + chrono::Duration::from_std(TIER1_INTERVAL).unwrap()
    } else if time_to_start <= tier2 {
        now + chrono::Duration::from_std(TIER2_INTERVAL).unwrap()
    } else if time_to_start <= tier3 {
        now + chrono::Duration::from_std(TIER3_INTERVAL).unwrap()
    } else {
        now + chrono::Duration::from_std(TIER4_INTERVAL).unwrap()
    }
}
