use chrono::{DateTime, Duration, DurationRound, Utc};
use sha2::{Digest, Sha256};

use crate::model::AlarmType;

// ─────────────────────────────────────────────────────────────────────────────
// 알람 키 상수 (Go alarm_types.go 1:1 대응, 바이트 호환 필수)
// ─────────────────────────────────────────────────────────────────────────────

pub const ALARM_KEY_PREFIX: &str = "alarm:";
pub const ALARM_REGISTRY_KEY: &str = "alarm:registry";
pub const ALARM_CHANNEL_REGISTRY_KEY: &str = "alarm:channel_registry";
pub const CHANNEL_SUBSCRIBERS_KEY_PREFIX: &str = "alarm:channel_subscribers:";
pub const MEMBER_NAME_KEY: &str = "alarm:member_names";
pub const ROOM_NAMES_CACHE_KEY: &str = "alarm:room_names";
pub const USER_NAMES_CACHE_KEY: &str = "alarm:user_names";
pub const NOTIFIED_KEY_PREFIX: &str = "notified:";
pub const NOTIFY_CLAIM_KEY_PREFIX: &str = "notified:claim:";
pub const NOTIFY_LOGICAL_CLAIM_KEY_PREFIX: &str = "notified:claim:event:";
pub const UPCOMING_EVENT_KEY_PREFIX: &str = "notified:upcoming:event:";
pub const SCHEDULE_TRANSITION_KEY_PREFIX: &str = "notified:schedule:transition:";
pub const NEXT_STREAM_KEY_PREFIX: &str = "alarm:next_stream:";
pub const CHANNEL_SUBSCRIBERS_COMMUNITY_PREFIX: &str = "alarm:channel_subscribers:COMMUNITY:";
pub const CHANNEL_SUBSCRIBERS_SHORTS_PREFIX: &str = "alarm:channel_subscribers:SHORTS:";
pub const CHZZK_LIVE_NOTIFIED_KEY_PREFIX: &str = "notified:chzzk:live:";
pub const INTEGRATED_NOTIFIED_KEY_PREFIX: &str = "notified:integrated:";
pub const TWITCH_LIVE_NOTIFIED_KEY_PREFIX: &str = "notified:twitch:live:";

// ─────────────────────────────────────────────────────────────────────────────
// 단순 키 빌더 (Go alarm_keys.go 대응)
// ─────────────────────────────────────────────────────────────────────────────

/// room_id 기반 알람 해시 키 생성 → "alarm:{room_id}"
pub fn alarm_key(room_id: &str) -> String {
    format!("{ALARM_KEY_PREFIX}{room_id}")
}

/// 채널 구독자 set 키 (기본 LIVE 타입)
pub fn channel_subscribers_key(channel_id: &str) -> String {
    format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}{channel_id}")
}

/// 알람 타입별 채널 구독자 set 키
/// Community/Shorts는 전용 프리픽스, Live는 기본 프리픽스
pub fn channel_subscribers_key_by_type(channel_id: &str, alarm_type: AlarmType) -> String {
    match alarm_type {
        AlarmType::Community => format!("{CHANNEL_SUBSCRIBERS_COMMUNITY_PREFIX}{channel_id}"),
        AlarmType::Shorts => format!("{CHANNEL_SUBSCRIBERS_SHORTS_PREFIX}{channel_id}"),
        AlarmType::Live => format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}{channel_id}"),
    }
}

/// Chzzk 라이브 알림 dedup 키 (10분 버킷 truncation)
/// 형식: "notified:chzzk:live:{chzzk_channel_id}:{YYYYMMDDTHHmm}"
/// Go 포맷 "20060102T1504" = chrono "%Y%m%dT%H%M"
pub fn chzzk_live_notified_key(chzzk_channel_id: &str, detected_at: DateTime<Utc>) -> String {
    let bucket = truncate_to_10min(detected_at);
    let ts = bucket.format("%Y%m%dT%H%M");
    format!("{CHZZK_LIVE_NOTIFIED_KEY_PREFIX}{chzzk_channel_id}:{ts}")
}

