use chrono::{DateTime, Utc};
use deadpool_redis::Pool;
use redis::AsyncCommands;
use serde::{Deserialize, Serialize};
use std::time::Duration;
use tracing::{debug, warn};

use crate::config::SessionConfig;

const KEY_PREFIX: &str = "session:admin:";

fn utc_now() -> DateTime<Utc> {
    Utc::now()
}

pub fn session_key(session_id: &str) -> String {
    format!("{KEY_PREFIX}{session_id}")
}

/// 절대 만료 시각 초과 여부
pub fn is_absolutely_expired(session: &Session) -> bool {
    Utc::now() >= session.absolute_expires_at
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    pub id: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub absolute_expires_at: DateTime<Utc>,
    #[serde(default = "utc_now")]
    pub last_rotated_at: DateTime<Utc>,
}

#[async_trait::async_trait]
pub trait SessionProvider: Send + Sync {
    async fn create_session(&self) -> Result<Session, anyhow::Error>;
    async fn get_session(&self, session_id: &str) -> Result<Option<Session>, anyhow::Error>;
    async fn validate_session(&self, session_id: &str) -> bool;
    async fn delete_session(&self, session_id: &str);
    async fn refresh_session_with_validation(
        &self, session_id: &str, idle: bool,
    ) -> Result<(bool, bool), anyhow::Error>;
    async fn rotate_session(&self, old_session_id: &str) -> Result<Option<Session>, anyhow::Error>;
}

pub struct ValkeySessionStore {
    pool: Pool,
    config: SessionConfig,
}

impl ValkeySessionStore {
    pub fn new(pool: Pool, config: SessionConfig) -> Self {
        Self { pool, config }
    }

    fn build_session(&self, id: String, now: DateTime<Utc>) -> Session {
        Session {
            id,
            created_at: now,
            expires_at: now + self.config.expiry_duration,
            absolute_expires_at: now + self.config.absolute_timeout,
            last_rotated_at: now,
        }
    }
}

// Lua 스크립트: 세션 원자적 교체
const ROTATE_LUA: &str = r#"
local old_key = KEYS[1]
local new_key = KEYS[2]
local new_data = ARGV[1]
local new_ttl = tonumber(ARGV[2])
local grace_ttl = tonumber(ARGV[3])
local old_data = redis.call('GET', old_key)
if not old_data then return nil end
redis.call('SET', new_key, new_data, 'EX', new_ttl)
redis.call('EXPIRE', old_key, grace_ttl)
return old_data
"#;

#[async_trait::async_trait]
impl SessionProvider for ValkeySessionStore {
    async fn create_session(&self) -> Result<Session, anyhow::Error> {
        let id = super::generate_session_id();
        let now = Utc::now();
        let session = self.build_session(id.clone(), now);
        let data = serde_json::to_string(&session)?;
        let ttl_secs = self.config.expiry_duration.as_secs() as i64;

        let mut conn = self.pool.get().await?;
        conn.set_ex::<_, _, ()>(session_key(&id), &data, ttl_secs as u64).await?;

        debug!(session_id = %super::truncate_session_id(&id), "session created");
        Ok(session)
    }

    async fn get_session(&self, session_id: &str) -> Result<Option<Session>, anyhow::Error> {
        let mut conn = self.pool.get().await?;
        let data: Option<String> = conn.get(session_key(session_id)).await?;

        let Some(data) = data else {
            return Ok(None);
        };

        let session: Session = serde_json::from_str(&data)?;

        if is_absolutely_expired(&session) {
            debug!(
                session_id = %super::truncate_session_id(session_id),
                "session absolute timeout exceeded, deleting"
            );
            conn.del::<_, ()>(session_key(session_id)).await?;
            return Ok(None);
        }

        Ok(Some(session))
    }

    async fn validate_session(&self, session_id: &str) -> bool {
        match self.get_session(session_id).await {
            Ok(Some(_)) => true,
            Ok(None) => false,
            Err(e) => {
                warn!(
                    session_id = %super::truncate_session_id(session_id),
                    error = %e,
                    "session validation error"
                );
                false
            }
        }
    }

    async fn delete_session(&self, session_id: &str) {
        let result = tokio::time::timeout(Duration::from_secs(5), async {
            let mut conn = self.pool.get().await?;
            conn.del::<_, ()>(session_key(session_id)).await?;
            Ok::<(), anyhow::Error>(())
        })
        .await;

        match result {
            Ok(Ok(())) => {
                debug!(
                    session_id = %super::truncate_session_id(session_id),
                    "session deleted"
                );
            }
            Ok(Err(e)) => {
                warn!(
                    session_id = %super::truncate_session_id(session_id),
                    error = %e,
                    "session delete failed"
                );
            }
            Err(_) => {
                warn!(
                    session_id = %super::truncate_session_id(session_id),
                    "session delete timed out"
                );
            }
        }
    }

    async fn refresh_session_with_validation(
        &self, session_id: &str, idle: bool,
    ) -> Result<(bool, bool), anyhow::Error> {
        let mut conn = self.pool.get().await?;
        let data: Option<String> = conn.get(session_key(session_id)).await?;

        let Some(data) = data else {
            return Ok((false, false));
        };

        let session: Session = serde_json::from_str(&data)?;

        // 절대 만료 검사
        if is_absolutely_expired(&session) {
            debug!(
                session_id = %super::truncate_session_id(session_id),
                "session absolute timeout on refresh, deleting"
            );
            conn.del::<_, ()>(session_key(session_id)).await?;
            return Ok((false, true));
        }

        let ttl = if idle {
            self.config.idle_session_ttl.as_secs() as i64
        } else {
            self.config.expiry_duration.as_secs() as i64
        };

        conn.set_ex::<_, _, ()>(session_key(session_id), &data, ttl as u64).await?;

        Ok((true, false))
    }

