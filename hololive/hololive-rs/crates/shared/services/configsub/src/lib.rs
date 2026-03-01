use std::sync::Arc;

use serde::{Deserialize, Serialize};
use shared_core::error::SharedError;
use shared_infra::valkey::ValkeyClient;

pub const DEFAULT_CHANNEL: &str = "config:update";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConfigUpdate {
    pub update_type: String,
    pub payload: serde_json::Value,
}

pub struct Subscriber {
    client: Arc<dyn ValkeyClient>,
    channel: String,
    poll_timeout: f64,
}

impl Subscriber {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self {
            client,
            channel: DEFAULT_CHANNEL.to_string(),
            poll_timeout: 5.0,
        }
    }

    #[must_use]
    pub fn with_channel(mut self, channel: &str) -> Self {
        self.channel = channel.to_string();
        self
    }

    #[must_use]
    pub fn with_poll_timeout(mut self, poll_timeout: f64) -> Self {
        self.poll_timeout = poll_timeout;
        self
    }

    pub fn channel(&self) -> &str {
        &self.channel
    }

    // ValkeyClient가 Pub/Sub API를 노출하지 않아 BRPOP 기반 전송 채널로 구독을 구현한다.
    pub async fn next_update(&self) -> Result<Option<ConfigUpdate>, SharedError> {
        let message = self.client.brpop(&self.channel, self.poll_timeout).await?;
        let Some(raw) = message else {
            return Ok(None);
        };

        let update = serde_json::from_str::<ConfigUpdate>(&raw)?;
        Ok(Some(update))
    }

    pub async fn drain_batch(&self, max_items: usize) -> Result<Vec<ConfigUpdate>, SharedError> {
        let limit = max_items.max(1);
        let mut updates = Vec::with_capacity(limit);

        while updates.len() < limit {
            let timeout = if updates.is_empty() {
                self.poll_timeout
            } else {
                0.05
            };
            let message = self.client.brpop(&self.channel, timeout).await?;
            let Some(raw) = message else {
                break;
            };

            let update = serde_json::from_str::<ConfigUpdate>(&raw)?;
            updates.push(update);
        }

        Ok(updates)
    }
}