/// 통합(YouTube+Chzzk) 알림 dedup 키 (1분 버킷 truncation)
/// 형식: "notified:integrated:{youtube_channel_id}:{YYYYMMDDTHHmm}"
pub fn integrated_notified_key(youtube_channel_id: &str, scheduled_at: DateTime<Utc>) -> String {
    let bucket = truncate_to_1min(scheduled_at);
    let ts = bucket.format("%Y%m%dT%H%M");
    format!("{INTEGRATED_NOTIFIED_KEY_PREFIX}{youtube_channel_id}:{ts}")
}

/// 방송(stream_id) 기반 알림 이력 키 → "notified:{stream_id}"
pub fn notified_key(stream_id: &str) -> String {
    format!("{NOTIFIED_KEY_PREFIX}{stream_id}")
}

/// 채널 기준 다음 방송 캐시 키 → "alarm:next_stream:{channel_id}"
pub fn next_stream_key(channel_id: &str) -> String {
    format!("{NEXT_STREAM_KEY_PREFIX}{channel_id}")
}

// ─────────────────────────────────────────────────────────────────────────────
// 복합 키 빌더 (Go alarm_cache.go buildXxx 대응)
// ─────────────────────────────────────────────────────────────────────────────

/// 예정 시각을 분 단위로 truncate (초/나노초 제거)
pub fn normalize_scheduled_minute(start_scheduled: DateTime<Utc>) -> DateTime<Utc> {
    truncate_to_1min(start_scheduled)
}

/// 제목 핑거프린트 생성 (SHA256 앞 8바이트 hex = 16문자)
/// 1. NormalizeKey(title) → 비어있으면 NormalizeKey(stream_id) → 비어있으면 "untitled"
/// 2. SHA256(정규화 문자열) → hex[:16]
pub fn build_title_fingerprint(title: &str, stream_id: &str) -> String {
    let normalized = normalize_key(title);
    let input = if !normalized.is_empty() {
        normalized
    } else {
        let fallback = normalize_key(stream_id);
        if !fallback.is_empty() {
            fallback
        } else {
            "untitled".to_string()
        }
    };

    let mut hasher = Sha256::new();
    hasher.update(input.as_bytes());
    let result = hasher.finalize();
    // 앞 8바이트(16 hex char)
    hex::encode(&result[..8])
}

/// 알림 claim 키 생성 (SETNX 기반 선점)
/// 형식: "notified:claim:{room_id}:{stream_id}:{schedule_unix}:{category}"
pub fn build_notify_claim_key(
    room_id: &str,
    stream_id: &str,
    start_scheduled: DateTime<Utc>,
    category: &str,
) -> String {
    let schedule_unix = normalize_scheduled_minute(start_scheduled).timestamp();
    format!("{NOTIFY_CLAIM_KEY_PREFIX}{room_id}:{stream_id}:{schedule_unix}:{category}")
}

/// 논리적 이벤트 claim 키 (stream_id 변경 대응)
/// 형식: "notified:claim:event:{room_id}:{channel_id}:{schedule_unix}:{title_fp}:{category}"
pub fn build_logical_event_claim_key(
    room_id: &str,
    channel_id: &str,
    stream_id: &str,
    title: &str,
    start_scheduled: DateTime<Utc>,
    category: &str,
) -> String {
    let schedule_unix = normalize_scheduled_minute(start_scheduled).timestamp();
    let title_fp = build_title_fingerprint(title, stream_id);
    format!(
        "{NOTIFY_LOGICAL_CLAIM_KEY_PREFIX}{room_id}:{channel_id}:{schedule_unix}:{title_fp}:{category}"
    )
}

/// 예정 알림 이벤트 키 (MarkUpcomingEventNotified용)
/// 형식: "notified:upcoming:event:{room_id}:{channel_id}:{schedule_unix}:{title_fp}"
pub fn build_upcoming_event_key(
    room_id: &str,
    channel_id: &str,
    stream_id: &str,
    title: &str,
    start_scheduled: DateTime<Utc>,
) -> String {
    let schedule_unix = normalize_scheduled_minute(start_scheduled).timestamp();
    let title_fp = build_title_fingerprint(title, stream_id);
    format!("{UPCOMING_EVENT_KEY_PREFIX}{room_id}:{channel_id}:{schedule_unix}:{title_fp}")
}

