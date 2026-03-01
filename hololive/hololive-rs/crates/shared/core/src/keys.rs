use chrono::{DateTime, Duration, DurationRound, Utc};
use sha2::{Digest, Sha256};

use crate::model::alarm::AlarmType;

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

pub fn alarm_key(room_id: &str) -> String {
    format!("{ALARM_KEY_PREFIX}{room_id}")
}

pub fn channel_subscribers_key(channel_id: &str) -> String {
    format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}{channel_id}")
}

pub fn channel_subscribers_key_by_type(channel_id: &str, alarm_type: AlarmType) -> String {
    match alarm_type {
        AlarmType::Community => format!("{CHANNEL_SUBSCRIBERS_COMMUNITY_PREFIX}{channel_id}"),
        AlarmType::Shorts => format!("{CHANNEL_SUBSCRIBERS_SHORTS_PREFIX}{channel_id}"),
        AlarmType::Live => format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}{channel_id}"),
    }
}

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

pub fn next_stream_key(channel_id: &str) -> String {
    format!("{NEXT_STREAM_KEY_PREFIX}{channel_id}")
}

pub fn normalize_scheduled_minute(start_scheduled: DateTime<Utc>) -> DateTime<Utc> {
    truncate_to_1min(start_scheduled)
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

#[cfg(test)]
#[allow(clippy::expect_used, clippy::panic, clippy::unwrap_used)]
mod tests {
    use std::{fs, path::Path};

    use chrono::{DateTime, Utc};
    use serde::Deserialize;

    use super::*;

    #[derive(Debug, Deserialize)]
    struct Fixture {
        alarm_key: Vec<RoomCase>,
        channel_subscribers_key: Vec<ChannelCase>,
        channel_subscribers_key_by_type: Vec<ChannelTypeCase>,
        notified_key: Vec<StreamCase>,
        next_stream_key: Vec<ChannelCase>,
        chzzk_live_notified_key: Vec<ChzzkCase>,
        integrated_notified_key: Vec<IntegratedCase>,
        build_notify_claim_key: Vec<NotifyClaimCase>,
        build_logical_event_claim_key: Vec<LogicalEventCase>,
        build_upcoming_event_key: Vec<UpcomingEventCase>,
        build_schedule_transition_key: Vec<ScheduleTransitionCase>,
        notification_category: Vec<CategoryCase>,
        build_title_fingerprint: Vec<TitleFingerprintCase>,
    }

    #[derive(Debug, Deserialize)]
    struct RoomCase {
        input: RoomInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct RoomInput {
        room_id: String,
    }

    #[derive(Debug, Deserialize)]
    struct ChannelCase {
        input: ChannelInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct ChannelInput {
        channel_id: String,
    }

    #[derive(Debug, Deserialize)]
    struct ChannelTypeCase {
        input: ChannelTypeInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct ChannelTypeInput {
        channel_id: String,
        alarm_type: String,
    }

    #[derive(Debug, Deserialize)]
    struct StreamCase {
        input: StreamInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct StreamInput {
        stream_id: String,
    }

    #[derive(Debug, Deserialize)]
    struct ChzzkCase {
        input: ChzzkInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct ChzzkInput {
        chzzk_channel_id: String,
        detected_at: String,
    }

    #[derive(Debug, Deserialize)]
    struct IntegratedCase {
        input: IntegratedInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct IntegratedInput {
        youtube_channel_id: String,
        scheduled_at: String,
    }

    #[derive(Debug, Deserialize)]
    struct NotifyClaimCase {
        input: NotifyClaimInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct NotifyClaimInput {
        room_id: String,
        stream_id: String,
        start_scheduled: String,
        category: String,
    }

    #[derive(Debug, Deserialize)]
    struct LogicalEventCase {
        input: LogicalEventInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct LogicalEventInput {
        room_id: String,
        channel_id: String,
        stream_id: String,
        title: String,
        start_scheduled: String,
        category: String,
    }

    #[derive(Debug, Deserialize)]
    struct UpcomingEventCase {
        input: UpcomingEventInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct UpcomingEventInput {
        room_id: String,
        channel_id: String,
        stream_id: String,
        title: String,
        start_scheduled: String,
    }

    #[derive(Debug, Deserialize)]
    struct ScheduleTransitionCase {
        input: ScheduleTransitionInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct ScheduleTransitionInput {
        stream_id: String,
        old_minute: String,
        new_minute: String,
    }

    #[derive(Debug, Deserialize)]
    struct CategoryCase {
        input: CategoryInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct CategoryInput {
        target_minutes: Vec<i32>,
        minutes_until: i32,
    }

    #[derive(Debug, Deserialize)]
    struct TitleFingerprintCase {
        input: TitleFingerprintInput,
        expected: String,
    }

    #[derive(Debug, Deserialize)]
    struct TitleFingerprintInput {
        title: String,
        stream_id: String,
    }

    #[test]
    fn fixture_key_builders_match() {
        let fixture = load_fixture();
        assert_basic_key_builders(&fixture);
        assert_notification_key_builders(&fixture);
        assert_claim_key_builders(&fixture);
        assert_other_key_builders(&fixture);
    }

    fn assert_basic_key_builders(fixture: &Fixture) {
        for case in &fixture.alarm_key {
            assert_eq!(alarm_key(&case.input.room_id), case.expected.as_str());
        }
        for case in &fixture.channel_subscribers_key {
            assert_eq!(
                channel_subscribers_key(&case.input.channel_id),
                case.expected.as_str()
            );
        }
        for case in &fixture.channel_subscribers_key_by_type {
            let alarm_type = parse_alarm_type(&case.input.alarm_type);
            assert_eq!(
                channel_subscribers_key_by_type(&case.input.channel_id, alarm_type),
                case.expected.as_str()
            );
        }
        for case in &fixture.notified_key {
            assert_eq!(notified_key(&case.input.stream_id), case.expected.as_str());
        }
        for case in &fixture.next_stream_key {
            assert_eq!(
                next_stream_key(&case.input.channel_id),
                case.expected.as_str()
            );
        }
    }

    fn assert_notification_key_builders(fixture: &Fixture) {
        for case in &fixture.chzzk_live_notified_key {
            let detected_at = parse_utc(&case.input.detected_at);
            assert_eq!(
                chzzk_live_notified_key(&case.input.chzzk_channel_id, detected_at),
                case.expected.as_str()
            );
        }
        for case in &fixture.integrated_notified_key {
            let scheduled_at = parse_utc(&case.input.scheduled_at);
            assert_eq!(
                integrated_notified_key(&case.input.youtube_channel_id, scheduled_at),
                case.expected.as_str()
            );
        }
        for case in &fixture.build_schedule_transition_key {
            let old_minute = parse_utc(&case.input.old_minute);
            let new_minute = parse_utc(&case.input.new_minute);
            assert_eq!(
                build_schedule_transition_key(&case.input.stream_id, old_minute, new_minute),
                case.expected.as_str()
            );
        }
    }

    fn assert_claim_key_builders(fixture: &Fixture) {
        for case in &fixture.build_notify_claim_key {
            let start_scheduled = parse_utc(&case.input.start_scheduled);
            assert_eq!(
                build_notify_claim_key(
                    &case.input.room_id,
                    &case.input.stream_id,
                    start_scheduled,
                    &case.input.category,
                ),
                case.expected.as_str()
            );
        }
        for case in &fixture.build_logical_event_claim_key {
            let start_scheduled = parse_utc(&case.input.start_scheduled);
            assert_eq!(
                build_logical_event_claim_key(
                    &case.input.room_id,
                    &case.input.channel_id,
                    &case.input.stream_id,
                    &case.input.title,
                    start_scheduled,
                    &case.input.category,
                ),
                case.expected.as_str()
            );
        }
        for case in &fixture.build_upcoming_event_key {
            let start_scheduled = parse_utc(&case.input.start_scheduled);
            assert_eq!(
                build_upcoming_event_key(
                    &case.input.room_id,
                    &case.input.channel_id,
                    &case.input.stream_id,
                    &case.input.title,
                    start_scheduled,
                ),
                case.expected.as_str()
            );
        }
    }

    fn assert_other_key_builders(fixture: &Fixture) {
        for case in &fixture.notification_category {
            assert_eq!(
                notification_category(&case.input.target_minutes, case.input.minutes_until),
                case.expected.as_str()
            );
        }
        for case in &fixture.build_title_fingerprint {
            assert_eq!(
                build_title_fingerprint(&case.input.title, &case.input.stream_id),
                case.expected.as_str()
            );
        }
    }

    fn load_fixture() -> Fixture {
        let fixture_path = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../../testdata/valkey_keys/expected_keys.json");
        let json = fs::read_to_string(fixture_path).expect("fixture file must exist");
        serde_json::from_str(&json).expect("fixture must be valid")
    }

    fn parse_alarm_type(value: &str) -> AlarmType {
        match value {
            "LIVE" => AlarmType::Live,
            "COMMUNITY" => AlarmType::Community,
            "SHORTS" => AlarmType::Shorts,
            _ => panic!("unknown alarm type in fixture: {value}"),
        }
    }

    fn parse_utc(value: &str) -> DateTime<Utc> {
        DateTime::parse_from_rfc3339(value)
            .expect("fixture timestamp must be RFC3339")
            .with_timezone(&Utc)
    }
}
