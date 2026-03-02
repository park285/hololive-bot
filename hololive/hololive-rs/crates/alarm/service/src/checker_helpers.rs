use chrono::{DateTime, Utc};

// ─────────────────────────────────────────────────────────────────────────────
// YouTube 알림 체커 헬퍼 함수
// ─────────────────────────────────────────────────────────────────────────────

/// 시작 시각까지 남은 시간을 분 단위 내림으로 계산
/// 이미 지난 경우 0 반환, 정확히 현재 시각이면 0 반환
///
/// 내림 사용 이유: 카카오톡은 메시지 도착 시각을 분 단위 절삭으로 표시하므로,
/// 올림 시 실제 발송 시각이 "N+1분 전"으로 보이는 UX 이슈가 발생한다.
/// 예: 5분 1초 전 발송 → ceil=6 스킵 → 4분 1초 전 발송 → 카카오톡 "4분 전" 표시
/// 내림 시: 5분 59초 전 → floor=5 → 발송 → 카카오톡 "5분 전" 표시 (정확)
pub fn minutes_until_floor(start: DateTime<Utc>, now: DateTime<Utc>) -> i32 {
    let diff = start - now;
    let secs = diff.num_seconds();
    if secs <= 0 {
        return 0;
    }
    // 내림 나눗셈: floor(secs / 60)
    (secs / 60) as i32
}

/// 일정 변경 메시지 포맷
/// old_time > new_time → 앞당겨짐 / old_time < new_time → 늦춰짐 / 동일 → 빈 문자열
pub fn format_schedule_change_message(old_time: DateTime<Utc>, new_time: DateTime<Utc>) -> String {
    if old_time < new_time {
        "일정이 늦춰졌습니다.".to_owned()
    } else if old_time > new_time {
        "일정이 앞당겨졌습니다.".to_owned()
    } else {
        String::new()
    }
}

/// minutes_until이 target_minutes 목록에 포함되는지 확인
pub fn is_target_minute(target_minutes: &[i32], minutes_until: i32) -> bool {
    target_minutes.contains(&minutes_until)
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use chrono::Duration;

    use super::*;

    // ── minutes_until_floor 테스트 ───────────────────────────────────────────

    /// 4분 30초 후 → 4분 내림
    #[test]
    fn minutes_until_floor_4min_30sec() {
        let now = Utc::now();
        let start = now + Duration::seconds(270); // 4분 30초
        assert_eq!(minutes_until_floor(start, now), 4);
    }

    /// 정확히 5분 후 → 5
    #[test]
    fn minutes_until_floor_exactly_5min() {
        let now = Utc::now();
        let start = now + Duration::seconds(300); // 5분 정확
        assert_eq!(minutes_until_floor(start, now), 5);
    }

    /// 5분 59초 후 → 5분 내림
    #[test]
    fn minutes_until_floor_5min_59sec() {
        let now = Utc::now();
        let start = now + Duration::seconds(359); // 5분 59초
        assert_eq!(minutes_until_floor(start, now), 5);
    }

    /// 4분 59초 후 → 4분 내림
    #[test]
    fn minutes_until_floor_4min_59sec() {
        let now = Utc::now();
        let start = now + Duration::seconds(299); // 4분 59초
        assert_eq!(minutes_until_floor(start, now), 4);
    }

    /// 이미 지난 시각 → 0
    #[test]
    fn minutes_until_floor_past() {
        let now = Utc::now();
        let start = now - Duration::seconds(60);
        assert_eq!(minutes_until_floor(start, now), 0);
    }

    /// 정확히 현재 시각 → 0
    #[test]
    fn minutes_until_floor_now_returns_zero() {
        let now = Utc::now();
        assert_eq!(minutes_until_floor(now, now), 0);
    }

    /// 1초 후 → 0 (내림)
    #[test]
    fn minutes_until_floor_1sec_future() {
        let now = Utc::now();
        let start = now + Duration::seconds(1);
        assert_eq!(minutes_until_floor(start, now), 0);
    }

    /// 60초 후 → 1분 정확
    #[test]
    fn minutes_until_floor_exactly_1min() {
        let now = Utc::now();
        let start = now + Duration::seconds(60);
        assert_eq!(minutes_until_floor(start, now), 1);
    }

    // ── format_schedule_change_message 테스트 ────────────────────────────────

    /// old < new → 늦춰짐
    #[test]
    fn format_schedule_change_message_delayed() {
        let old = Utc::now();
        let new = old + Duration::minutes(30);
        assert_eq!(
            format_schedule_change_message(old, new),
            "일정이 늦춰졌습니다."
        );
    }

    /// old > new → 앞당겨짐
    #[test]
    fn format_schedule_change_message_early() {
        let old = Utc::now();
        let new = old - Duration::minutes(30);
        assert_eq!(
            format_schedule_change_message(old, new),
            "일정이 앞당겨졌습니다."
        );
    }

    /// old == new → 빈 문자열
    #[test]
    fn format_schedule_change_message_same() {
        let t = Utc::now();
        assert_eq!(format_schedule_change_message(t, t), "");
    }

    // ── is_target_minute 테스트 ──────────────────────────────────────────────

    /// 목록에 포함된 분 → true
    #[test]
    fn is_target_minute_in_list() {
        assert!(is_target_minute(&[5, 3, 1], 5));
        assert!(is_target_minute(&[5, 3, 1], 3));
        assert!(is_target_minute(&[5, 3, 1], 1));
    }

    /// 목록에 없는 분 → false
    #[test]
    fn is_target_minute_not_in_list() {
        assert!(!is_target_minute(&[5, 3, 1], 10));
        assert!(!is_target_minute(&[5, 3, 1], 2));
        assert!(!is_target_minute(&[5, 3, 1], 0));
    }

    /// 빈 목록 → false
    #[test]
    fn is_target_minute_empty_list() {
        assert!(!is_target_minute(&[], 5));
    }
}
