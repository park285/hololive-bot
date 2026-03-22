use std::{env, time::Duration};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SecurityMode {
    Enforce,
    Monitor,
    Off,
}

impl SecurityMode {
    pub fn parse(input: &str) -> Self {
        match input.trim().to_ascii_lowercase().as_str() {
            "enforce" => Self::Enforce,
            "monitor" => Self::Monitor,
            "off" => Self::Off,
            _ => Self::Enforce,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SecurityConfig {
    pub allowed_origins: Vec<String>,
    pub allow_localhost_in_prod: bool,
    pub csrf_mode: SecurityMode,
    pub ws_origin_mode: SecurityMode,
    pub stream_limit_mode: SecurityMode,
    pub global_stream_limit: usize,
    pub per_session_stream_limit: usize,
    pub force_https: bool,
    pub tls_enabled: bool,
    pub tls_cert_path: String,
    pub tls_key_path: String,
}

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
    pub holo_bot_url: String,
    pub holo_bot_api_key: String,
    pub security: SecurityConfig,
    pub session: SessionConfig,
}

impl Config {
    pub fn load() -> Self {
        let environment = env_string("ENV", "production");
        let allow_localhost_in_prod = env_bool("ALLOW_LOCALHOST_IN_PROD", false);
        let allowed_origins = parse_allowed_origins(&environment, allow_localhost_in_prod);

        let session = SessionConfig {
            token_rotation_enabled: env_bool("SESSION_TOKEN_ROTATION", true),
            ..SessionConfig::default()
        };

        let security = SecurityConfig {
            allowed_origins,
            allow_localhost_in_prod,
            csrf_mode: SecurityMode::parse(&env_string("CSRF_MODE", "enforce")),
            ws_origin_mode: SecurityMode::parse(&env_string("WS_ORIGIN_MODE", "enforce")),
            stream_limit_mode: SecurityMode::parse(&env_string("STREAM_LIMIT_MODE", "enforce")),
            global_stream_limit: env_int("GLOBAL_STREAM_LIMIT", 10),
            per_session_stream_limit: env_int("PER_SESSION_STREAM_LIMIT", 2),
            force_https: env_bool("FORCE_HTTPS", true),
            tls_enabled: env_bool("TLS_ENABLED", false),
            tls_cert_path: env_string("TLS_CERT_PATH", "/certs/localhost.crt"),
            tls_key_path: env_string("TLS_KEY_PATH", "/certs/localhost.key"),
        };

        Self {
            port: {
                let p = env_int("PORT", 30190);
                u16::try_from(p).unwrap_or_else(|_| panic!("PORT={p} is out of u16 range"))
            },
            env: environment,
            log_level: env_string("LOG_LEVEL", "info"),
            admin_user: env_string("ADMIN_USER", "admin"),
            admin_pass_hash: required_alias(&["ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT"]),
            session_secret: required_alias(&["SESSION_SECRET", "ADMIN_SECRET_KEY"]),
            valkey_url: env_string("VALKEY_URL", "valkey-cache:6379"),
            docker_host: env_string("DOCKER_HOST", "tcp://docker-proxy:2375"),
            holo_bot_url: env_string("HOLO_BOT_URL", "http://hololive-kakao-bot-go:30001"),
            holo_bot_api_key: env_string("HOLO_BOT_API_KEY", ""),
            security,
            session,
        }
    }
}

pub fn env_string(key: &str, default: &str) -> String {
    env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| default.to_string())
}

pub fn env_bool(key: &str, default: bool) -> bool {
    match env::var(key) {
        Ok(value) => match value.trim().to_ascii_lowercase().as_str() {
            "1" | "true" | "yes" | "on" => true,
            "0" | "false" | "no" | "off" => false,
            _ => default,
        },
        Err(_) => default,
    }
}

pub fn env_i64(key: &str, default: i64) -> i64 {
    env::var(key)
        .ok()
        .and_then(|value| value.trim().parse::<i64>().ok())
        .unwrap_or(default)
}

pub fn env_int(key: &str, default: usize) -> usize {
    env::var(key)
        .ok()
        .and_then(|value| value.trim().parse::<usize>().ok())
        .unwrap_or(default)
}

pub fn normalize_origin(origin: &str) -> String {
    origin.trim().trim_end_matches('/').to_string()
}

pub fn is_localhost_origin(origin: &str) -> bool {
    let normalized = normalize_origin(origin).to_ascii_lowercase();
    // "://" 이후의 host(:port) 부분 추출
    let authority = normalized.split("://").nth(1).unwrap_or(&normalized);
    // IPv6 bracket 주소 처리: [::1]:3000 -> host=[::1]
    let host = if authority.starts_with('[') {
        authority.split(']').next().map(|s| format!("{s}]")).unwrap_or_default()
    } else {
        authority.split(':').next().unwrap_or("").to_string()
    };
    host == "localhost" || host == "127.0.0.1" || host == "[::1]"
}

pub fn filter_localhost_origins(origins: Vec<String>) -> Vec<String> {
    origins
        .into_iter()
        .filter(|origin| !is_localhost_origin(origin))
        .collect()
}

pub fn parse_allowed_origins(environment: &str, allow_localhost_in_prod: bool) -> Vec<String> {
    let origins = match env::var("ALLOWED_ORIGINS") {
        Ok(value) if !value.trim().is_empty() => value
            .split(',')
            .map(normalize_origin)
            .filter(|origin| !origin.is_empty())
            .collect::<Vec<_>>(),
        _ => {
            tracing::warn!(
                "ALLOWED_ORIGINS is not set; using fallback origin allowlist"
            );
            fallback_origins()
        }
    };

    if environment.eq_ignore_ascii_case("production") && !allow_localhost_in_prod {
        filter_localhost_origins(origins)
    } else {
        origins
    }
}

fn fallback_origins() -> Vec<String> {
    vec![
        "https://admin.capu.blog",
        "http://localhost:5173",
        "http://localhost:30190",
        "http://127.0.0.1:5173",
        "http://127.0.0.1:30190",
    ]
    .into_iter()
    .map(str::to_string)
    .collect()
}

fn required_alias(keys: &[&str]) -> String {
    keys.iter()
        .find_map(|key| {
            env::var(key)
                .ok()
                .map(|value| value.trim().to_string())
                .filter(|value| !value.is_empty())
        })
        .unwrap_or_else(|| panic!("required environment variable missing: {}", keys.join(" or ")))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{Mutex, OnceLock};

    fn env_lock() -> &'static Mutex<()> {
        static ENV_LOCK: OnceLock<Mutex<()>> = OnceLock::new();
        ENV_LOCK.get_or_init(|| Mutex::new(()))
    }