/// 일정 변경 전환 claim 키
/// 형식: "notified:schedule:transition:{stream_id}:{old_unix}:{new_unix}"
pub fn build_schedule_transition_key(
    stream_id: &str,
    old_minute: DateTime<Utc>,
    new_minute: DateTime<Utc>,
) -> String {
    let old_unix = normalize_scheduled_minute(old_minute).timestamp();
    let new_unix = normalize_scheduled_minute(new_minute).timestamp();
    format!("{SCHEDULE_TRANSITION_KEY_PREFIX}{stream_id}:{old_unix}:{new_unix}")
}

/// 알림 카테고리 문자열 생성 (dedup 키 구성 요소)
/// - minutesUntil == 0 → "live"
/// - targetMinutes에 포함 → "target"
/// - 그 외 → minutesUntil을 문자열로 변환
pub fn notification_category(target_minutes: &[i32], minutes_until: i32) -> String {
    if minutes_until == 0 {
        return "live".to_string();
    }
    if target_minutes.contains(&minutes_until) {
        return "target".to_string();
    }
    minutes_until.to_string()
}

// ─────────────────────────────────────────────────────────────────────────────
// 내부 헬퍼
// ─────────────────────────────────────────────────────────────────────────────

/// Go stringutil.NormalizeKey 포팅
/// strings.ToLower + strings.TrimSpace → 특수문자(공백, -, _, ., !, ☆, ・, '', ', ー, —) 제거
fn normalize_key(s: &str) -> String {
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
                    | '\u{2018}' // '  left single quotation
                    | '\u{2019}' // '  right single quotation
                    | '\''
                    | 'ー'
                    | '—'
            )
        })
        .collect()
}

/// DateTime을 10분 단위로 truncate
fn truncate_to_10min(dt: DateTime<Utc>) -> DateTime<Utc> {
    dt.duration_trunc(Duration::minutes(10)).unwrap_or(dt)
}

