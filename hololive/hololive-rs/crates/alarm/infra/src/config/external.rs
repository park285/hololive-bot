use std::{collections::HashSet, net::IpAddr};

use secrecy::SecretString;
use serde::{Deserialize, Deserializer};
use validator::Validate;

/// Valkey(Redis) 연결 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct ValkeyConfig {
    /// TCP: "redis://host:port", Unix: "redis+unix:///path/to/sock"
    pub url: String,
    #[validate(range(min = 1))]
    pub pool_size: u32,
}

/// Holodex API 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct HolodexConfig {
    pub base_url: String,
    /// API 키 로테이션용 복수 키 목록
    #[serde(deserialize_with = "deserialize_api_keys")]
    pub api_keys: Vec<String>,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    /// 요청 간 최소 간격(밀리초)
    #[validate(range(min = 1))]
    pub rate_limit_ms: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

fn deserialize_api_keys<'de, D>(deserializer: D) -> Result<Vec<String>, D::Error>
where
    D: Deserializer<'de>,
{
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum ApiKeysInput {
        List(Vec<String>),
        Text(String),
    }

    match ApiKeysInput::deserialize(deserializer)? {
        ApiKeysInput::List(keys) => Ok(keys),
        ApiKeysInput::Text(raw) => parse_api_keys_text(&raw).map_err(serde::de::Error::custom),
    }
}

fn parse_api_keys_text(raw: &str) -> Result<Vec<String>, String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }

    if trimmed.starts_with('[') {
        return serde_json::from_str::<Vec<String>>(trimmed)
            .map_err(|e| format!("invalid holodex.api_keys JSON array: {e}"));
    }

    Ok(trimmed
        .split(',')
        .map(|token| token.trim().trim_matches('"').trim_matches('\''))
        .filter(|token| !token.is_empty())
        .map(ToOwned::to_owned)
        .collect())
}

pub(super) fn normalize_api_keys(keys: Vec<String>) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut normalized = Vec::new();
    for key in keys {
        let trimmed = key.trim();
        if trimmed.is_empty() {
            continue;
        }
        if seen.insert(trimmed.to_owned()) {
            normalized.push(trimmed.to_owned());
        }
    }
    normalized
}

/// Chzzk API 설정 (서킷 브레이커 포함)
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct ChzzkConfig {
    pub base_url: String,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

/// Twitch API 설정 (OAuth2 + 서킷 브레이커)
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct TwitchConfig {
    pub base_url: String,
    pub auth_url: String,
    pub client_id: String,
    pub client_secret: SecretString,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

/// Iris 메시지 발송 클라이언트 설정
#[derive(Debug, Clone, Deserialize, Validate)]
pub struct IrisConfig {
    pub base_url: String,
    pub bot_token: SecretString,
    #[validate(range(min = 1))]
    pub timeout_secs: u64,
    #[validate(range(min = 1))]
    pub circuit_failure_threshold: u32,
    #[validate(range(min = 1))]
    pub circuit_reset_secs: u64,
}

/// Iris base_url 보안 정책:
/// - https는 항상 허용
/// - http는 내부망/로컬 주소만 허용 (공개망 HTTP 차단)
pub fn validate_iris_base_url_policy(base_url: &str) -> Result<(), String> {
    let parsed = url::Url::parse(base_url).map_err(|e| format!("iris.base_url 파싱 실패: {e}"))?;

    match parsed.scheme() {
        "https" => Ok(()),
        "http" => validate_http_host_is_private_or_internal(&parsed),
        scheme => Err(format!(
            "iris.base_url scheme '{scheme}' is not allowed (http/https only)"
        )),
    }
}

fn validate_http_host_is_private_or_internal(parsed: &url::Url) -> Result<(), String> {
    let Some(host) = parsed.host_str() else {
        return Err("iris.base_url host is missing".to_owned());
    };

    // IP 호스트: 사설/루프백/링크로컬만 허용
    if let Ok(ip) = host.parse::<IpAddr>() {
        if is_private_or_local_ip(ip) {
            return Ok(());
        }
        return Err(format!(
            "public HTTP endpoint is blocked for iris.base_url: {host}"
        ));
    }

    let normalized = host.trim_end_matches('.').to_ascii_lowercase();
    if normalized == "localhost" || normalized.ends_with(".localhost") {
        return Ok(());
    }

    // dot 없는 단일 라벨(hostname)은 내부 서비스 DNS로 간주
    if !normalized.contains('.') {
        return Ok(());
    }

    // 내부 DNS suffix 허용
    if normalized.ends_with(".local")
        || normalized.ends_with(".internal")
        || normalized.ends_with(".svc")
        || normalized.ends_with(".cluster.local")
    {
        return Ok(());
    }

    Err(format!(
        "public HTTP endpoint is blocked for iris.base_url: {host}"
    ))
}

fn is_private_or_local_ip(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => {
            v4.is_private() || v4.is_loopback() || v4.is_link_local() || v4.is_unspecified()
        }
        IpAddr::V6(v6) => {
            v6.is_loopback()
                || v6.is_unique_local()
                || v6.is_unicast_link_local()
                || v6.is_unspecified()
        }
    }
}
