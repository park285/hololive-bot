mod app;
mod external;
mod internal;
mod observability;

pub use app::{AlarmAppConfig, ConfigLoadError};
pub use external::{
    ChzzkConfig, HolodexConfig, IrisConfig, TwitchConfig, ValkeyConfig,
    validate_iris_base_url_policy,
};
pub use internal::{AlarmConfig, DatabaseConfig, HealthConfig, LoggingConfig};
pub use observability::TelemetryConfig;

#[cfg(test)]
mod tests {
    use std::{
        fs,
        sync::{LazyLock, Mutex},
    };

    use secrecy::{ExposeSecret, SecretString};

    use super::{AlarmAppConfig, ConfigLoadError, validate_iris_base_url_policy};

    static ENV_LOCK: LazyLock<Mutex<()>> = LazyLock::new(|| Mutex::new(()));

    struct EnvVarGuard {
        keys: Vec<String>,
    }

    impl EnvVarGuard {
        fn new(keys: Vec<&str>) -> Self {
            Self {
                keys: keys.into_iter().map(ToOwned::to_owned).collect(),
            }
        }
    }

    impl Drop for EnvVarGuard {
        fn drop(&mut self) {
            for key in &self.keys {
                // SAFETY: ENV_LOCK으로 직렬화하여 안전하게 환경변수 제거
                unsafe { std::env::remove_var(key) };
            }
        }
    }

    #[test]
    fn load_config_applies_alarm_env_overrides() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let _env_guard = EnvVarGuard::new(vec![
            "ALARM__DATABASE__HOST",
            "ALARM__HEALTH__PORT",
            "ALARM__LOGGING__LEVEL",
            "ALARM__VALKEY__POOL_SIZE",
            "ALARM__ALARM__YOUTUBE_CHECK_TIMEOUT_SECS",
            "ALARM__IRIS__CIRCUIT_FAILURE_THRESHOLD",
            "ALARM__ALARM__TWITCH_ENABLED",
            "ALARM__DATABASE__PASSWORD",
            "ALARM__HOLODEX__API_KEYS",
        ]);

        let temp_config =
            std::env::temp_dir().join(format!("alarm-config-{}.toml", std::process::id()));
        fs::write(&temp_config, "").expect("임시 설정 파일 작성 가능해야 함");

        // SAFETY: ENV_LOCK으로 직렬화하여 안전하게 환경변수 설정
        unsafe {
            std::env::set_var("ALARM__DATABASE__HOST", "override-db-host");
            std::env::set_var("ALARM__HEALTH__PORT", "30099");
            std::env::set_var("ALARM__LOGGING__LEVEL", "debug");
            std::env::set_var("ALARM__VALKEY__POOL_SIZE", "8");
            std::env::set_var("ALARM__ALARM__YOUTUBE_CHECK_TIMEOUT_SECS", "50");
            std::env::set_var("ALARM__IRIS__CIRCUIT_FAILURE_THRESHOLD", "7");
            std::env::set_var("ALARM__ALARM__TWITCH_ENABLED", "false");
            std::env::set_var("ALARM__DATABASE__PASSWORD", "super-secret-password");
            std::env::set_var("ALARM__HOLODEX__API_KEYS", "[\"key-a\",\"key-b\"]");
        }

        let loaded = AlarmAppConfig::load_from_path(&temp_config).expect("설정 로드 성공해야 함");

        assert_eq!(loaded.database.host, "override-db-host");
        assert_eq!(loaded.health.port, 30099);
        assert_eq!(loaded.logging.level, "debug");
        assert!(!loaded.alarm.twitch_enabled);
        assert_eq!(loaded.valkey.pool_size, 8);
        assert_eq!(loaded.alarm.youtube_check_timeout_secs, 50);
        assert_eq!(loaded.iris.circuit_failure_threshold, 7);
        assert_eq!(loaded.holodex.api_keys, vec!["key-a", "key-b"]);
        assert_eq!(
            loaded.database.password.expose_secret(),
            "super-secret-password"
        );

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn load_config_defaults_applied() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");

        let temp_config =
            std::env::temp_dir().join(format!("alarm-config-defaults-{}.toml", std::process::id()));
        fs::write(&temp_config, "").expect("임시 설정 파일 작성 가능해야 함");

        let loaded =
            AlarmAppConfig::load_from_path(&temp_config).expect("기본값으로 로드 성공해야 함");