/// DateTime을 1분 단위로 truncate
fn truncate_to_1min(dt: DateTime<Utc>) -> DateTime<Utc> {
    dt.duration_trunc(Duration::minutes(1)).unwrap_or(dt)
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;

    use super::*;

    // ─────────────────────────────────────────────────────────────────────────
    // 단순 키 빌더 테스트
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn alarm_key_basic() {
        assert_eq!(alarm_key("room123"), "alarm:room123");
    }

    #[test]
    fn channel_subscribers_key_basic() {
        assert_eq!(
            channel_subscribers_key("UCtest"),
            "alarm:channel_subscribers:UCtest"
        );
    }

    #[test]
    fn channel_subscribers_key_by_type_live() {
        assert_eq!(
            channel_subscribers_key_by_type("UCtest", AlarmType::Live),
            "alarm:channel_subscribers:UCtest"
        );
    }

    #[test]
    fn channel_subscribers_key_by_type_community() {
        assert_eq!(
            channel_subscribers_key_by_type("UCtest", AlarmType::Community),
            "alarm:channel_subscribers:COMMUNITY:UCtest"
        );
    }

    #[test]
    fn channel_subscribers_key_by_type_shorts() {
        assert_eq!(
            channel_subscribers_key_by_type("UCtest", AlarmType::Shorts),
            "alarm:channel_subscribers:SHORTS:UCtest"
        );
    }

    #[test]
    fn notified_key_basic() {
        assert_eq!(notified_key("vid001"), "notified:vid001");
    }

    #[test]
    fn next_stream_key_basic() {
        assert_eq!(next_stream_key("UCtest"), "alarm:next_stream:UCtest");
    }

    // ─────────────────────────────────────────────────────────────────────────
    // chzzk_live_notified_key: 10분 버킷
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn chzzk_live_notified_key_exact_10min() {
        // 2026-02-23T01:10:00Z → 버킷: 20260223T0110
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 1, 10, 0).unwrap();
        let key = chzzk_live_notified_key("ch_abc", dt);
        assert_eq!(key, "notified:chzzk:live:ch_abc:20260223T0110");
    }

    #[test]
    fn chzzk_live_notified_key_truncates_to_10min() {
        // 01:17:45 → 버킷: 01:10 (10분 truncate)
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 1, 17, 45).unwrap();
        let key = chzzk_live_notified_key("ch_abc", dt);
        assert_eq!(key, "notified:chzzk:live:ch_abc:20260223T0110");
    }

    #[test]
    fn chzzk_live_notified_key_on_the_hour() {
        // 자정 → 20260223T0000
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 0, 0, 0).unwrap();
        let key = chzzk_live_notified_key("ch_xyz", dt);
        assert_eq!(key, "notified:chzzk:live:ch_xyz:20260223T0000");
    }

    #[test]
    fn chzzk_live_notified_key_off_by_1min() {
        // 00:09:59 → 버킷: 00:00
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 0, 9, 59).unwrap();
        let key = chzzk_live_notified_key("ch_xyz", dt);
        assert_eq!(key, "notified:chzzk:live:ch_xyz:20260223T0000");
    }

    // ─────────────────────────────────────────────────────────────────────────
    // integrated_notified_key: 1분 버킷
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn integrated_notified_key_truncates_to_1min() {
        // 15:30:45 → 버킷: 15:30
        let dt = Utc.with_ymd_and_hms(2026, 1, 1, 15, 30, 45).unwrap();
        let key = integrated_notified_key("UCchannel", dt);
        assert_eq!(key, "notified:integrated:UCchannel:20260101T1530");
    }

    #[test]
    fn integrated_notified_key_exact_minute() {
        let dt = Utc.with_ymd_and_hms(2026, 1, 1, 15, 30, 0).unwrap();
        let key = integrated_notified_key("UCchannel", dt);
        assert_eq!(key, "notified:integrated:UCchannel:20260101T1530");
    }

    // ─────────────────────────────────────────────────────────────────────────
    // normalize_scheduled_minute
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn normalize_scheduled_minute_strips_seconds() {
        let dt = Utc.with_ymd_and_hms(2026, 3, 1, 9, 30, 45).unwrap();
        let norm = normalize_scheduled_minute(dt);
        assert_eq!(norm, Utc.with_ymd_and_hms(2026, 3, 1, 9, 30, 0).unwrap());
    }

    #[test]
    fn normalize_scheduled_minute_on_exact_minute() {
        let dt = Utc.with_ymd_and_hms(2026, 3, 1, 9, 30, 0).unwrap();
        let norm = normalize_scheduled_minute(dt);
        assert_eq!(norm, dt);
    }

    // ─────────────────────────────────────────────────────────────────────────
    // build_title_fingerprint: SHA256 앞 8바이트 hex
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn build_title_fingerprint_normal_title() {
        // "Hello World" → normalize_key → "helloworld" → sha256 → hex[:16]
        let fp = build_title_fingerprint("Hello World", "vid001");
        assert_eq!(fp.len(), 16, "핑거프린트는 16문자 hex여야 함");
        // 결정론적 검증: sha256("helloworld") 앞 8바이트
        let mut h = Sha256::new();
        h.update(b"helloworld");
        let expected = hex::encode(&h.finalize()[..8]);
        assert_eq!(fp, expected);
    }

    #[test]
    fn build_title_fingerprint_empty_title_fallback_to_stream_id() {
        // 빈 제목 → stream_id 폴백
        let fp = build_title_fingerprint("", "myVideoId");
        let mut h = Sha256::new();
        h.update(b"myvideoid"); // normalize_key("myVideoId") = "myvideoid"
        let expected = hex::encode(&h.finalize()[..8]);
        assert_eq!(fp, expected);
    }

    #[test]
    fn build_title_fingerprint_both_empty_fallback_untitled() {
        // 둘 다 비어있으면 "untitled"
        let fp = build_title_fingerprint("", "");
        let mut h = Sha256::new();
        h.update(b"untitled");
        let expected = hex::encode(&h.finalize()[..8]);
        assert_eq!(fp, expected);
    }

    #[test]
    fn build_title_fingerprint_unicode_title() {
        // 한국어 제목 정규화 후 해시
        let fp = build_title_fingerprint("테스트 방송!", "vid");
        assert_eq!(fp.len(), 16);
        // normalize_key("테스트 방송!") = "테스트방송" (공백·! 제거, 소문자)
        let mut h = Sha256::new();
        h.update("테스트방송".as_bytes());
        let expected = hex::encode(&h.finalize()[..8]);
        assert_eq!(fp, expected);
    }

    // ─────────────────────────────────────────────────────────────────────────
    // build_notify_claim_key
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn build_notify_claim_key_format() {
        // UTC 시각: 2026-02-23T12:05:30Z → 분 truncate → 12:05:00 → unix
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 12, 5, 30).unwrap();
        let expected_unix = Utc
            .with_ymd_and_hms(2026, 2, 23, 12, 5, 0)
            .unwrap()
            .timestamp();
        let key = build_notify_claim_key("room1", "vid1", dt, "target");
        assert_eq!(
            key,
            format!("notified:claim:room1:vid1:{expected_unix}:target")
        );
    }

    #[test]
    fn build_notify_claim_key_live_category() {
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 0, 0, 0).unwrap();
        let unix = dt.timestamp();
        let key = build_notify_claim_key("roomA", "vidB", dt, "live");
        assert_eq!(key, format!("notified:claim:roomA:vidB:{unix}:live"));
    }

    // ─────────────────────────────────────────────────────────────────────────
    // build_schedule_transition_key
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn build_schedule_transition_key_format() {
        let old = Utc.with_ymd_and_hms(2026, 2, 23, 12, 0, 0).unwrap();
        let new = Utc.with_ymd_and_hms(2026, 2, 23, 13, 0, 0).unwrap();
        let key = build_schedule_transition_key("vid1", old, new);
        let old_unix = old.timestamp();
        let new_unix = new.timestamp();
        assert_eq!(
            key,
            format!("notified:schedule:transition:vid1:{old_unix}:{new_unix}")
        );
    }

    // ─────────────────────────────────────────────────────────────────────────
    // notification_category
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn notification_category_zero_is_live() {
        assert_eq!(notification_category(&[5, 3, 1], 0), "live");
    }

    #[test]
    fn notification_category_target_minute() {
        assert_eq!(notification_category(&[5, 3, 1], 5), "target");
        assert_eq!(notification_category(&[5, 3, 1], 3), "target");
        assert_eq!(notification_category(&[5, 3, 1], 1), "target");
    }

    #[test]
    fn notification_category_non_target() {
        assert_eq!(notification_category(&[5, 3, 1], 10), "10");
        assert_eq!(notification_category(&[5, 3, 1], 2), "2");
    }

    // ─────────────────────────────────────────────────────────────────────────
    // KST 자정 경계 (UTC 15:00 전날) — 버킷 경계 확인
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn chzzk_key_kst_midnight_boundary() {
        // KST 자정 = UTC 전날 15:00
        // 2026-02-24T00:00:00 KST = 2026-02-23T15:00:00 UTC
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 15, 0, 0).unwrap();
        let key = chzzk_live_notified_key("ch", dt);
        // 버킷: 15:00 (10분 truncate → 15:00 그대로)
        assert_eq!(key, "notified:chzzk:live:ch:20260223T1500");
    }

    #[test]
    fn chzzk_key_kst_midnight_minus_1min() {
        // 14:59:59 UTC → 버킷: 14:50
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 14, 59, 59).unwrap();
        let key = chzzk_live_notified_key("ch", dt);
        assert_eq!(key, "notified:chzzk:live:ch:20260223T1450");
    }

    // ─────────────────────────────────────────────────────────────────────────
    // build_logical_event_claim_key
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn build_logical_event_claim_key_format() {
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 12, 0, 0).unwrap();
        let unix = dt.timestamp();
        let fp = build_title_fingerprint("My Stream", "vid1");
        let key =
            build_logical_event_claim_key("room1", "UCchan", "vid1", "My Stream", dt, "target");
        assert_eq!(
            key,
            format!("notified:claim:event:room1:UCchan:{unix}:{fp}:target")
        );
    }

    // ─────────────────────────────────────────────────────────────────────────
    // build_upcoming_event_key
    // ─────────────────────────────────────────────────────────────────────────

    #[test]
    fn build_upcoming_event_key_format() {
        let dt = Utc.with_ymd_and_hms(2026, 2, 23, 12, 0, 0).unwrap();
        let unix = dt.timestamp();
        let fp = build_title_fingerprint("Upcoming Show", "vid2");
        let key = build_upcoming_event_key("room2", "UCchan2", "vid2", "Upcoming Show", dt);
        assert_eq!(
            key,
            format!("notified:upcoming:event:room2:UCchan2:{unix}:{fp}")
        );
    }
}
