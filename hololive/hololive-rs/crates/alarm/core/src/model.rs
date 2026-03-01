use serde::{Deserialize, Serialize};
pub use shared_core::model::{
    alarm::{AlarmNotification, AlarmQueueEnvelope, AlarmType, NotifiedData},
    channel::Channel,
    stream::{Stream, StreamStatus},
};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpcomingEventNotifiedData {
    pub notified_at: String,
}
