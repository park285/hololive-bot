use chrono::{DateTime, Utc};
use deadpool_redis::Pool;
use redis::AsyncCommands;
use std::time::Duration;
use tracing::{debug, warn};

use super::session::{
    Session, SessionProvider, SessionRefreshResult, capped_expires_at, is_absolutely_expired,
    is_absolutely_expired_at, is_refreshable, refreshed_session, rotated_session_marker,
    session_key, ttl_seconds_until,
};
use crate::config::SessionConfig;

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
