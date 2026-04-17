mod env;
mod security;
mod session;

use anyhow::{Result, anyhow};

use self::env::{env_bool, env_int, env_string, optional_alias, required_alias};
pub use self::security::{SecurityConfig, SecurityMode};
pub use self::session::SessionConfig;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Config {
    pub port: u16,
    pub env: String,
    pub log_level: String,
    pub admin_user: String,
    pub admin_pass_hash: String,
    pub session_secret: String,
    pub valkey_url: String,
    pub docker_host: String,
    pub holo_admin_api_url: String,
    pub holo_bot_api_key: String,
    pub enable_openapi: bool,
    pub enable_swagger_ui: bool,
    pub log_dir: String,
    pub security: SecurityConfig,
    pub session: SessionConfig,
}

impl Config {
    pub fn load() -> Result<Self> {
        let environment = env_string("ENV", "production");
        let allow_localhost_in_prod = env_bool("ALLOW_LOCALHOST_IN_PROD", false);
        let enable_swagger_ui = env_bool("ENABLE_SWAGGER_UI", environment != "production");
        let enable_openapi = env_bool(
            "ENABLE_OPENAPI",
            enable_swagger_ui || environment != "production",
        );
        let admin_pass_hash =
            normalize_admin_pass_hash(required_alias(&["ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT"])?);
        validate_admin_pass_hash(&admin_pass_hash)?;

        Ok(Self {
            port: {
                let port = env_int("PORT", 30190);
                u16::try_from(port).map_err(|_| anyhow!("PORT={port} is out of u16 range"))?
            },
            env: environment.clone(),
            log_level: env_string("LOG_LEVEL", "info"),
            admin_user: env_string("ADMIN_USER", "admin"),
            admin_pass_hash,
            session_secret: required_alias(&["SESSION_SECRET", "ADMIN_SECRET_KEY"])?,
            valkey_url: env_string("VALKEY_URL", "valkey-cache:6379"),
            docker_host: env_string("DOCKER_HOST", "tcp://docker-proxy:2375"),
            holo_admin_api_url: optional_alias(&["HOLO_ADMIN_API_URL", "HOLO_BOT_URL"])
                .unwrap_or_else(|| "http://hololive-admin-api:30006".to_string()),
            holo_bot_api_key: optional_alias(&["HOLO_BOT_API_KEY", "API_SECRET_KEY"])
                .unwrap_or_default(),
            enable_openapi,
            enable_swagger_ui,
            log_dir: env_string("LOG_DIR", "/app/logs"),
            security: SecurityConfig::load(&environment, allow_localhost_in_prod),
            session: SessionConfig::load(),
        })
    }
}

fn normalize_admin_pass_hash(hash: String) -> String {
    if hash.starts_with("$$2a$$") || hash.starts_with("$$2b$$") || hash.starts_with("$$2y$$") {
        hash.replace("$$", "$")
    } else {
        hash
    }
}

fn validate_admin_pass_hash(hash: &str) -> Result<()> {
    bcrypt::verify("", hash)
        .map(|_| ())
        .map_err(|err| anyhow!("invalid ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt hash: {err}"))
}

#[cfg(test)]
mod tests {
    use super::env::test_support::with_env_vars;
    use super::*;
    use std::sync::OnceLock;

    fn primary_hash() -> &'static str {
        static HASH: OnceLock<String> = OnceLock::new();
        HASH.get_or_init(|| bcrypt::hash("primary-password", bcrypt::DEFAULT_COST).expect("hash"))
            .as_str()
    }

    fn alias_hash() -> &'static str {
        static HASH: OnceLock<String> = OnceLock::new();
        HASH.get_or_init(|| bcrypt::hash("alias-password", bcrypt::DEFAULT_COST).expect("hash"))
            .as_str()
    }

    #[test]
    fn test_admin_pass_hash_alias_first_non_empty_wins() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", Some(alias_hash())),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.admin_pass_hash, primary_hash());
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("   ")),
                ("ADMIN_PASS_BCRYPT", Some(alias_hash())),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.admin_pass_hash, alias_hash());
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", None),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let err = Config::load().expect_err("missing admin pass hash should fail");
                assert!(err.to_string().contains(
                    "required environment variable missing: ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT"
                ));
            },
        );
    }

    #[test]
    fn test_admin_pass_hash_normalizes_escaped_bcrypt_hash() {
        let bcrypt_hash = bcrypt::hash("test-password", bcrypt::DEFAULT_COST).expect("bcrypt hash");
        let escaped_hash = bcrypt_hash.replace('$', "$$");

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(&escaped_hash)),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.admin_pass_hash, bcrypt_hash);
            },
        );
    }

    #[test]
    fn test_invalid_admin_pass_hash_returns_error() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("not-a-valid-bcrypt-hash")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let err = Config::load().expect_err("invalid bcrypt hash should fail");
                assert!(
                    err.to_string()
                        .contains("invalid ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT bcrypt hash")
                );
            },
        );
    }

    #[test]
    fn test_session_secret_alias_first_non_empty_wins() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("secret-primary")),
                ("ADMIN_SECRET_KEY", Some("secret-alias")),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.session_secret, "secret-primary");
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("   ")),
                ("ADMIN_SECRET_KEY", Some("secret-alias")),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.session_secret, "secret-alias");
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", None),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let err = Config::load().expect_err("missing session secret should fail");
                assert!(err.to_string().contains(
                    "required environment variable missing: SESSION_SECRET or ADMIN_SECRET_KEY"
                ));
            },
        );
    }

    #[test]
    fn test_invalid_port_returns_error() {
        with_env_vars(
            &[
                ("PORT", Some("70000")),
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let err = Config::load().expect_err("invalid port should fail");
                assert_eq!(err.to_string(), "PORT=70000 is out of u16 range");
            },
        );
    }

    #[test]
    fn test_openapi_defaults_disabled_in_production() {
        with_env_vars(
            &[
                ("ENV", Some("production")),
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
                ("ENABLE_OPENAPI", None),
                ("ENABLE_SWAGGER_UI", None),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert!(!cfg.enable_openapi);
                assert!(!cfg.enable_swagger_ui);
            },
        );
    }

    #[test]
    fn test_swagger_flag_enables_openapi() {
        with_env_vars(
            &[
                ("ENV", Some("production")),
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
                ("ENABLE_SWAGGER_UI", Some("true")),
                ("ENABLE_OPENAPI", None),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert!(cfg.enable_swagger_ui);
                assert!(cfg.enable_openapi);
            },
        );
    }

    #[test]
    fn test_holo_bot_api_key_falls_back_to_api_secret_key() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
                ("HOLO_BOT_API_KEY", None),
                ("API_SECRET_KEY", Some("shared-secret")),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.holo_bot_api_key, "shared-secret");
            },
        );
    }

    #[test]
    fn test_holo_admin_api_url_prefers_new_alias_and_falls_back_to_legacy() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
                (
                    "HOLO_ADMIN_API_URL",
                    Some("http://hololive-admin-api:30006"),
                ),
                ("HOLO_BOT_URL", Some("http://hololive-kakao-bot-go:30001")),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.holo_admin_api_url, "http://hololive-admin-api:30006");
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some(primary_hash())),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
                ("HOLO_ADMIN_API_URL", Some("   ")),
                ("HOLO_BOT_URL", Some("http://legacy-bot:30001")),
            ],
            || {
                let cfg = Config::load().expect("config load");
                assert_eq!(cfg.holo_admin_api_url, "http://legacy-bot:30001");
            },
        );
    }
}
