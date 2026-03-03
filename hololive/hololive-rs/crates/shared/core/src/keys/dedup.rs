//! 알림 중복 방지(dedup) 및 claim 관련 Valkey 키 상수/빌더.

use chrono::{DateTime, Utc};
use sha2::{Digest, Sha256};

use super::helpers::{
    normalize_key, normalize_scheduled_minute, truncate_to_1min, truncate_to_10min,
};

pub const NOTIFIED_KEY_PREFIX: &str = "notified:";
pub const NOTIFY_CLAIM_KEY_PREFIX: &str = "notified:claim:";
pub const NOTIFY_LOGICAL_CLAIM_KEY_PREFIX: &str = "notified:claim:event:";
pub const UPCOMING_EVENT_KEY_PREFIX: &str = "notified:upcoming:event:";
pub const SCHEDULE_TRANSITION_KEY_PREFIX: &str = "notified:schedule:transition:";

pub const CHZZK_LIVE_NOTIFIED_KEY_PREFIX: &str = "notified:chzzk:live:";
pub const INTEGRATED_NOTIFIED_KEY_PREFIX: &str = "notified:integrated:";
pub const TWITCH_LIVE_NOTIFIED_KEY_PREFIX: &str = "notified:twitch:live:";

pub fn chzzk_live_notified_key(chzzk_channel_id: &str, detected_at: DateTime<Utc>) -> String {
    let bucket = truncate_to_10min(detected_at);
    let ts = bucket.format("%Y%m%dT%H%M");
    format!("{CHZZK_LIVE_NOTIFIED_KEY_PREFIX}{chzzk_channel_id}:{ts}")
}

pub fn integrated_notified_key(youtube_channel_id: &str, scheduled_at: DateTime<Utc>) -> String {
    let bucket = truncate_to_1min(scheduled_at);
    let ts = bucket.format("%Y%m%dT%H%M");
    format!("{INTEGRATED_NOTIFIED_KEY_PREFIX}{youtube_channel_id}:{ts}")
}

pub fn notified_key(stream_id: &str) -> String {
    format!("{NOTIFIED_KEY_PREFIX}{stream_id}")
}

pub fn build_title_fingerprint(title: &str, stream_id: &str) -> String {
    let normalized = normalize_key(title);
    let input = if normalized.is_empty() {
        let fallback = normalize_key(stream_id);
        if fallback.is_empty() {
            "untitled".to_owned()
        } else {
            fallback
        }
    } else {
        normalized
    };

    let mut hasher = Sha256::new();
    hasher.update(input.as_bytes());
    let result = hasher.finalize();
    hex::encode(&result[..8])
}

pub fn build_notify_claim_key(
    room_id: &str,
    stream_id: &str,
    start_scheduled: DateTime<Utc>,
    category: &str,
) -> String {
    let schedule_unix = normalize_scheduled_minute(start_scheduled).timestamp();
    format!("{NOTIFY_CLAIM_KEY_PREFIX}{room_id}:{stream_id}:{schedule_unix}:{category}")
}

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

pub fn build_schedule_transition_key(
    stream_id: &str,
    old_minute: DateTime<Utc>,
    new_minute: DateTime<Utc>,
) -> String {
    let old_unix = normalize_scheduled_minute(old_minute).timestamp();
    let new_unix = normalize_scheduled_minute(new_minute).timestamp();
    format!("{SCHEDULE_TRANSITION_KEY_PREFIX}{stream_id}:{old_unix}:{new_unix}")
}

pub fn notification_category(target_minutes: &[i32], minutes_until: i32) -> String {
    if minutes_until == 0 {
        return "live".to_owned();
    }

    if target_minutes.contains(&minutes_until) {
        return "target".to_owned();
    }

    minutes_until.to_string()
}
