use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::time::Duration;

pub use super::session_store::ValkeySessionStore;

const KEY_PREFIX: &str = "session:admin:";

fn utc_now() -> DateTime<Utc> {
    Utc::now()
}

pub(super) fn capped_expires_at(
    now: DateTime<Utc>,
    ttl: Duration,
    absolute_expires_at: DateTime<Utc>,
) -> anyhow::Result<DateTime<Utc>> {
    let ttl = chrono::Duration::from_std(ttl)?;
    Ok(std::cmp::min(now + ttl, absolute_expires_at))
}

pub(super) fn ttl_seconds_until(
    expires_at: DateTime<Utc>,
    now: DateTime<Utc>,
) -> anyhow::Result<u64> {
    let ttl = expires_at
        .signed_duration_since(now)
        .to_std()
        .map_err(|_| anyhow::anyhow!("session expiry is already in the past"))?;

    Ok(ttl.as_secs().max(1))
}

pub(super) fn refreshed_session(
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

pub(super) fn is_absolutely_expired_at(session: &Session, now: DateTime<Utc>) -> bool {
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

pub(super) const fn is_refreshable(session: &Session) -> bool {
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

pub(super) fn rotated_session_marker(
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
}
