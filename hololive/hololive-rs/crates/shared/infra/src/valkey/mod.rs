#[cfg(any(test, feature = "test-support"))]
mod mock;

use std::{collections::HashMap, time::Duration};

use async_trait::async_trait;
use fred::prelude::*;
#[cfg(any(test, feature = "test-support"))]
pub use mock::MockValkeyClient;
use shared_core::error::SharedError;
use tracing::debug;
use url::Url;

#[async_trait]
pub trait ValkeyClient: Send + Sync {
    async fn get(&self, key: &str) -> Result<Option<String>, SharedError>;
    async fn set(&self, key: &str, value: &str, ttl: Option<Duration>) -> Result<(), SharedError>;
    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, SharedError>;
    async fn del(&self, keys: &[&str]) -> Result<(), SharedError>;
    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, SharedError>;
    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), SharedError>;
    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, SharedError>;
    async fn hmset(&self, key: &str, fields: &[(String, String)]) -> Result<(), SharedError>;
    async fn smembers(&self, key: &str) -> Result<Vec<String>, SharedError>;
    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, SharedError>;
    async fn expire(&self, key: &str, ttl: Duration) -> Result<(), SharedError>;
    async fn lpush(&self, key: &str, value: &str) -> Result<(), SharedError>;
    async fn ping(&self) -> Result<(), SharedError>;
    async fn brpop(&self, key: &str, timeout: f64) -> Result<Option<String>, SharedError>;
    async fn llen(&self, key: &str) -> Result<i64, SharedError>;
}

pub struct FredValkeyClient {
    client: Client,
}

impl FredValkeyClient {
    pub async fn new(url: &str) -> Result<Self, SharedError> {
        let config = Config::from_url(url)
            .map_err(|e| SharedError::Config(format!("parse valkey url: {e}")))?;

        let client = Client::new(config, None, None, None);
        client
            .init()
            .await
            .map_err(|e| SharedError::Valkey(format!("init valkey client: {e}")))?;

        debug!(endpoint = %redact_valkey_endpoint(url), "valkey connected");
        Ok(Self { client })
    }

    #[allow(clippy::needless_pass_by_value)]
    fn map_err(e: Error) -> SharedError {
        SharedError::Valkey(e.to_string())
    }
}

fn redact_valkey_endpoint(raw: &str) -> String {
    let Ok(parsed) = Url::parse(raw) else {
        return "<invalid-valkey-url>".to_string();
    };

    let scheme = parsed.scheme();
    if scheme == "redis+unix" {
        return format!("{scheme}://{}", parsed.path());
    }

    let host = parsed.host_str().unwrap_or("<unknown-host>");
    let mut endpoint = format!("{scheme}://{host}");
    if let Some(port) = parsed.port() {
        endpoint.push(':');
        endpoint.push_str(&port.to_string());
    }
    if parsed.path() != "/" {
        endpoint.push_str(parsed.path());
    }
    endpoint
}

#[async_trait]
impl ValkeyClient for FredValkeyClient {
    async fn get(&self, key: &str) -> Result<Option<String>, SharedError> {
        self.client.get(key).await.map_err(Self::map_err)
    }

    async fn set(&self, key: &str, value: &str, ttl: Option<Duration>) -> Result<(), SharedError> {
        let expiration = ttl
            .filter(|duration| !duration.is_zero())
            .map(|duration| Expiration::EX(i64::try_from(duration.as_secs()).unwrap_or(i64::MAX)));

        let _: () = self
            .client
            .set(key, value, expiration, None, false)
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, SharedError> {
        let expiration = Expiration::EX(i64::try_from(ttl.as_secs()).unwrap_or(i64::MAX));
        let result: Option<String> = self
            .client
            .set(key, value, Some(expiration), Some(SetOptions::NX), true)
            .await
            .map_err(Self::map_err)?;
        Ok(result.is_none())
    }

    async fn del(&self, keys: &[&str]) -> Result<(), SharedError> {
        if keys.is_empty() {
            return Ok(());
        }

        let _: u64 = self
            .client
            .del(keys.to_vec())
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, SharedError> {
        self.client.hget(key, field).await.map_err(Self::map_err)
    }

    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), SharedError> {
        let _: () = self
            .client
            .hset(key, (field, value))
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, SharedError> {
        self.client.hgetall(key).await.map_err(Self::map_err)
    }

    async fn hmset(&self, key: &str, fields: &[(String, String)]) -> Result<(), SharedError> {
        if fields.is_empty() {
            return Ok(());
        }

        let _: () = self
            .client
            .hset(key, fields.to_vec())
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn smembers(&self, key: &str) -> Result<Vec<String>, SharedError> {
        self.client.smembers(key).await.map_err(Self::map_err)
    }

    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, SharedError> {
        if keys.is_empty() {
            return Ok(Vec::new());
        }

        let pipeline = self.client.pipeline();
        for key in keys {
            let _: () = pipeline
                .smembers::<(), _>(key)
                .await
                .map_err(Self::map_err)?;
        }

        pipeline.all().await.map_err(Self::map_err)
    }

    async fn expire(&self, key: &str, ttl: Duration) -> Result<(), SharedError> {
        let _: bool = self
            .client
            .expire(key, i64::try_from(ttl.as_secs()).unwrap_or(i64::MAX), None)
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn lpush(&self, key: &str, value: &str) -> Result<(), SharedError> {
        let _: i64 = self.client.lpush(key, value).await.map_err(Self::map_err)?;
        Ok(())
    }

    async fn ping(&self) -> Result<(), SharedError> {
        let _: String = self.client.ping(None).await.map_err(Self::map_err)?;
        Ok(())
    }

    async fn brpop(&self, key: &str, timeout: f64) -> Result<Option<String>, SharedError> {
        let result: Option<(String, String)> = self
            .client
            .brpop(key, timeout)
            .await
            .map_err(Self::map_err)?;
        Ok(result.map(|(_, value)| value))
    }

    async fn llen(&self, key: &str) -> Result<i64, SharedError> {
        self.client.llen(key).await.map_err(Self::map_err)
    }
}
