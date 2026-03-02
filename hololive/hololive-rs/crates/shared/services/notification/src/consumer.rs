use std::sync::Arc;

use async_trait::async_trait;
use shared_core::{error::SharedError, model::alarm::AlarmQueueEnvelope};
use shared_infra::valkey::ValkeyClient;
use tracing::warn;

use crate::{CLAIM_KEY_PREFIX, DEFAULT_QUEUE_KEY, QueueConsumer};

pub struct ValkeyQueueConsumer {
    client: Arc<dyn ValkeyClient>,
    queue_key: String,
    block_timeout: f64,
    drain_timeout: f64,
    max_batch: usize,
}

impl ValkeyQueueConsumer {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self {
            client,
            queue_key: DEFAULT_QUEUE_KEY.to_owned(),
            // fred 기본 커맨드 타임아웃(약 5s)보다 짧게 두어 BRPOP 타임아웃이
            // 네트워크 오류로 오인되지 않도록 한다.
            block_timeout: 1.0,
            drain_timeout: 0.05,
            max_batch: 50,
        }
    }

    #[must_use]
    pub fn with_queue_key(mut self, queue_key: &str) -> Self {
        queue_key.clone_into(&mut self.queue_key);
        self
    }

    fn parse_envelope(raw: &str) -> Option<AlarmQueueEnvelope> {
        match serde_json::from_str::<AlarmQueueEnvelope>(raw) {
            Ok(envelope) if envelope.version == 0 || envelope.version == 1 => Some(envelope),
            Ok(envelope) => {
                warn!(
                    version = envelope.version,
                    "unsupported alarm queue envelope version"
                );
                None
            }
            Err(error) => {
                warn!(error = %error, "failed to parse alarm queue envelope");
                None
            }
        }
    }
}

#[async_trait]
impl QueueConsumer for ValkeyQueueConsumer {
    async fn drain_batch(&self, max_items: usize) -> Result<Vec<AlarmQueueEnvelope>, SharedError> {
        let limit = max_items.min(self.max_batch).max(1);
        let mut envelopes = Vec::with_capacity(limit);

        let first = self
            .client
            .brpop(&self.queue_key, self.block_timeout)
            .await?;
        let Some(first_raw) = first else {
            return Ok(envelopes);
        };

        if let Some(envelope) = Self::parse_envelope(&first_raw) {
            envelopes.push(envelope);
        }

        while envelopes.len() < limit {
            let raw = self
                .client
                .brpop(&self.queue_key, self.drain_timeout)
                .await?;
            let Some(payload) = raw else {
                break;
            };

            if let Some(envelope) = Self::parse_envelope(&payload) {
                envelopes.push(envelope);
            }
        }

        Ok(envelopes)
    }

    async fn release_claim_keys(&self, keys: &[String]) -> Result<(), SharedError> {
        let filtered: Vec<String> = keys
            .iter()
            .map(|key| key.trim())
            .filter(|key| !key.is_empty() && key.starts_with(CLAIM_KEY_PREFIX))
            .map(ToOwned::to_owned)
            .collect();

        if filtered.is_empty() {
            return Ok(());
        }

        let references: Vec<&str> = filtered.iter().map(String::as_str).collect();
        self.client.del(&references).await
    }
}
