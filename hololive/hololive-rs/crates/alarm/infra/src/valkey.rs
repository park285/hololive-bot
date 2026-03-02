use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
    time::{Duration, Instant},
};

use alarm_core::error::AlarmError;
use async_trait::async_trait;
use dashmap::DashMap;
use fred::prelude::*;
use tracing::debug;
use url::Url;

/// Valkey(Redis) 클라이언트 추상 인터페이스
#[async_trait]
pub trait ValkeyClient: Send + Sync {
    // ── 기본 키-값 ops ────────────────────────────────────────────────────────

    async fn get(&self, key: &str) -> Result<Option<String>, AlarmError>;
    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), AlarmError>;
    /// NX(없을 때만 저장) — 성공 시 true 반환
    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, AlarmError>;
    async fn del(&self, keys: &[&str]) -> Result<u64, AlarmError>;

    // ── Hash ops ──────────────────────────────────────────────────────────────

    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, AlarmError>;
    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), AlarmError>;
    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, AlarmError>;
    async fn hmset(&self, key: &str, fields: &HashMap<String, String>) -> Result<(), AlarmError>;

    // ── Set ops ───────────────────────────────────────────────────────────────

    async fn smembers(&self, key: &str) -> Result<Vec<String>, AlarmError>;
    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, AlarmError>;

    // ── TTL ───────────────────────────────────────────────────────────────────

    async fn expire(&self, key: &str, ttl: Duration) -> Result<bool, AlarmError>;

    // ── List ops ─────────────────────────────────────────────────────────────

    async fn lpush(&self, key: &str, value: &str) -> Result<i64, AlarmError>;

    // ── 헬스체크 ─────────────────────────────────────────────────────────────

    async fn ping(&self) -> Result<(), AlarmError>;
}

// ─────────────────────────────────────────────────────────────────────────────
// Fred 기반 실제 클라이언트 구현
// ─────────────────────────────────────────────────────────────────────────────

/// Fred 라이브러리를 사용하는 Valkey 클라이언트 구현체
/// TCP("redis://host:port")와 Unix 소켓("redis+unix:///path") URL을 모두 지원한다.
pub struct FredValkeyClient {
    client: Client,
}

impl FredValkeyClient {
    /// URL로부터 Fred 클라이언트를 초기화하고 연결한다.
    pub async fn new(url: &str) -> Result<Self, AlarmError> {
        let config = Config::from_url(url)
            .map_err(|e| AlarmError::Config(format!("Valkey URL 파싱 실패: {e}")))?;

        let client = Client::new(config, None, None, None);
        // 비동기 연결 초기화 — 실패 시 즉시 에러 반환
        client
            .init()
            .await
            .map_err(|e| AlarmError::Valkey(format!("Valkey 연결 실패: {e}")))?;

        debug!(endpoint = %redact_valkey_endpoint(url), "Valkey 연결 완료");
        Ok(Self { client })
    }

    /// fred 에러를 AlarmError로 변환
    fn map_err(e: fred::error::Error) -> AlarmError {
        AlarmError::Valkey(e.to_string())
    }
}

