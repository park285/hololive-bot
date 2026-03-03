//! Valkey(=Redis) 키 빌더 모음.
//!
//! 기존 `keys.rs`(단일 파일)를 서브모듈로 분할하되, **기존 public API는 `pub use`로 유지**한다.

pub mod alarm;
pub mod channel;
pub mod dedup;
pub mod helpers;

/// 문자열 기반 Valkey 키로 변환 가능한 타입을 표현한다.
///
/// - 현재는 함수 기반 키 빌더가 주이지만, 도메인 타입 기반 키를 도입할 때를 대비해 제공한다.
pub trait ValkeyKey {
    fn valkey_key(&self) -> String;
}

// public API 유지: 기존 `shared_core::keys::*` 호출부가 그대로 동작하도록 전부 re-export
pub use alarm::*;
pub use channel::*;
pub use dedup::*;
pub use helpers::*;

#[cfg(test)]
#[allow(clippy::expect_used, clippy::panic, clippy::unwrap_used)]
mod tests {
    use std::{fs, path::Path};

    use chrono::{DateTime, Utc};
    use serde::Deserialize;

    use super::*;
    use crate::model::alarm::AlarmType;

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
