use std::{collections::HashMap, sync::Arc, time::Duration};

use async_trait::async_trait;
use shared_core::error::SharedError;
use shared_infra::valkey::ValkeyClient;

#[async_trait]
pub trait CacheService: Send + Sync {
    async fn get(&self, key: &str) -> Result<Option<String>, SharedError>;
    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), SharedError>;
    async fn delete(&self, key: &str) -> Result<(), SharedError>;
    async fn hget(&self, hash_key: &str, field: &str) -> Result<Option<String>, SharedError>;
    async fn hset(&self, hash_key: &str, field: &str, value: &str) -> Result<(), SharedError>;
    async fn hget_all(&self, hash_key: &str) -> Result<HashMap<String, String>, SharedError>;
}

pub struct ValkeyCacheService {
    client: Arc<dyn ValkeyClient>,
}

impl ValkeyCacheService {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self { client }
    }
}

#[async_trait]
impl CacheService for ValkeyCacheService {
    async fn get(&self, key: &str) -> Result<Option<String>, SharedError> {
        self.client.get(key).await
    }

    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), SharedError> {
        self.client.set(key, value, Some(ttl)).await
    }

    async fn delete(&self, key: &str) -> Result<(), SharedError> {
        let keys = [key];
        self.client.del(&keys).await
    }

    async fn hget(&self, hash_key: &str, field: &str) -> Result<Option<String>, SharedError> {
        self.client.hget(hash_key, field).await
    }

    async fn hset(&self, hash_key: &str, field: &str, value: &str) -> Result<(), SharedError> {
        self.client.hset(hash_key, field, value).await
    }

    async fn hget_all(&self, hash_key: &str) -> Result<HashMap<String, String>, SharedError> {
        self.client.hget_all(hash_key).await
    }
}
