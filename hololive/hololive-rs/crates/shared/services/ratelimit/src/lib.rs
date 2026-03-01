use std::{
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use serde::{Deserialize, Serialize};
use shared_core::error::SharedError;
use shared_infra::valkey::ValkeyClient;
use tracing::debug;

const DEFAULT_KEY_PREFIX: &str = "ratelimit:sliding";

#[derive(Debug, Clone)]
pub struct Decision {
    pub allowed: bool,
    pub current: i64,
    pub remaining: i64,
    pub limit: i64,
    pub window: Duration,
    pub retry_after: Duration,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
struct SlidingWindowState {
    timestamps_ms: Vec<i64>,
}

pub struct SlidingWindowLimiter {
    client: Arc<dyn ValkeyClient>,
    key_prefix: String,
}

impl SlidingWindowLimiter {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self {
            client,
            key_prefix: DEFAULT_KEY_PREFIX.to_string(),
        }
    }

    #[must_use]
    pub fn with_key_prefix(mut self, key_prefix: &str) -> Self {
        self.key_prefix = key_prefix.to_string();
        self
    }

    pub async fn allow(
        &self,
        bucket: &str,
        limit: i64,
        window: Duration,
    ) -> Result<Decision, SharedError> {
        if bucket.trim().is_empty() {
            return Err(SharedError::Config(
                "allow: bucket must not be empty".to_string(),
            ));
        }
        if limit <= 0 {
            return Err(SharedError::Config(
                "allow: limit must be greater than zero".to_string(),
            ));
        }
        if window.is_zero() {
            return Err(SharedError::Config(
                "allow: window must be greater than zero".to_string(),
            ));
        }

        let key = format!("{}:{}", self.key_prefix, bucket);
        let now_ms = now_millis();
        let window_ms = i64::try_from(window.as_millis()).unwrap_or(i64::MAX);
        let cutoff = now_ms.saturating_sub(window_ms);

        let mut state = self.load_state(&key).await?;
        state.timestamps_ms.retain(|timestamp| *timestamp > cutoff);

        let allowed = i64::try_from(state.timestamps_ms.len()).unwrap_or(i64::MAX) < limit;
        if allowed {
            state.timestamps_ms.push(now_ms);
            state.timestamps_ms.sort_unstable();
        }

        self.save_state(&key, &state, window).await?;

        let current = i64::try_from(state.timestamps_ms.len()).unwrap_or(i64::MAX);
        let remaining = (limit - current).max(0);
        let retry_after = if allowed {
            Duration::ZERO
        } else {
            compute_retry_after(&state.timestamps_ms, now_ms, window_ms)
        };

        if !allowed {
            debug!(
                bucket,
                limit,
                current,
                retry_after_ms = retry_after.as_millis(),
                "rate limit denied"
            );
        }

        Ok(Decision {
            allowed,
            current,
            remaining,
            limit,
            window,
            retry_after,
        })
    }

    async fn load_state(&self, key: &str) -> Result<SlidingWindowState, SharedError> {
        let Some(payload) = self.client.get(key).await? else {
            return Ok(SlidingWindowState::default());
        };

        match serde_json::from_str::<SlidingWindowState>(&payload) {
            Ok(state) => Ok(state),
            Err(error) => {
                debug!(key, error = %error, "invalid rate limit state; resetting");
                Ok(SlidingWindowState::default())
            }
        }
    }

    async fn save_state(
        &self,
        key: &str,
        state: &SlidingWindowState,
        window: Duration,
    ) -> Result<(), SharedError> {
        let payload = serde_json::to_string(state)?;
        let ttl = window.saturating_add(Duration::from_secs(1));
        self.client.set(key, &payload, Some(ttl)).await
    }
}

fn now_millis() -> i64 {
    let elapsed = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or(Duration::ZERO);

    i64::try_from(elapsed.as_millis()).unwrap_or(i64::MAX)
}

fn compute_retry_after(timestamps_ms: &[i64], now_ms: i64, window_ms: i64) -> Duration {
    let Some(oldest) = timestamps_ms.iter().min() else {
        return Duration::ZERO;
    };

    let retry_ms = window_ms.saturating_sub(now_ms.saturating_sub(*oldest));
    let positive_retry = u64::try_from(retry_ms).unwrap_or(0);
    Duration::from_millis(positive_retry)
}