fn redact_valkey_endpoint(raw: &str) -> String {
    let Ok(parsed) = Url::parse(raw) else {
        return "<invalid-valkey-url>".to_owned();
    };

    let scheme = parsed.scheme();
    if scheme == "redis+unix" {
        // unix socket path 자체는 비밀정보가 아니므로 userinfo/query 제거 후 path만 노출
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
    async fn get(&self, key: &str) -> Result<Option<String>, AlarmError> {
        let val: Option<String> = self.client.get(key).await.map_err(Self::map_err)?;
        Ok(val)
    }

    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), AlarmError> {
        let secs = ttl.as_secs() as i64;
        let _: () = self
            .client
            .set(key, value, Some(Expiration::EX(secs)), None, false)
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, AlarmError> {
        let secs = ttl.as_secs() as i64;
        // SET key value NX EX secs
        let result: Option<String> = self
            .client
            .set(
                key,
                value,
                Some(Expiration::EX(secs)),
                Some(SetOptions::NX),
                true,
            )
            .await
            .map_err(Self::map_err)?;
        // GET 플래그 사용 시: 이전 값 반환, NX 성공이면 None 반환
        Ok(result.is_none())
    }

    async fn del(&self, keys: &[&str]) -> Result<u64, AlarmError> {
        if keys.is_empty() {
            return Ok(0);
        }
        let count: u64 = self
            .client
            .del(keys.to_vec())
            .await
            .map_err(Self::map_err)?;
        Ok(count)
    }

    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, AlarmError> {
        let val: Option<String> = self.client.hget(key, field).await.map_err(Self::map_err)?;
        Ok(val)
    }

    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), AlarmError> {
        let _: () = self
            .client
            .hset(key, (field, value))
            .await
            .map_err(Self::map_err)?;
        Ok(())
    }

    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, AlarmError> {
        let map: HashMap<String, String> = self.client.hgetall(key).await.map_err(Self::map_err)?;
        Ok(map)
    }

    async fn hmset(&self, key: &str, fields: &HashMap<String, String>) -> Result<(), AlarmError> {
        if fields.is_empty() {
            return Ok(());
        }
        let pairs: Vec<(String, String)> =
            fields.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        let _: () = self.client.hset(key, pairs).await.map_err(Self::map_err)?;
        Ok(())
    }

    async fn smembers(&self, key: &str) -> Result<Vec<String>, AlarmError> {
        let members: Vec<String> = self.client.smembers(key).await.map_err(Self::map_err)?;
        Ok(members)
    }

    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, AlarmError> {
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

        let results: Vec<Vec<String>> = pipeline.all().await.map_err(Self::map_err)?;
        Ok(results)
    }

    async fn expire(&self, key: &str, ttl: Duration) -> Result<bool, AlarmError> {
        let secs = ttl.as_secs() as i64;
        let result: bool = self
            .client
            .expire(key, secs, None)
            .await
            .map_err(Self::map_err)?;
        Ok(result)
    }

    async fn lpush(&self, key: &str, value: &str) -> Result<i64, AlarmError> {
        let count: i64 = self.client.lpush(key, value).await.map_err(Self::map_err)?;
        Ok(count)
    }

    async fn ping(&self) -> Result<(), AlarmError> {
        let _: String = self.client.ping(None).await.map_err(Self::map_err)?;
        Ok(())
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트용 인메모리 Mock 구현 (DashMap 기반)
// ─────────────────────────────────────────────────────────────────────────────

/// 키-값 저장소 항목 (TTL 만료 추적 포함)
struct MockEntry {
    value: String,
    expires_at: Option<Instant>,
}

impl MockEntry {
    fn is_expired(&self) -> bool {
        self.expires_at
            .map(|t| Instant::now() >= t)
            .unwrap_or(false)
    }
}

/// 테스트용 인메모리 Valkey 클라이언트
/// DashMap으로 단순 키-값 및 해시를 모킹하며, TTL 만료를 지원한다.
pub struct MockValkeyClient {
    // 일반 키-값 저장소
    store: Arc<DashMap<String, MockEntry>>,
    // 해시 저장소: key → (field → value)
    hstore: Arc<DashMap<String, DashMap<String, String>>>,
    // Set 저장소: key → [member, ...]
    sstore: Arc<DashMap<String, Vec<String>>>,
    // List 저장소: key → [value, ...] (LPUSH 순서)
    pub lstore: Arc<DashMap<String, Vec<String>>>,
    // TTL 트래커: key → expires_at (expire 명령용)
    ttl: Arc<Mutex<HashMap<String, Instant>>>,
}

impl MockValkeyClient {
    pub fn new() -> Self {
        Self {
            store: Arc::new(DashMap::new()),
            hstore: Arc::new(DashMap::new()),
            sstore: Arc::new(DashMap::new()),
            lstore: Arc::new(DashMap::new()),
            ttl: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// 키가 만료되었으면 제거하고 None 반환
    fn get_live(&self, key: &str) -> Option<String> {
        let entry = self.store.get(key)?;
        if entry.is_expired() {
            drop(entry);
            self.store.remove(key);
            return None;
        }
        Some(entry.value.clone())
    }
}

impl Default for MockValkeyClient {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl ValkeyClient for MockValkeyClient {
    async fn get(&self, key: &str) -> Result<Option<String>, AlarmError> {
        Ok(self.get_live(key))
    }

    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), AlarmError> {
        let expires_at = if ttl.is_zero() {
            None
        } else {
            Some(Instant::now() + ttl)
        };
        self.store.insert(
            key.to_owned(),
            MockEntry {
                value: value.to_owned(),
                expires_at,
            },
        );
        Ok(())
    }

    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, AlarmError> {
        // 이미 키가 존재하면(살아있으면) false 반환
        if self.get_live(key).is_some() {
            return Ok(false);
        }
        self.set(key, value, ttl).await?;
        Ok(true)
    }

    async fn del(&self, keys: &[&str]) -> Result<u64, AlarmError> {
        let mut count = 0u64;
        for key in keys {
            if self.store.remove(*key).is_some() {
                count += 1;
            }
            self.hstore.remove(*key);
            self.sstore.remove(*key);
        }
        Ok(count)
    }

    async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, AlarmError> {
        let val = self
            .hstore
            .get(key)
            .and_then(|h| h.get(field).map(|v| v.clone()));
        Ok(val)
    }

    async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), AlarmError> {
        self.hstore
            .entry(key.to_owned())
            .or_default()
            .insert(field.to_owned(), value.to_owned());
        Ok(())
    }

    async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, AlarmError> {
        let map = self
            .hstore
            .get(key)
            .map(|h| {
                h.iter()
                    .map(|e| (e.key().clone(), e.value().clone()))
                    .collect()
            })
            .unwrap_or_default();
        Ok(map)
    }

    async fn hmset(&self, key: &str, fields: &HashMap<String, String>) -> Result<(), AlarmError> {
        let hash = self.hstore.entry(key.to_owned()).or_default();
        for (f, v) in fields {
            hash.insert(f.clone(), v.clone());
        }
        Ok(())
    }

    async fn smembers(&self, key: &str) -> Result<Vec<String>, AlarmError> {
        let members = self.sstore.get(key).map(|v| v.clone()).unwrap_or_default();
        Ok(members)
    }

    async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, AlarmError> {
        let mut out = Vec::with_capacity(keys.len());
        for key in keys {
            out.push(self.sstore.get(key).map(|v| v.clone()).unwrap_or_default());
        }
        Ok(out)
    }

    async fn expire(&self, key: &str, ttl: Duration) -> Result<bool, AlarmError> {
        let exists = self.store.contains_key(key);
        if exists {
            let expires_at = Instant::now() + ttl;
            self.ttl.lock().unwrap().insert(key.to_owned(), expires_at);
        }
        Ok(exists)
    }

    async fn lpush(&self, key: &str, value: &str) -> Result<i64, AlarmError> {
        let mut entry = self.lstore.entry(key.to_owned()).or_default();
        entry.insert(0, value.to_owned());
        Ok(entry.len() as i64)
    }

    async fn ping(&self) -> Result<(), AlarmError> {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn redact_valkey_endpoint_masks_password_and_query() {
        let masked = redact_valkey_endpoint("redis://user:secret@valkey.internal:6379/0?token=abc");
        assert_eq!(masked, "redis://valkey.internal:6379/0");
        assert!(!masked.contains("secret"));
        assert!(!masked.contains("token="));
    }

    #[test]
    fn redact_valkey_endpoint_supports_unix_socket() {
        let masked = redact_valkey_endpoint("redis+unix:///tmp/valkey.sock?password=secret");
        assert_eq!(masked, "redis+unix:///tmp/valkey.sock");
        assert!(!masked.contains("secret"));
    }

    #[tokio::test]
    async fn mock_get_set_basic() {
        let client = MockValkeyClient::new();
        // 초기에는 None
        assert!(client.get("key1").await.unwrap().is_none());

        client
            .set("key1", "value1", Duration::from_secs(60))
            .await
            .unwrap();
        assert_eq!(client.get("key1").await.unwrap(), Some("value1".into()));
    }

    #[tokio::test]
    async fn mock_set_nx_prevents_overwrite() {
        let client = MockValkeyClient::new();

        let ok = client
            .set_nx("key1", "first", Duration::from_secs(60))
            .await
            .unwrap();
        assert!(ok);

        // 이미 존재 → false 반환, 값 유지
        let ok2 = client
            .set_nx("key1", "second", Duration::from_secs(60))
            .await
            .unwrap();
        assert!(!ok2);
        assert_eq!(client.get("key1").await.unwrap(), Some("first".into()));
    }

    #[tokio::test]
    async fn mock_del_removes_key() {
        let client = MockValkeyClient::new();
        client
            .set("k1", "v1", Duration::from_secs(10))
            .await
            .unwrap();
        client
            .set("k2", "v2", Duration::from_secs(10))
            .await
            .unwrap();

        let count = client.del(&["k1", "k2"]).await.unwrap();
        assert_eq!(count, 2);
        assert!(client.get("k1").await.unwrap().is_none());
    }

    #[tokio::test]
    async fn mock_hset_hget_hget_all() {
        let client = MockValkeyClient::new();

        client.hset("myhash", "field1", "val1").await.unwrap();
        client.hset("myhash", "field2", "val2").await.unwrap();

        assert_eq!(
            client.hget("myhash", "field1").await.unwrap(),
            Some("val1".into())
        );
        assert!(client.hget("myhash", "missing").await.unwrap().is_none());

        let all = client.hget_all("myhash").await.unwrap();
        assert_eq!(all.len(), 2);
    }

    #[tokio::test]
    async fn mock_hmset_merges_fields() {
        let client = MockValkeyClient::new();
        let mut fields = HashMap::new();
        fields.insert("a".into(), "1".into());
        fields.insert("b".into(), "2".into());

        client.hmset("hash1", &fields).await.unwrap();

        assert_eq!(client.hget("hash1", "a").await.unwrap(), Some("1".into()));
        assert_eq!(client.hget("hash1", "b").await.unwrap(), Some("2".into()));
    }

    #[tokio::test]
    async fn mock_smembers_multi_returns_each_key_members_in_order() {
        let client = MockValkeyClient::new();
        client
            .sstore
            .insert("set:a".into(), vec!["room1".into(), "room2".into()]);
        client.sstore.insert("set:b".into(), vec!["room9".into()]);

        let keys = vec!["set:a".to_owned(), "set:b".to_owned(), "set:c".to_owned()];
        let values = client.smembers_multi(&keys).await.unwrap();

        assert_eq!(
            values,
            vec![
                vec!["room1".to_owned(), "room2".to_owned()],
                vec!["room9".to_owned()],
                Vec::<String>::new(),
            ]
        );
    }

    #[tokio::test]
    async fn mock_smembers_multi_empty_input_returns_empty() {
        let client = MockValkeyClient::new();
        let keys: Vec<String> = Vec::new();
        let values = client.smembers_multi(&keys).await.unwrap();
        assert!(values.is_empty());
    }

    #[tokio::test]
    async fn mock_ping_always_ok() {
        let client = MockValkeyClient::new();
        client.ping().await.unwrap();
    }

    #[tokio::test]
    async fn mock_lpush_prepends_to_list() {
        let client = MockValkeyClient::new();
        let count1 = client.lpush("mylist", "first").await.unwrap();
        assert_eq!(count1, 1);
        let count2 = client.lpush("mylist", "second").await.unwrap();
        assert_eq!(count2, 2);
        // LPUSH는 리스트 앞에 삽입 → ["second", "first"]
        let list = client.lstore.get("mylist").unwrap();
        assert_eq!(*list, vec!["second".to_owned(), "first".to_owned()]);
    }

    #[tokio::test]
    async fn mock_ttl_zero_no_expiry() {
        let client = MockValkeyClient::new();
        // TTL 0 → 만료 없음
        client
            .set("eternal", "value", Duration::ZERO)
            .await
            .unwrap();
        assert_eq!(client.get("eternal").await.unwrap(), Some("value".into()));
    }
}
