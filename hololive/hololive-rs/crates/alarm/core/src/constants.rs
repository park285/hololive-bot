use std::time::Duration;

// ─────────────────────────────────────────────────────────────────────────────
// Tier 판정 구간 (timeToStart 기준 — 이 값 이하이면 해당 Tier로 분류)
// ─────────────────────────────────────────────────────────────────────────────

/// Tier 1 구간: 45분 이내 — 1분 간격 폴링 (알림 분 단위 정확도 확보)
pub const TIER1_WINDOW: Duration = Duration::from_secs(45 * 60);

/// Tier 2 구간: 3시간 이내 — 3분 간격 폴링
pub const TIER2_WINDOW: Duration = Duration::from_secs(3 * 3_600);

/// Tier 3 구간: 12시간 이내 — 10분 간격 폴링
pub const TIER3_WINDOW: Duration = Duration::from_secs(12 * 3_600);

// ─────────────────────────────────────────────────────────────────────────────
// Tier 폴링 간격
// ─────────────────────────────────────────────────────────────────────────────

/// Tier 1 폴링 간격: 1분
pub const TIER1_INTERVAL: Duration = Duration::from_secs(60);

/// Tier 2 폴링 간격: 3분
pub const TIER2_INTERVAL: Duration = Duration::from_secs(3 * 60);

/// Tier 3 폴링 간격: 10분
pub const TIER3_INTERVAL: Duration = Duration::from_secs(10 * 60);

/// Tier 4 폴링 간격: 15분 (멀리 있으면 느리게)
pub const TIER4_INTERVAL: Duration = Duration::from_secs(15 * 60);

// ─────────────────────────────────────────────────────────────────────────────
// 예정 없음 / 전체 갱신 간격
// ─────────────────────────────────────────────────────────────────────────────

/// 예정이 없거나 시작 시간 불명 시 기본 폴링 간격: 5분
pub const NO_UPCOMING_INTERVAL: Duration = Duration::from_secs(5 * 60);

/// Tier 무시 전체 채널 강제 체크 주기: 5분
pub const FULL_REFRESH_INTERVAL: Duration = Duration::from_secs(5 * 60);

/// 알림 발송 후 고빈도 폴링 유지 시간: 15분
/// 이 기간 내에는 no-upcoming이어도 Tier1 간격으로 폴링하여 일정 변경 즉시 감지
pub const RECENTLY_NOTIFIED_WINDOW: Duration = Duration::from_secs(15 * 60);

/// 예정 알림 직후 동일 이벤트 catch-up 억제 구간: 15분
pub const LIVE_CATCHUP_SUPPRESS_WINDOW: Duration = Duration::from_secs(15 * 60);

// ─────────────────────────────────────────────────────────────────────────────
// 캐시 TTL (Go constants.go CacheTTL 대응)
// ─────────────────────────────────────────────────────────────────────────────

/// 알림 발송 기록 TTL: 24시간
pub const NOTIFICATION_SENT_TTL: Duration = Duration::from_secs(24 * 3_600);

/// 다음 방송 정보 캐시 TTL: 1시간
pub const NEXT_STREAM_INFO_TTL: Duration = Duration::from_secs(3_600);

/// Twitch 알림 발송 기록 TTL: 7일 (stream_id 기반)
pub const TWITCH_NOTIFICATION_TTL: Duration = Duration::from_secs(7 * 24 * 3_600);

// ─────────────────────────────────────────────────────────────────────────────
// 로컬 폴백 dedup (Valkey 장애 시)
// ─────────────────────────────────────────────────────────────────────────────

/// 로컬 폴백 dedup TTL: 10분
pub const LOCAL_FALLBACK_DEDUP_TTL: Duration = Duration::from_secs(10 * 60);

/// 로컬 dedup 맵 최대 키 수 (초과 시 만료된 항목 정리)
pub const LOCAL_FALLBACK_CLEANUP_MAX_KEYS: usize = 4096;

// ─────────────────────────────────────────────────────────────────────────────
// 기본 알림 대상 분 목록
// Go: buildTargetMinutes 기본값 = []int{5, 3, 1}
// ─────────────────────────────────────────────────────────────────────────────

/// 기본 알림 대상 분: 5분, 3분, 1분 전 (내림차순)
pub const DEFAULT_TARGET_MINUTES: &[i32] = &[5, 3, 1];

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tier_windows_ascending() {
        // Tier 구간은 오름차순이어야 함
        assert!(TIER1_WINDOW < TIER2_WINDOW);
        assert!(TIER2_WINDOW < TIER3_WINDOW);
    }

    #[test]
    fn tier1_window_is_45_min() {
        assert_eq!(TIER1_WINDOW.as_secs(), 45 * 60);
    }

    #[test]
    fn tier2_window_is_3_hours() {
        assert_eq!(TIER2_WINDOW.as_secs(), 3 * 3_600);
    }

    #[test]
    fn tier3_window_is_12_hours() {
        assert_eq!(TIER3_WINDOW.as_secs(), 12 * 3_600);
    }

    #[test]
    fn tier1_interval_is_1_min() {
        assert_eq!(TIER1_INTERVAL.as_secs(), 60);
    }

    #[test]
    fn tier2_interval_is_3_min() {
        assert_eq!(TIER2_INTERVAL.as_secs(), 3 * 60);
    }

    #[test]
    fn tier3_interval_is_10_min() {
        assert_eq!(TIER3_INTERVAL.as_secs(), 10 * 60);
    }

    #[test]
    fn tier4_interval_is_15_min() {
        assert_eq!(TIER4_INTERVAL.as_secs(), 15 * 60);
    }

    #[test]
    fn no_upcoming_interval_is_5_min() {
        assert_eq!(NO_UPCOMING_INTERVAL.as_secs(), 5 * 60);
    }

    #[test]
    fn full_refresh_interval_is_5_min() {
        assert_eq!(FULL_REFRESH_INTERVAL.as_secs(), 5 * 60);
    }

    #[test]
    fn recently_notified_window_is_15_min() {
        assert_eq!(RECENTLY_NOTIFIED_WINDOW.as_secs(), 15 * 60);
    }

    #[test]
    fn live_catchup_suppress_window_is_15_min() {
        assert_eq!(LIVE_CATCHUP_SUPPRESS_WINDOW.as_secs(), 15 * 60);
    }

    #[test]
    fn notification_sent_ttl_is_24h() {
        assert_eq!(NOTIFICATION_SENT_TTL.as_secs(), 24 * 3_600);
    }

    #[test]
    fn next_stream_info_ttl_is_1h() {
        assert_eq!(NEXT_STREAM_INFO_TTL.as_secs(), 3_600);
    }

    #[test]
    fn twitch_notification_ttl_is_7d() {
        assert_eq!(TWITCH_NOTIFICATION_TTL.as_secs(), 7 * 24 * 3_600);
    }

    #[test]
    fn local_fallback_dedup_ttl_is_10_min() {
        assert_eq!(LOCAL_FALLBACK_DEDUP_TTL.as_secs(), 10 * 60);
    }

    #[test]
    fn local_fallback_cleanup_max_keys() {
        assert_eq!(LOCAL_FALLBACK_CLEANUP_MAX_KEYS, 4096);
    }

    #[test]
    fn default_target_minutes_matches_go_fallback() {
        // Go buildTargetMinutes 기본값: []int{5, 3, 1}
        assert_eq!(DEFAULT_TARGET_MINUTES, &[5, 3, 1]);
    }
}
