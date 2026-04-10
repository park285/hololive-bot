use std::time::Duration;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionConfig {
    pub token_rotation_enabled: bool,
    pub expiry_duration: Duration,
    pub absolute_timeout: Duration,
    pub idle_session_ttl: Duration,
    pub grace_period: Duration,
    pub rotation_interval: Duration,
}

impl Default for SessionConfig {
    fn default() -> Self {
        Self {
            token_rotation_enabled: true,
            expiry_duration: Duration::from_secs(30 * 60),
            absolute_timeout: Duration::from_secs(8 * 3600),
            idle_session_ttl: Duration::from_secs(10),
            grace_period: Duration::from_secs(30),
            rotation_interval: Duration::from_secs(15 * 60),
        }
    }
}

impl SessionConfig {
    pub fn load() -> Self {
        Self {
            token_rotation_enabled: super::env::env_bool("SESSION_TOKEN_ROTATION", true),
            ..Self::default()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_session_config_defaults() {
        let cfg = SessionConfig::default();
        assert_eq!(cfg.expiry_duration, Duration::from_secs(30 * 60));
        assert_eq!(cfg.absolute_timeout, Duration::from_secs(8 * 3600));
        assert_eq!(cfg.idle_session_ttl, Duration::from_secs(10));
        assert_eq!(cfg.grace_period, Duration::from_secs(30));
        assert_eq!(cfg.rotation_interval, Duration::from_secs(15 * 60));
    }
}
