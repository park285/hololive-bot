//! 알람 관련 Valkey 키 상수/빌더.

/// `alarm:{room_id}`
pub const ALARM_KEY_PREFIX: &str = "alarm:";
pub const ALARM_REGISTRY_KEY: &str = "alarm:registry";
pub const ALARM_CHANNEL_REGISTRY_KEY: &str = "alarm:channel_registry";
pub const MEMBER_NAME_KEY: &str = "alarm:member_names";
pub const ROOM_NAMES_CACHE_KEY: &str = "alarm:room_names";
pub const USER_NAMES_CACHE_KEY: &str = "alarm:user_names";

pub const NEXT_STREAM_KEY_PREFIX: &str = "alarm:next_stream:";

pub const ALARM_DISPATCH_QUEUE_KEY: &str = "alarm:dispatch:queue";
pub const ALARM_QUEUE_ENVELOPE_VERSION_V1: u8 = 1;

pub fn alarm_key(room_id: &str) -> String {
    format!("{ALARM_KEY_PREFIX}{room_id}")
}

pub fn next_stream_key(channel_id: &str) -> String {
    format!("{NEXT_STREAM_KEY_PREFIX}{channel_id}")
}
