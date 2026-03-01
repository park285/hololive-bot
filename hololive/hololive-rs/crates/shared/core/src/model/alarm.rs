use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::model::{channel::Channel, stream::Stream};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum AlarmType {
    #[serde(rename = "LIVE")]
    Live,
    #[serde(rename = "COMMUNITY")]
    Community,
    #[serde(rename = "SHORTS")]
    Shorts,
}

impl AlarmType {
    pub fn is_valid(&self) -> bool {
        true
    }

    pub fn is_valid_str(s: &str) -> bool {
        matches!(s, "LIVE" | "COMMUNITY" | "SHORTS")
    }

    pub fn display_name(&self) -> &'static str {
        match self {
            Self::Live => "방송",
            Self::Community => "커뮤니티",
            Self::Shorts => "쇼츠",
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Live => "LIVE",
            Self::Community => "COMMUNITY",
            Self::Shorts => "SHORTS",
        }
    }

    pub fn all() -> &'static [Self] {
        &[Self::Live, Self::Community, Self::Shorts]
    }
}

impl std::fmt::Display for AlarmType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

pub type AlarmTypes = Vec<AlarmType>;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Alarm {
    pub id: Option<i64>,
    pub room_id: String,
    pub user_id: String,
    pub channel_id: String,
    pub member_name: Option<String>,
    pub room_name: Option<String>,
    pub user_name: Option<String>,
    pub alarm_types: AlarmTypes,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NotifiedData {
    pub start_scheduled: String,
    pub sent_at: HashMap<i32, bool>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AlarmNotification {
    pub room_id: String,
    pub channel: Option<Channel>,
    pub stream: Option<Stream>,
    pub minutes_until: i32,
    pub users: Vec<String>,
    pub schedule_change_message: String,
}

impl AlarmNotification {
    pub fn new(
        room_id: String,
        channel: Option<Channel>,
        stream: Option<Stream>,
        minutes_until: i32,
        users: Vec<String>,
        schedule_change_message: String,
    ) -> Self {
        Self {
            room_id,
            channel,
            stream,
            minutes_until,
            users,
            schedule_change_message,
        }
    }

    pub fn user_count(&self) -> usize {
        self.users.len()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AlarmQueueEnvelope {
    pub notification: AlarmNotification,
    pub claim_keys: Vec<String>,
    pub enqueued_at: String,
    pub version: u8,
}

#[cfg(test)]
#[allow(clippy::expect_used, clippy::panic, clippy::unwrap_used)]
mod tests {
    use std::{fs, path::Path};

    use super::*;

    #[test]
    fn alarm_queue_envelope_roundtrip() {
        let fixture_path = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../../testdata/alarm_queue/alarm_queue_envelope.json");

        let json = fs::read_to_string(fixture_path).expect("fixture file must exist");
        let envelopes: Vec<AlarmQueueEnvelope> =
            serde_json::from_str(&json).expect("must parse fixture");

        let reserialized = serde_json::to_value(&envelopes).expect("must serialize fixture");
        let original: serde_json::Value = serde_json::from_str(&json).expect("must parse original");

        assert_eq!(reserialized, original);
    }
}
