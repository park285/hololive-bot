use std::env;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SecurityMode {
    Enforce,
    Monitor,
    Off,
}

impl SecurityMode {
    pub fn parse(input: &str) -> Self {
        match input.trim().to_ascii_lowercase().as_str() {
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
    pub force_https: bool,
    pub tls_enabled: bool,
    pub tls_cert_path: String,
    pub tls_key_path: String,
}

impl SecurityConfig {
    pub fn load(environment: &str, allow_localhost_in_prod: bool) -> Self {
        Self {
            allowed_origins: parse_allowed_origins(environment, allow_localhost_in_prod),
            allow_localhost_in_prod,
            csrf_mode: SecurityMode::parse(&super::env::env_string("CSRF_MODE", "enforce")),
            ws_origin_mode: SecurityMode::parse(&super::env::env_string(
                "WS_ORIGIN_MODE",
                "enforce",
            )),
            force_https: super::env::env_bool("FORCE_HTTPS", true),
            tls_enabled: super::env::env_bool("TLS_ENABLED", false),
            tls_cert_path: super::env::env_string("TLS_CERT_PATH", "/certs/localhost.crt"),
            tls_key_path: super::env::env_string("TLS_KEY_PATH", "/certs/localhost.key"),
        }
    }
}

pub fn normalize_origin(origin: &str) -> String {
    origin.trim().trim_end_matches('/').to_string()
}

pub fn is_localhost_origin(origin: &str) -> bool {
    let normalized = normalize_origin(origin).to_ascii_lowercase();
    let authority = normalized.split("://").nth(1).unwrap_or(&normalized);
    let host = if authority.starts_with('[') {
        authority
            .split(']')
            .next()
            .map(|value| format!("{value}]"))
            .unwrap_or_default()
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
            tracing::warn!("ALLOWED_ORIGINS is not set; using fallback origin allowlist");
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

#[cfg(test)]
mod tests {
    use super::super::env::test_support::with_env_vars;
    use super::*;

    #[test]
    fn test_parse_security_mode_variants() {
        assert_eq!(SecurityMode::parse("enforce"), SecurityMode::Enforce);
        assert_eq!(SecurityMode::parse("monitor"), SecurityMode::Monitor);
        assert_eq!(SecurityMode::parse("off"), SecurityMode::Off);
        assert_eq!(SecurityMode::parse("invalid"), SecurityMode::Enforce);
        assert_eq!(SecurityMode::parse("Monitor"), SecurityMode::Monitor);
    }

    #[test]
    fn test_normalize_origin() {
        assert_eq!(
            normalize_origin("  https://example.com/  "),
            "https://example.com"
        );
        assert_eq!(
            normalize_origin("https://example.com"),
            "https://example.com"
        );
    }

    #[test]
    fn test_is_localhost_origin() {
        assert!(is_localhost_origin("http://localhost:5173"));
        assert!(is_localhost_origin("http://127.0.0.1:3000"));
        assert!(is_localhost_origin("http://[::1]:3000"));
        assert!(!is_localhost_origin("https://admin.capu.blog"));
    }

    #[test]
    fn test_parse_allowed_origins_filters_localhost_in_production() {
        with_env_vars(
            &[
                ("ENV", Some("production")),
                ("ALLOW_LOCALHOST_IN_PROD", Some("false")),
                (
                    "ALLOWED_ORIGINS",
                    Some("https://admin.capu.blog,http://localhost:5173,http://127.0.0.1:30190"),
                ),
            ],
            || {
                assert_eq!(
                    parse_allowed_origins("production", false),
                    vec!["https://admin.capu.blog"]
                );
            },
        );
    }

    #[test]
    fn test_parse_allowed_origins_keeps_localhost_when_enabled() {
        with_env_vars(
            &[
                ("ENV", Some("production")),
                ("ALLOW_LOCALHOST_IN_PROD", Some("true")),
                (
                    "ALLOWED_ORIGINS",
                    Some("https://admin.capu.blog,http://localhost:5173"),
                ),
            ],
            || {
                assert_eq!(
                    parse_allowed_origins("production", true),
                    vec!["https://admin.capu.blog", "http://localhost:5173"]
                );
            },
        );
    }

    #[test]
    fn test_parse_allowed_origins_uses_fallback_when_missing() {
        with_env_vars(
            &[
                ("ENV", Some("development")),
                ("ALLOWED_ORIGINS", None),
                ("ALLOW_LOCALHOST_IN_PROD", Some("false")),
            ],
            || {
                assert_eq!(
                    parse_allowed_origins("development", false),
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
