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

fn capped_expires_at(
    now: DateTime<Utc>,
    ttl: Duration,
    absolute_expires_at: DateTime<Utc>,
) -> anyhow::Result<DateTime<Utc>> {
    let ttl = chrono::Duration::from_std(ttl)?;
    Ok(std::cmp::min(now + ttl, absolute_expires_at))
}

fn ttl_seconds_until(expires_at: DateTime<Utc>, now: DateTime<Utc>) -> anyhow::Result<u64> {
    let ttl = expires_at
        .signed_duration_since(now)
        .to_std()
        .map_err(|_| anyhow::anyhow!("session expiry is already in the past"))?;

    Ok(ttl.as_secs().max(1))
}

fn refreshed_session(
    session: &Session,
    now: DateTime<Utc>,
    ttl: Duration,
) -> anyhow::Result<Session> {
    let mut refreshed = session.clone();
    refreshed.expires_at = capped_expires_at(now, ttl, session.absolute_expires_at)?;
    Ok(refreshed)
}

pub fn session_key(session_id: &str) -> String {
    format!("{KEY_PREFIX}{session_id}")
}

pub fn is_absolutely_expired(session: &Session) -> bool {
    Utc::now() >= session.absolute_expires_at
}

fn is_absolutely_expired_at(session: &Session, now: DateTime<Utc>) -> bool {
    now >= session.absolute_expires_at
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    pub id: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub absolute_expires_at: DateTime<Utc>,
    #[serde(default = "utc_now")]
    pub last_rotated_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rotated_to: Option<String>,
}

const fn is_refreshable(session: &Session) -> bool {
    session.rotated_to.is_none()
}

#[derive(Debug, Clone)]
pub enum SessionRefreshResult {
    Refreshed(Session),
    IdleShortened,
    Rotated(Session),
    Missing,
    NotRefreshable,
    AbsoluteExpired,
}

fn rotated_session_marker(
    session: &Session,
    new_session_id: &str,
    now: DateTime<Utc>,
    grace_ttl: Duration,
) -> anyhow::Result<Session> {
    let grace_expires_at = capped_expires_at(now, grace_ttl, session.absolute_expires_at)?;

    Ok(Session {
        id: session.id.clone(),
        created_at: session.created_at,
        expires_at: grace_expires_at,
        absolute_expires_at: session.absolute_expires_at,
        last_rotated_at: now,
        rotated_to: Some(new_session_id.to_string()),
    })
}

#[async_trait::async_trait]
pub trait SessionProvider: Send + Sync {
    async fn create_session(&self) -> Result<Session, anyhow::Error>;
    async fn get_session(&self, session_id: &str) -> Result<Option<Session>, anyhow::Error>;
    #[allow(dead_code)]
    async fn validate_session(&self, session_id: &str) -> Result<bool, anyhow::Error>;
    async fn delete_session(&self, session_id: &str);
    async fn refresh_session_with_validation(
        &self,
        session_id: &str,
        idle: bool,
    ) -> Result<SessionRefreshResult, anyhow::Error>;
    async fn rotate_session(&self, old_session_id: &str) -> Result<Option<Session>, anyhow::Error>;
}

#[allow(missing_debug_implementations)]
pub struct ValkeySessionStore {
    pool: Pool,
    config: SessionConfig,
}

impl ValkeySessionStore {
    pub const fn new(pool: Pool, config: SessionConfig) -> Self {
        Self { pool, config }
    }

    fn build_session(&self, id: String, now: DateTime<Utc>) -> Session {
        let absolute_expires_at = now + self.config.absolute_timeout;
        Session {
            id,
            created_at: now,
            expires_at: std::cmp::min(now + self.config.expiry_duration, absolute_expires_at),
            absolute_expires_at,
            last_rotated_at: now,
            rotated_to: None,
        }
    }

    async fn refresh_result_for_rotated_to(
        &self,
        rotated_to: &str,
    ) -> Result<SessionRefreshResult, anyhow::Error> {
        match self.get_session(rotated_to).await? {
            Some(replacement) => Ok(SessionRefreshResult::Rotated(replacement)),
            None => Ok(SessionRefreshResult::NotRefreshable),
        }
    }
}

const REFRESH_CAS_LUA: &str = r"
local key = KEYS[1]
local expected_data = ARGV[1]
local refreshed_data = ARGV[2]
local ttl = tonumber(ARGV[3])

local current_data = redis.call('GET', key)
if not current_data then
  return 0
end

if current_data ~= expected_data then
  return -1
end

redis.call('SET', key, refreshed_data, 'EX', ttl)
return 1
";

