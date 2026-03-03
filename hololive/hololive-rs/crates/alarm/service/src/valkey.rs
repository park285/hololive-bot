use std::{collections::HashMap, time::Duration};

use async_trait::async_trait;
use shared_core::error::SharedError;

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

#[cfg(test)]
use std::time::Instant;

#[cfg(test)]
use dashmap::DashMap;

#[cfg(test)]
pub struct MockValkeyClient {
    store: DashMap<String, String>,
    hstore: DashMap<String, HashMap<String, String>>,
    pub sstore: DashMap<String, Vec<String>>,
    pub lstore: DashMap<String, Vec<String>>,
    expiry: DashMap<String, Instant>,
}

#[cfg(test)]
impl MockValkeyClient {
    pub fn new() -> Self {
        Self {
            store: DashMap::new(),
            hstore: DashMap::new(),
            sstore: DashMap::new(),
            lstore: DashMap::new(),
            expiry: DashMap::new(),
        }
    }

    fn set_expiry(&self, key: &str, ttl: Duration) {
        if ttl.is_zero() {
            self.expiry.remove(key);
            return;
        }

        self.expiry.insert(key.to_owned(), Instant::now() + ttl);
    }

    fn clear_expiry(&self, key: &str) {
        self.expiry.remove(key);
    }

    fn delete_key(&self, key: &str) {
        self.store.remove(key);
        self.hstore.remove(key);
        self.sstore.remove(key);
        self.lstore.remove(key);
        self.expiry.remove(key);
    }

    fn purge_if_expired(&self, key: &str) {
        let should_delete = self
            .expiry
            .get(key)
            .is_some_and(|deadline| Instant::now() >= *deadline);

        if should_delete {
            self.delete_key(key);
        }
    }

    fn key_exists(&self, key: &str) -> bool {
        self.purge_if_expired(key);
        self.store.contains_key(key)
            || self.hstore.contains_key(key)
            || self.sstore.contains_key(key)
            || self.lstore.contains_key(key)
    }
}

#[cfg(test)]
impl Default for MockValkeyClient {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
#[async_trait]
impl ValkeyClient for MockValkeyClient {
    async fn get(&self, key: &str) -> Result<Option<String>, SharedError> {
        self.purge_if_expired(key);
        Ok(self.store.get(key).map(|value| value.clone()))
    }

    async fn set(&self, key: &str, value: &str, ttl: Option<Duration>) -> Result<(), SharedError> {
        self.store.insert(key.to_owned(), value.to_owned());

        if let Some(duration) = ttl {
            self.set_expiry(key, duration);
        } else {
            self.clear_expiry(key);
        }

        Ok(())
    }

    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, SharedError> {
        if self.key_exists(key) {
            return Ok(false);
        }

        self.store.insert(key.to_owned(), value.to_owned());
        self.set_expiry(key, ttl);
        Ok(true)
    }

    async fn del(&self, keys: &[&str]) -> Result<(), SharedError> {
        for key in keys {
            self.delete_key(key);
        }
        Ok(())
    }

    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, SharedError> {
        self.purge_if_expired(key);
        Ok(self
            .hstore
            .get(key)
            .and_then(|hash| hash.get(field).cloned()))
    }

    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), SharedError> {
        self.purge_if_expired(key);
        let mut hash = self.hstore.entry(key.to_owned()).or_default();
        hash.insert(field.to_owned(), value.to_owned());
        Ok(())
    }

    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, SharedError> {
        self.purge_if_expired(key);
        Ok(self
            .hstore
            .get(key)
            .map(|hash| hash.clone())
            .unwrap_or_default())
    }

    async fn hmset(&self, key: &str, fields: &[(String, String)]) -> Result<(), SharedError> {
        self.purge_if_expired(key);
        let mut hash = self.hstore.entry(key.to_owned()).or_default();
        for (field, value) in fields {
            hash.insert(field.clone(), value.clone());
        }
        Ok(())
    }

    async fn smembers(&self, key: &str) -> Result<Vec<String>, SharedError> {
        self.purge_if_expired(key);
        Ok(self
            .sstore
            .get(key)
            .map(|items| items.clone())
            .unwrap_or_default())
    }

    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, SharedError> {
        let mut all_members = Vec::with_capacity(keys.len());
        for key in keys {
            self.purge_if_expired(key);
            all_members.push(
                self.sstore
                    .get(key)
                    .map(|items| items.clone())
                    .unwrap_or_default(),
            );
        }
        Ok(all_members)
    }

    async fn expire(&self, key: &str, ttl: Duration) -> Result<(), SharedError> {
        if self.key_exists(key) {
            self.set_expiry(key, ttl);
        }
        Ok(())
    }

    async fn lpush(&self, key: &str, value: &str) -> Result<(), SharedError> {
        self.purge_if_expired(key);
        let mut list = self.lstore.entry(key.to_owned()).or_default();
        list.insert(0, value.to_owned());
        Ok(())
    }

    async fn ping(&self) -> Result<(), SharedError> {
        Ok(())
    }

    async fn brpop(&self, key: &str, _timeout: f64) -> Result<Option<String>, SharedError> {
        self.purge_if_expired(key);

        let (value, remove_key) = if let Some(mut list) = self.lstore.get_mut(key) {
            let value = list.pop();
            let remove_key = list.is_empty();
            (value, remove_key)
        } else {
            (None, false)
        };

        if remove_key {
            self.lstore.remove(key);
            self.expiry.remove(key);
        }

        Ok(value)
    }

    async fn llen(&self, key: &str) -> Result<i64, SharedError> {
        self.purge_if_expired(key);
        Ok(self
            .lstore
            .get(key)
            .map_or(0, |list| i64::try_from(list.len()).unwrap_or(i64::MAX)))
    }
}
