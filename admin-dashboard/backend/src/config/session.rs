use anyhow::{Result, ensure};
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
    pub fn load() -> Result<Self> {
        let defaults = Self::default();
        let idle_timeout = duration_from_ms(super::env::env_int(
            "SESSION_IDLE_TIMEOUT_MS",
            defaults.idle_timeout.as_millis() as usize,
        ));
        let idle_warning_timeout = duration_from_ms(super::env::env_int(
            "SESSION_IDLE_WARNING_TIMEOUT_MS",
            defaults.idle_warning_timeout.as_millis() as usize,
        ));

        let cfg = Self {
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
        };

        cfg.validate()?;
        Ok(cfg)
    }

    pub fn validate(&self) -> Result<()> {
        ensure!(
            self.heartbeat_interval >= Duration::from_secs(1),
            "SESSION_HEARTBEAT_INTERVAL_MS must be at least 1000"
        );

        ensure!(
            self.expiry_duration >= Duration::from_mins(1),
            "session expiry_duration must be at least 60 seconds"
        );

        ensure!(
            self.absolute_timeout > self.expiry_duration,
            "session absolute_timeout must be greater than expiry_duration"
        );

        ensure!(
            self.idle_timeout >= Duration::from_mins(1),
            "SESSION_IDLE_TIMEOUT_MS must be at least 60000"
        );

        ensure!(
            self.idle_warning_timeout < self.idle_timeout,
            "SESSION_IDLE_WARNING_TIMEOUT_MS must be less than SESSION_IDLE_TIMEOUT_MS"
        );

        ensure!(
            self.idle_session_ttl >= Duration::from_secs(1),
            "idle_session_ttl must be at least 1 second"
        );

        ensure!(
            self.idle_session_ttl < self.idle_timeout,
            "idle_session_ttl must be less than idle_timeout"
        );

        ensure!(
            self.absolute_warning_window < self.absolute_timeout,
            "SESSION_ABSOLUTE_WARNING_WINDOW_MS must be less than absolute_timeout"
        );

        ensure!(
            self.rotation_interval >= self.grace_period,
            "rotation_interval must be greater than or equal to grace_period"
        );

        Ok(())
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
                let cfg = SessionConfig::load().expect("session config load");
                assert_eq!(cfg.heartbeat_interval, Duration::from_secs(120));
                assert_eq!(cfg.idle_timeout, Duration::from_secs(900));
                assert_eq!(cfg.idle_warning_timeout, Duration::from_secs(600));
                assert_eq!(cfg.absolute_warning_window, Duration::from_secs(180));
            },
        );
    }

    #[test]
    fn test_session_config_rejects_idle_warning_timeout_at_or_after_idle_timeout() {
        with_env_vars(
            &[
                ("SESSION_IDLE_TIMEOUT_MS", Some("600000")),
                ("SESSION_IDLE_WARNING_TIMEOUT_MS", Some("900000")),
            ],
            || {
                let err =
                    SessionConfig::load().expect_err("idle warning >= idle timeout must fail");
                assert_eq!(
                    err.to_string(),
                    "SESSION_IDLE_WARNING_TIMEOUT_MS must be less than SESSION_IDLE_TIMEOUT_MS"
                );
            },
        );
    }

    #[test]
    fn test_session_config_rejects_too_fast_heartbeat() {
        with_env_vars(&[("SESSION_HEARTBEAT_INTERVAL_MS", Some("0"))], || {
            let err = SessionConfig::load().expect_err("zero heartbeat must fail");
            assert_eq!(
                err.to_string(),
                "SESSION_HEARTBEAT_INTERVAL_MS must be at least 1000"
            );
        });
    }

    #[test]
    fn test_session_config_rejects_absolute_warning_window_at_or_after_absolute_timeout() {
        let cfg = SessionConfig {
            absolute_warning_window: Duration::from_hours(8),
            ..SessionConfig::default()
        };

        let err = cfg
            .validate()
            .expect_err("absolute warning window >= absolute timeout must fail");
        assert_eq!(
            err.to_string(),
            "SESSION_ABSOLUTE_WARNING_WINDOW_MS must be less than absolute_timeout"
        );
    }
}