    fn with_env_vars<R>(vars: &[(&str, Option<&str>)], test: impl FnOnce() -> R) -> R {
        let _guard = env_lock().lock().unwrap();
        let snapshot = vars
            .iter()
            .map(|(key, _)| ((*key).to_string(), env::var(key).ok()))
            .collect::<Vec<_>>();

        for (key, value) in vars {
            match value {
                Some(value) => unsafe { env::set_var(key, value) },
                None => unsafe { env::remove_var(key) },
            }
        }

        let result = test();

        for (key, value) in snapshot {
            match value {
                Some(value) => unsafe { env::set_var(&key, value) },
                None => unsafe { env::remove_var(&key) },
            }
        }

        result
    }

    #[test]
    fn test_parse_security_mode_enforce() {
        assert_eq!(SecurityMode::parse("enforce"), SecurityMode::Enforce);
    }

    #[test]
    fn test_parse_security_mode_monitor() {
        assert_eq!(SecurityMode::parse("monitor"), SecurityMode::Monitor);
    }

    #[test]
    fn test_parse_security_mode_off() {
        assert_eq!(SecurityMode::parse("off"), SecurityMode::Off);
    }

    #[test]
    fn test_parse_security_mode_unknown_defaults_enforce() {
        assert_eq!(SecurityMode::parse("invalid"), SecurityMode::Enforce);
        assert_eq!(SecurityMode::parse(""), SecurityMode::Enforce);
    }