    async fn rotate_session(
        &self, old_session_id: &str,
    ) -> Result<Option<Session>, anyhow::Error> {
        // 기존 세션 조회
        let mut conn = self.pool.get().await?;
        let old_data: Option<String> = conn.get(session_key(old_session_id)).await?;

        let Some(old_data) = old_data else {
            return Ok(None);
        };

        let old_session: Session = serde_json::from_str(&old_data)?;

        // 절대 만료 검사
        if is_absolutely_expired(&old_session) {
            conn.del::<_, ()>(session_key(old_session_id)).await?;
            return Ok(None);
        }

        // 교체 간격 검사: 마지막 교체 후 rotation_interval 미경과 시 스킵
        let elapsed = Utc::now()
            .signed_duration_since(old_session.last_rotated_at)
            .to_std()
            .unwrap_or(Duration::ZERO);
        if elapsed < self.config.rotation_interval {
            debug!(
                session_id = %super::truncate_session_id(old_session_id),
                elapsed_secs = elapsed.as_secs(),
                interval_secs = self.config.rotation_interval.as_secs(),
                "rotation skipped, interval not reached"
            );
            return Ok(None);
        }

        // 새 세션 생성
        let new_id = super::generate_session_id();
        let now = Utc::now();
        let new_session = Session {
            id: new_id.clone(),
            created_at: old_session.created_at,
            expires_at: now + self.config.expiry_duration,
            absolute_expires_at: old_session.absolute_expires_at,
            last_rotated_at: now,
        };
        let new_data = serde_json::to_string(&new_session)?;
        let ttl_secs = self.config.expiry_duration.as_secs() as i64;
        let grace_secs = self.config.grace_period.as_secs() as i64;

        // Lua 원자적 교체
        let result: Option<String> = redis::Script::new(ROTATE_LUA)
            .key(session_key(old_session_id))
            .key(session_key(&new_id))
            .arg(&new_data)
            .arg(ttl_secs)
            .arg(grace_secs)
            .invoke_async(&mut conn)
            .await?;

        if result.is_none() {
            debug!(
                session_id = %super::truncate_session_id(old_session_id),
                "rotation failed: old session not found in Lua"
            );
            return Ok(None);
        }

        debug!(
            old_session = %super::truncate_session_id(old_session_id),
            new_session = %super::truncate_session_id(&new_id),
            "session rotated"
        );

        Ok(Some(new_session))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_session_key_format() {
        assert_eq!(session_key("abc123"), "session:admin:abc123");
        assert_eq!(session_key(""), "session:admin:");
    }

    #[test]
    fn test_session_serialization_roundtrip() {
        let now = Utc::now();
        let session = Session {
            id: "test-session-id".to_string(),
            created_at: now,
            expires_at: now + Duration::from_secs(1800),
            absolute_expires_at: now + Duration::from_secs(28800),
            last_rotated_at: now,
        };

        let json = serde_json::to_string(&session).expect("serialize");
        let deserialized: Session = serde_json::from_str(&json).expect("deserialize");

        assert_eq!(deserialized.id, session.id);
        assert_eq!(deserialized.created_at, session.created_at);
        assert_eq!(deserialized.expires_at, session.expires_at);
        assert_eq!(deserialized.absolute_expires_at, session.absolute_expires_at);
        assert_eq!(deserialized.last_rotated_at, session.last_rotated_at);
    }

    #[test]
    fn test_session_deserialization_without_last_rotated_at() {
        // last_rotated_at 필드 누락 시 serde default 적용 확인
        let json = r#"{
            "id": "session-no-rotated",
            "created_at": "2026-03-22T10:00:00Z",
            "expires_at": "2026-03-22T10:30:00Z",
            "absolute_expires_at": "2026-03-22T18:00:00Z"
        }"#;

        let session: Session = serde_json::from_str(json).expect("deserialize");
        assert_eq!(session.id, "session-no-rotated");
        // default 함수가 Utc::now()를 호출하므로 역직렬화 시점의 시각이 설정됨
        assert!(session.last_rotated_at <= Utc::now());
    }

    #[test]
    fn test_is_absolutely_expired_not_expired() {
        let now = Utc::now();
        let session = Session {
            id: "active".to_string(),
            created_at: now,
            expires_at: now + Duration::from_secs(1800),
            absolute_expires_at: now + Duration::from_secs(28800),
            last_rotated_at: now,
        };
        assert!(!is_absolutely_expired(&session));
    }

    #[test]
    fn test_is_absolutely_expired_past() {
        let now = Utc::now();
        let session = Session {
            id: "expired".to_string(),
            created_at: now - chrono::Duration::hours(9),
            expires_at: now - chrono::Duration::hours(1),
            absolute_expires_at: now - chrono::Duration::seconds(1),
            last_rotated_at: now - chrono::Duration::hours(1),
        };
        assert!(is_absolutely_expired(&session));
    }

    #[test]
    fn test_session_json_fields_present() {
        let now = Utc::now();
        let session = Session {
            id: "field-check".to_string(),
            created_at: now,
            expires_at: now + Duration::from_secs(1800),
            absolute_expires_at: now + Duration::from_secs(28800),
            last_rotated_at: now,
        };

        let json: serde_json::Value =
            serde_json::to_value(&session).expect("to_value");
        assert!(json.get("id").is_some());
        assert!(json.get("created_at").is_some());
        assert!(json.get("expires_at").is_some());
        assert!(json.get("absolute_expires_at").is_some());
        assert!(json.get("last_rotated_at").is_some());
    }
}
