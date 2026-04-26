use std::time::Duration;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionConfig {
    pub token_rotation_enabled: bool,
    pub heartbeat_interval: Duration,
    pub expiry_duration: Duration,
    pub absolute_timeout: Duration,
    pub absolute_warning_window: Duration,
    pub idle_timeout: Duration,
    pub idle_warning_timeout: Duration,
    pub idle_session_ttl: Duration,
    pub grace_period: Duration,
    pub rotation_interval: Duration,
}

impl Default for SessionConfig {
    fn default() -> Self {
        Self {
            token_rotation_enabled: true,
            heartbeat_interval: Duration::from_mins(5),
            expiry_duration: Duration::from_mins(30),
            absolute_timeout: Duration::from_hours(8),
            absolute_warning_window: Duration::from_mins(5),
            idle_timeout: Duration::from_mins(10),
            idle_warning_timeout: Duration::from_mins(9),
            idle_session_ttl: Duration::from_secs(10),
            grace_period: Duration::from_secs(30),
            rotation_interval: Duration::from_mins(15),
        }
    }
}

impl SessionConfig {
    pub fn load() -> Self {
        let defaults = Self::default();
        let idle_timeout = duration_from_ms(super::env::env_int(
            "SESSION_IDLE_TIMEOUT_MS",
            defaults.idle_timeout.as_millis() as usize,
        ));
        let idle_warning_timeout = duration_from_ms(super::env::env_int(
            "SESSION_IDLE_WARNING_TIMEOUT_MS",
            defaults.idle_warning_timeout.as_millis() as usize,
        ))
        .min(idle_timeout);

        Self {
            token_rotation_enabled: super::env::env_bool("SESSION_TOKEN_ROTATION", true),
            heartbeat_interval: duration_from_ms(super::env::env_int(
                "SESSION_HEARTBEAT_INTERVAL_MS",
                defaults.heartbeat_interval.as_millis() as usize,
            )),
            absolute_warning_window: duration_from_ms(super::env::env_int(
                "SESSION_ABSOLUTE_WARNING_WINDOW_MS",
                defaults.absolute_warning_window.as_millis() as usize,
            )),
            idle_timeout,
            idle_warning_timeout,
            ..defaults
        }
    }
}

const fn duration_from_ms(value: usize) -> Duration {
    Duration::from_millis(value as u64)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::env::test_support::with_env_vars;

    #[test]
    fn test_session_config_defaults() {
        let cfg = SessionConfig::default();
        assert_eq!(cfg.heartbeat_interval, Duration::from_mins(5));
        assert_eq!(cfg.expiry_duration, Duration::from_mins(30));
        assert_eq!(cfg.absolute_timeout, Duration::from_hours(8));
        assert_eq!(cfg.absolute_warning_window, Duration::from_mins(5));
        assert_eq!(cfg.idle_timeout, Duration::from_mins(10));
        assert_eq!(cfg.idle_warning_timeout, Duration::from_mins(9));
        assert_eq!(cfg.idle_session_ttl, Duration::from_secs(10));
        assert_eq!(cfg.grace_period, Duration::from_secs(30));
        assert_eq!(cfg.rotation_interval, Duration::from_mins(15));
    }

    #[test]
    fn test_session_config_loads_warning_policy_env() {
        with_env_vars(
            &[
                ("SESSION_HEARTBEAT_INTERVAL_MS", Some("120000")),
                ("SESSION_IDLE_TIMEOUT_MS", Some("900000")),
                ("SESSION_IDLE_WARNING_TIMEOUT_MS", Some("600000")),
                ("SESSION_ABSOLUTE_WARNING_WINDOW_MS", Some("180000")),
            ],
            || {
                let cfg = SessionConfig::load();
                assert_eq!(cfg.heartbeat_interval, Duration::from_secs(120));
                assert_eq!(cfg.idle_timeout, Duration::from_secs(900));
                assert_eq!(cfg.idle_warning_timeout, Duration::from_secs(600));
                assert_eq!(cfg.absolute_warning_window, Duration::from_secs(180));
            },
        );
    }

    #[test]
    fn test_session_config_clamps_idle_warning_timeout_to_idle_timeout() {
        with_env_vars(
            &[
                ("SESSION_IDLE_TIMEOUT_MS", Some("600000")),
                ("SESSION_IDLE_WARNING_TIMEOUT_MS", Some("900000")),
            ],
            || {
                let cfg = SessionConfig::load();
                assert_eq!(cfg.idle_timeout, Duration::from_secs(600));
                assert_eq!(cfg.idle_warning_timeout, Duration::from_secs(600));
            },
        );
    }
}
