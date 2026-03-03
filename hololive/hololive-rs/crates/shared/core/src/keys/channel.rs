//! 채널(구독자) 관련 Valkey 키 상수/빌더.

use crate::model::alarm::AlarmType;

pub const CHANNEL_SUBSCRIBERS_KEY_PREFIX: &str = "alarm:channel_subscribers:";
pub const CHANNEL_SUBSCRIBERS_COMMUNITY_PREFIX: &str = "alarm:channel_subscribers:COMMUNITY:";
pub const CHANNEL_SUBSCRIBERS_SHORTS_PREFIX: &str = "alarm:channel_subscribers:SHORTS:";

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