const ROTATE_LUA: &str = r"
local old_key = KEYS[1]
local new_key = KEYS[2]
local new_data = ARGV[1]
local old_marker_data = ARGV[2]
local new_ttl = tonumber(ARGV[3])
local grace_ttl = tonumber(ARGV[4])
local expected_old_data = ARGV[5]

local old_data = redis.call('GET', old_key)
if not old_data then
  return nil
end

if old_data ~= expected_old_data then
  return nil
end

redis.call('SET', new_key, new_data, 'EX', new_ttl)
redis.call('SET', old_key, old_marker_data, 'EX', grace_ttl)

return old_data
";

#[async_trait::async_trait]
impl SessionProvider for ValkeySessionStore {
    async fn create_session(&self) -> Result<Session, anyhow::Error> {
        let id = super::generate_session_id();
        let now = Utc::now();
        let session = self.build_session(id.clone(), now);
        let data = serde_json::to_string(&session)?;
        let ttl_secs = ttl_seconds_until(session.expires_at, now)?;

        let mut conn = self.pool.get().await?;
        conn.set_ex::<_, _, ()>(session_key(&id), &data, ttl_secs)
            .await?;

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

    async fn validate_session(&self, session_id: &str) -> Result<bool, anyhow::Error> {
        match self.get_session(session_id).await {
            Ok(Some(session)) => Ok(is_refreshable(&session)),
            Ok(None) => Ok(false),
            Err(e) => {
                warn!(
                    session_id = %super::truncate_session_id(session_id),
                    error = %e,
                    "session validation error"
                );
                Err(e)
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
        &self,
        session_id: &str,
        idle: bool,
    ) -> Result<SessionRefreshResult, anyhow::Error> {
        let mut conn = self.pool.get().await?;

        for _ in 0..2 {
            let data: Option<String> = conn.get(session_key(session_id)).await?;

            let Some(data) = data else {
                return Ok(SessionRefreshResult::Missing);
            };

            let session: Session = serde_json::from_str(&data)?;

            let now = Utc::now();
            if is_absolutely_expired_at(&session, now) {
                debug!(
                    session_id = %super::truncate_session_id(session_id),
                    "session absolute timeout on refresh, deleting"
                );
                conn.del::<_, ()>(session_key(session_id)).await?;
                return Ok(SessionRefreshResult::AbsoluteExpired);
            }

            if let Some(rotated_to) = session.rotated_to.as_deref() {
                debug!(
                    session_id = %super::truncate_session_id(session_id),
                    rotated_to = %super::truncate_session_id(rotated_to),
                    "session refresh converged to rotated replacement"
                );
                let rotated_to = rotated_to.to_string();
                drop(conn);
                return self.refresh_result_for_rotated_to(&rotated_to).await;
            }

            let ttl = if idle {
                self.config.idle_session_ttl
            } else {
                self.config.expiry_duration
            };
            let refreshed = refreshed_session(&session, now, ttl)?;
            let refreshed_data = serde_json::to_string(&refreshed)?;
            let ttl_secs = ttl_seconds_until(refreshed.expires_at, now)?;

            let cas_result: i64 = redis::Script::new(REFRESH_CAS_LUA)
                .key(session_key(session_id))
                .arg(&data)
                .arg(&refreshed_data)
                .arg(ttl_secs as i64)
                .invoke_async(&mut conn)
                .await?;

            match cas_result {
                1 if idle => return Ok(SessionRefreshResult::IdleShortened),
                1 => return Ok(SessionRefreshResult::Refreshed(refreshed)),
                0 => return Ok(SessionRefreshResult::Missing),
                -1 => {}
                other => {
                    return Err(anyhow::anyhow!(
                        "unexpected session refresh CAS result: {other}"
                    ));
                }
            }
        }

        let data: Option<String> = conn.get(session_key(session_id)).await?;
        let Some(data) = data else {
            return Ok(SessionRefreshResult::Missing);
        };

        let session: Session = serde_json::from_str(&data)?;
        if is_absolutely_expired(&session) {
            conn.del::<_, ()>(session_key(session_id)).await?;
            return Ok(SessionRefreshResult::AbsoluteExpired);
        }

        if let Some(rotated_to) = session.rotated_to.as_deref() {
            let rotated_to = rotated_to.to_string();
            drop(conn);
            return self.refresh_result_for_rotated_to(&rotated_to).await;
        }

        if idle {
            return Err(anyhow::anyhow!("idle session refresh CAS did not converge"));
        }

        Ok(SessionRefreshResult::Refreshed(session))
    }

    async fn rotate_session(&self, old_session_id: &str) -> Result<Option<Session>, anyhow::Error> {
        let mut conn = self.pool.get().await?;
        let old_data: Option<String> = conn.get(session_key(old_session_id)).await?;

        let Some(old_data) = old_data else {
            return Ok(None);
        };

        let old_session: Session = serde_json::from_str(&old_data)?;
        let now = Utc::now();

        if is_absolutely_expired_at(&old_session, now) {
            conn.del::<_, ()>(session_key(old_session_id)).await?;
            return Ok(None);
        }

        if !is_refreshable(&old_session) {
            debug!(
                session_id = %super::truncate_session_id(old_session_id),
                "rotation skipped for already rotated session"
            );

            if let Some(rotated_to) = old_session.rotated_to.as_deref() {
                let rotated_to = rotated_to.to_string();
                drop(conn);
                return match self.get_session(&rotated_to).await? {
                    Some(replacement) => Ok(Some(replacement)),
                    None => Ok(None),
                };
            }

            return Ok(None);
        }

        let elapsed = now
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

        let new_id = super::generate_session_id();
        let new_expires_at = capped_expires_at(
            now,
            self.config.expiry_duration,
            old_session.absolute_expires_at,
        )?;
        let new_session = Session {
            id: new_id.clone(),
            created_at: old_session.created_at,
            expires_at: new_expires_at,
            absolute_expires_at: old_session.absolute_expires_at,
            last_rotated_at: now,
            rotated_to: None,
        };
        let old_marker =
            rotated_session_marker(&old_session, &new_id, now, self.config.grace_period)?;
        let new_data = serde_json::to_string(&new_session)?;
        let old_marker_data = serde_json::to_string(&old_marker)?;
        let ttl_secs = ttl_seconds_until(new_session.expires_at, now)?;
        let grace_secs = ttl_seconds_until(old_marker.expires_at, now)?;

        let result: Option<String> = redis::Script::new(ROTATE_LUA)
            .key(session_key(old_session_id))
            .key(session_key(&new_id))
            .arg(&new_data)
            .arg(&old_marker_data)
            .arg(ttl_secs as i64)
            .arg(grace_secs as i64)
            .arg(&old_data)
            .invoke_async(&mut conn)
            .await?;

        if result.is_none() {
            debug!(
                session_id = %super::truncate_session_id(old_session_id),
                "rotation skipped because session changed during CAS"
            );

            let current_data: Option<String> = conn.get(session_key(old_session_id)).await?;
            let Some(current_data) = current_data else {
                return Ok(None);
            };

            let current_session: Session = serde_json::from_str(&current_data)?;
            if let Some(rotated_to) = current_session.rotated_to.as_deref() {
                let rotated_to = rotated_to.to_string();
                drop(conn);
                return match self.get_session(&rotated_to).await? {
                    Some(replacement) => Ok(Some(replacement)),
                    None => Ok(None),
                };
            }

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
            rotated_to: None,
        };

        let json = serde_json::to_string(&session).expect("serialize");
        let deserialized: Session = serde_json::from_str(&json).expect("deserialize");

        assert_eq!(deserialized.id, session.id);
        assert_eq!(deserialized.created_at, session.created_at);
        assert_eq!(deserialized.expires_at, session.expires_at);
        assert_eq!(
            deserialized.absolute_expires_at,
            session.absolute_expires_at
        );
        assert_eq!(deserialized.last_rotated_at, session.last_rotated_at);
        assert_eq!(deserialized.rotated_to, session.rotated_to);
    }

    #[test]
    fn test_session_deserialization_without_last_rotated_at() {
        let json = r#"{
            "id": "session-no-rotated",
            "created_at": "2026-03-22T10:00:00Z",
            "expires_at": "2026-03-22T10:30:00Z",
            "absolute_expires_at": "2026-03-22T18:00:00Z"
        }"#;

        let session: Session = serde_json::from_str(json).expect("deserialize");
        assert_eq!(session.id, "session-no-rotated");
        assert!(session.last_rotated_at <= Utc::now());
        assert_eq!(session.rotated_to, None);
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
            rotated_to: None,
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
            rotated_to: None,
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
            rotated_to: None,
        };

        let json: serde_json::Value = serde_json::to_value(&session).expect("to_value");
        assert!(json.get("id").is_some());
        assert!(json.get("created_at").is_some());
        assert!(json.get("expires_at").is_some());
        assert!(json.get("absolute_expires_at").is_some());
        assert!(json.get("last_rotated_at").is_some());
        assert!(json.get("rotated_to").is_none());
    }

    #[test]
    fn test_refreshed_session_updates_expires_at() {
        let now = Utc::now();
        let session = Session {
            id: "refresh-check".to_string(),
            created_at: now - chrono::Duration::minutes(5),
            expires_at: now - chrono::Duration::minutes(1),
            absolute_expires_at: now + chrono::Duration::hours(1),
            last_rotated_at: now - chrono::Duration::minutes(5),
            rotated_to: None,
        };

        let refreshed = refreshed_session(&session, now, Duration::from_secs(90)).expect("refresh");

        assert_eq!(refreshed.id, session.id);
        assert_eq!(refreshed.created_at, session.created_at);
        assert_eq!(refreshed.absolute_expires_at, session.absolute_expires_at);
        assert_eq!(refreshed.last_rotated_at, session.last_rotated_at);
        assert_eq!(refreshed.rotated_to, session.rotated_to);
        assert_eq!(refreshed.expires_at, now + chrono::Duration::seconds(90));
    }

    #[test]
    fn test_refreshed_session_caps_expiry_at_absolute_timeout() {
        let now = Utc::now();
        let absolute_expires_at = now + chrono::Duration::seconds(20);
        let session = Session {
            id: "refresh-cap-check".to_string(),
            created_at: now - chrono::Duration::minutes(5),
            expires_at: now - chrono::Duration::minutes(1),
            absolute_expires_at,
            last_rotated_at: now - chrono::Duration::minutes(5),
            rotated_to: None,
        };

        let refreshed = refreshed_session(&session, now, Duration::from_secs(90)).expect("refresh");

        assert_eq!(refreshed.expires_at, absolute_expires_at);
    }

    #[test]
    fn test_ttl_seconds_until_uses_minimum_one_second() {
        let now = Utc::now();
        assert_eq!(ttl_seconds_until(now, now).expect("ttl"), 1);
    }

    #[test]
    fn test_rotated_session_marker_sets_refresh_blocker() {
        let now = Utc::now();
        let session = Session {
            id: "rotating-session".to_string(),
            created_at: now - chrono::Duration::minutes(10),
            expires_at: now + chrono::Duration::minutes(20),
            absolute_expires_at: now + chrono::Duration::hours(1),
            last_rotated_at: now - chrono::Duration::minutes(10),
            rotated_to: None,
        };

        let marker =
            rotated_session_marker(&session, "new-session-id", now, Duration::from_secs(30))
                .expect("marker");

        assert_eq!(marker.id, session.id);
        assert_eq!(marker.rotated_to.as_deref(), Some("new-session-id"));
        assert_eq!(marker.last_rotated_at, now);
        assert_eq!(marker.expires_at, now + chrono::Duration::seconds(30));
    }

    #[test]
    fn test_rotated_session_marker_caps_expiry_at_absolute_timeout() {
        let now = Utc::now();
        let absolute_expires_at = now + chrono::Duration::seconds(5);
        let session = Session {
            id: "rotating-cap-session".to_string(),
            created_at: now - chrono::Duration::minutes(10),
            expires_at: now + chrono::Duration::minutes(20),
            absolute_expires_at,
            last_rotated_at: now - chrono::Duration::minutes(10),
            rotated_to: None,
        };

        let marker =
            rotated_session_marker(&session, "new-session-id", now, Duration::from_secs(30))
                .expect("marker");

        assert_eq!(marker.expires_at, absolute_expires_at);
    }

    #[test]
    fn test_is_refreshable_rejects_rotated_session() {
        let now = Utc::now();
        let session = Session {
            id: "stale-session".to_string(),
            created_at: now - chrono::Duration::minutes(10),
            expires_at: now + chrono::Duration::seconds(30),
            absolute_expires_at: now + chrono::Duration::hours(1),
            last_rotated_at: now,
            rotated_to: Some("replacement-session".to_string()),
        };

        assert!(!is_refreshable(&session));
    }

    #[test]
    fn test_refresh_lua_uses_compare_and_set() {
        assert!(REFRESH_CAS_LUA.contains("local expected_data = ARGV[1]"));
        assert!(REFRESH_CAS_LUA.contains("current_data ~= expected_data"));
        assert!(REFRESH_CAS_LUA.contains("return -1"));
        assert!(REFRESH_CAS_LUA.contains("redis.call('SET', key, refreshed_data, 'EX', ttl)"));
    }

    #[test]
    fn test_rotate_lua_rewrites_old_session_with_marker_after_cas() {
        assert!(ROTATE_LUA.contains("local expected_old_data = ARGV[5]"));
        assert!(ROTATE_LUA.contains("old_data ~= expected_old_data"));
        assert!(
            ROTATE_LUA.contains("redis.call('SET', old_key, old_marker_data, 'EX', grace_ttl)")
        );
    }
}