        assert_eq!(loaded.health.port, 30011);
        assert_eq!(loaded.valkey.pool_size, 4);
        assert_eq!(loaded.holodex.timeout_secs, 30);
        assert_eq!(loaded.holodex.circuit_failure_threshold, 5);
        assert_eq!(loaded.iris.circuit_reset_secs, 30);
        assert!(loaded.alarm.twitch_enabled);
        assert_eq!(loaded.alarm.target_minutes, vec![5, 3, 1]);
        assert_eq!(loaded.alarm.youtube_check_timeout_secs, 45);
        assert_eq!(loaded.alarm.chzzk_check_timeout_secs, 30);
        assert_eq!(loaded.alarm.twitch_check_timeout_secs, 30);

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn load_config_holodex_legacy_env_is_ignored() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let _env_guard = EnvVarGuard::new(vec!["HOLODEX_API_KEY_1", "HOLODEX_API_KEY_2"]);

        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-holodex-legacy-ignored-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[holodex]
api_keys = []
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        // SAFETY: ENV_LOCK으로 직렬화하여 안전하게 환경변수 설정
        unsafe {
            std::env::set_var("HOLODEX_API_KEY_1", "key-a");
            std::env::set_var("HOLODEX_API_KEY_2", "key-b");
        }

        let loaded = AlarmAppConfig::load_from_path(&temp_config).expect("설정 로드 성공해야 함");
        assert!(loaded.holodex.api_keys.is_empty());

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn database_url_encodes_special_chars() {
        use super::DatabaseConfig;

        let cfg = DatabaseConfig {
            host: "db-host".into(),
            port: 5432,
            name: "my db".into(),
            user: "user@name".into(),
            password: SecretString::from("p@ss/word".to_owned()),
            sslmode: "disable".into(),
            max_connections: 5,
        };
        let url = cfg.database_url();
        // 특수문자가 URL 인코딩되었는지 확인
        assert!(url.starts_with("postgres://"));
        assert!(url.contains("p%40ss"));
        assert!(url.contains("my+db"));
    }

    #[test]
    fn iris_base_url_policy_allows_internal_http_and_public_https() {
        assert!(validate_iris_base_url_policy("http://iris:8080").is_ok());
        assert!(validate_iris_base_url_policy("http://10.0.0.10:8080").is_ok());
        assert!(validate_iris_base_url_policy("https://api.example.com").is_ok());
    }

    #[test]
    fn iris_base_url_policy_blocks_public_http() {
        let err =
            validate_iris_base_url_policy("http://api.example.com").expect_err("must be blocked");
        assert!(err.contains("public HTTP endpoint is blocked"));
    }

    #[test]
    fn load_config_rejects_public_http_iris_base_url() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-iris-public-http-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[iris]
base_url = "http://api.example.com"
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        let loaded = AlarmAppConfig::load_from_path(&temp_config);
        assert!(
            matches!(loaded, Err(ConfigLoadError::Validation(msg)) if msg.contains("public HTTP endpoint is blocked")),
            "공개망 HTTP는 설정 단계에서 차단되어야 함"
        );

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn load_config_rejects_zero_poll_with_validator() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-validator-zero-poll-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[alarm]
chzzk_poll_secs = 0
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        let loaded = AlarmAppConfig::load_from_path(&temp_config);
        assert!(
            matches!(loaded, Err(ConfigLoadError::Validation(msg)) if msg.contains("chzzk_poll_secs")),
            "poll 값 0은 validator에서 차단되어야 함"
        );

        let _ = fs::remove_file(&temp_config);
    }

    #[test]
    fn secret_fields_are_redacted_in_debug_output() {
        let _guard = ENV_LOCK.lock().expect("env lock should be available");
        let temp_config = std::env::temp_dir().join(format!(
            "alarm-config-secret-redaction-{}.toml",
            std::process::id()
        ));
        fs::write(
            &temp_config,
            r#"
[iris]
bot_token = "iris-secret-token"

[twitch]
client_secret = "twitch-secret-token"

[database]
password = "db-secret-password"
"#,
        )
        .expect("임시 설정 파일 작성 가능해야 함");

        let loaded = AlarmAppConfig::load_from_path(&temp_config).expect("설정 로드 성공해야 함");
        assert_eq!(loaded.iris.bot_token.expose_secret(), "iris-secret-token");
        assert_eq!(
            loaded.twitch.client_secret.expose_secret(),
            "twitch-secret-token"
        );
        assert_eq!(
            loaded.database.password.expose_secret(),
            "db-secret-password"
        );

        let debug_iris = format!("{:?}", loaded.iris.bot_token);
        let debug_twitch = format!("{:?}", loaded.twitch.client_secret);
        let debug_db = format!("{:?}", loaded.database.password);
        assert!(!debug_iris.contains("iris-secret-token"));
        assert!(!debug_twitch.contains("twitch-secret-token"));
        assert!(!debug_db.contains("db-secret-password"));

        let _ = fs::remove_file(&temp_config);
    }
}
