use std::sync::Arc;

use async_trait::async_trait;
use shared_core::{error::SharedError, model::alarm::AlarmQueueEnvelope};
use shared_infra::valkey::ValkeyClient;

use crate::{DEFAULT_QUEUE_KEY, QueueDispatcher};

pub struct ValkeyQueueDispatcher {
    client: Arc<dyn ValkeyClient>,
    queue_key: String,
}

impl ValkeyQueueDispatcher {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self {
            client,
            queue_key: DEFAULT_QUEUE_KEY.to_string(),
        }
    }

    #[must_use]
    pub fn with_queue_key(mut self, queue_key: &str) -> Self {
        self.queue_key = queue_key.to_string();
        self
    }
}

#[async_trait]
impl QueueDispatcher for ValkeyQueueDispatcher {
    async fn dispatch(&self, envelope: &AlarmQueueEnvelope) -> Result<(), SharedError> {
        let payload = serde_json::to_string(envelope)?;
        self.client.lpush(&self.queue_key, &payload).await
    }
}
