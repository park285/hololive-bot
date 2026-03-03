mod consumer;
mod dispatcher;

use async_trait::async_trait;
pub use consumer::ValkeyQueueConsumer;
pub use dispatcher::ValkeyQueueDispatcher;
use shared_core::{error::SharedError, model::alarm::AlarmQueueEnvelope};

pub(crate) const DEFAULT_QUEUE_KEY: &str = "alarm:dispatch:queue";
pub(crate) const CLAIM_KEY_PREFIX: &str = "notified:claim:";

#[async_trait]
pub trait QueueConsumer: Send + Sync {
    async fn drain_batch(&self, max_items: usize) -> Result<Vec<AlarmQueueEnvelope>, SharedError>;
    async fn release_claim_keys(&self, keys: &[String]) -> Result<(), SharedError>;
}

#[async_trait]
pub trait QueueDispatcher: Send + Sync {
    async fn dispatch(&self, envelope: &AlarmQueueEnvelope) -> Result<(), SharedError>;
}