    #[test]
    fn test_parse_security_mode_case_insensitive() {
        assert_eq!(SecurityMode::parse("ENFORCE"), SecurityMode::Enforce);
        assert_eq!(SecurityMode::parse("Monitor"), SecurityMode::Monitor);
    }

    #[test]
    fn test_normalize_origin() {
        assert_eq!(normalize_origin("  https://example.com/  "), "https://example.com");
        assert_eq!(normalize_origin("https://example.com"), "https://example.com");
    }

    #[test]
    fn test_is_localhost_origin() {
        assert!(is_localhost_origin("http://localhost:5173"));
        assert!(is_localhost_origin("http://127.0.0.1:3000"));
        assert!(is_localhost_origin("http://[::1]:3000"));
        assert!(!is_localhost_origin("https://admin.capu.blog"));
    }

    #[test]
    fn test_session_config_defaults() {
        let cfg = SessionConfig::default();
        assert_eq!(cfg.expiry_duration, std::time::Duration::from_secs(30 * 60));
        assert_eq!(cfg.absolute_timeout, std::time::Duration::from_secs(8 * 3600));
        assert_eq!(cfg.idle_session_ttl, std::time::Duration::from_secs(10));
        assert_eq!(cfg.grace_period, std::time::Duration::from_secs(30));
        assert_eq!(cfg.rotation_interval, std::time::Duration::from_secs(15 * 60));
    }

    #[test]
    fn test_admin_pass_hash_alias_first_non_empty_wins() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", Some("hash-alias")),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(cfg.admin_pass_hash, "hash-primary");
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("   ")),
                ("ADMIN_PASS_BCRYPT", Some("hash-alias")),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(cfg.admin_pass_hash, "hash-alias");
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
                let result = std::panic::catch_unwind(Config::load);
                assert!(result.is_err());
            },
        );
    }

    #[test]
    fn test_session_secret_alias_first_non_empty_wins() {
        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("secret-primary")),
                ("ADMIN_SECRET_KEY", Some("secret-alias")),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(cfg.session_secret, "secret-primary");
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("   ")),
                ("ADMIN_SECRET_KEY", Some("secret-alias")),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(cfg.session_secret, "secret-alias");
            },
        );

        with_env_vars(
            &[
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", None),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let result = std::panic::catch_unwind(Config::load);
                assert!(result.is_err());
            },
        );
    }

    #[test]
    fn test_production_filters_localhost_origins() {
        with_env_vars(
            &[
                ("ENV", Some("production")),
                ("ALLOW_LOCALHOST_IN_PROD", Some("false")),
                (
                    "ALLOWED_ORIGINS",
                    Some("https://admin.capu.blog,http://localhost:5173,http://127.0.0.1:30190"),
                ),
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(cfg.security.allowed_origins, vec!["https://admin.capu.blog"]);
            },
        );

        with_env_vars(
            &[
                ("ENV", Some("production")),
                ("ALLOW_LOCALHOST_IN_PROD", Some("true")),
                (
                    "ALLOWED_ORIGINS",
                    Some("https://admin.capu.blog,http://localhost:5173"),
                ),
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(
                    cfg.security.allowed_origins,
                    vec!["https://admin.capu.blog", "http://localhost:5173"]
                );
            },
        );
    }

    #[test]
    fn test_fallback_origins_used_when_env_not_set() {
        with_env_vars(
            &[
                ("ENV", Some("development")),
                ("ALLOWED_ORIGINS", None),
                ("ALLOW_LOCALHOST_IN_PROD", Some("false")),
                ("ADMIN_PASS_HASH", Some("hash-primary")),
                ("ADMIN_PASS_BCRYPT", None),
                ("SESSION_SECRET", Some("session-secret")),
                ("ADMIN_SECRET_KEY", None),
            ],
            || {
                let cfg = Config::load();
                assert_eq!(
                    cfg.security.allowed_origins,
                    vec![
                        "https://admin.capu.blog",
                        "http://localhost:5173",
                        "http://localhost:30190",
                        "http://127.0.0.1:5173",
                        "http://127.0.0.1:30190",
                    ]
                );
            },
        );
    }
}
