use shared_core::error::SharedError;
use thiserror::Error;

/// 알람 도메인 에러 카테고리
#[derive(Error, Debug)]
pub enum AlarmError {
    /// Valkey(Redis) 작업 실패
    #[error("valkey error: {0}")]
    Valkey(String),

    /// HTTP 요청 실패
    #[error("HTTP error: {0}")]
    Http(String),

    /// 데이터베이스 작업 실패
    #[error("database error: {0}")]
    Database(String),

    /// 외부 플랫폼 API 오류 (platform: 대상 플랫폼, message: 상세 메시지)
    #[error("API error ({platform}): {message}")]
    Api { platform: String, message: String },

    /// 서킷 브레이커 OPEN 상태 — 해당 플랫폼 요청 차단 중
    #[error("circuit breaker open for {platform}")]
    CircuitOpen { platform: String },

    /// 설정 오류 (잘못된 값, 누락 필드 등)
    #[error("config error: {0}")]
    Config(String),

    /// JSON 직렬화/역직렬화 오류
    #[error("serialization error: {0}")]
    Serialization(#[from] serde_json::Error),
}

impl From<SharedError> for AlarmError {
    fn from(err: SharedError) -> Self {
        match err {
            SharedError::Valkey(msg) => Self::Valkey(msg),
            SharedError::Http(msg) => Self::Http(msg),
            SharedError::HttpStatus { code, message } => {
                Self::Http(format!("status {code}: {message}"))
            }
            SharedError::Database(msg) => Self::Database(msg),
            SharedError::Config(msg) => Self::Config(msg),
            SharedError::Serialization(err) => Self::Serialization(err),
            SharedError::Api { platform, message } => Self::Api { platform, message },
            SharedError::CircuitOpen { platform } => Self::CircuitOpen { platform },
            SharedError::NotFound(msg) => Self::Http(format!("not found: {msg}")),
            SharedError::Unauthorized(msg) => Self::Http(format!("unauthorized: {msg}")),
            SharedError::Forbidden(msg) => Self::Http(format!("forbidden: {msg}")),
            SharedError::Conflict(msg) => Self::Http(format!("conflict: {msg}")),
            SharedError::Io(err) => Self::Http(format!("io: {err}")),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn error_display_valkey() {
        let e = AlarmError::Valkey("connection refused".into());
        assert_eq!(e.to_string(), "valkey error: connection refused");
    }

    #[test]
    fn error_display_http() {
        let e = AlarmError::Http("timeout".into());
        assert_eq!(e.to_string(), "HTTP error: timeout");
    }

    #[test]
    fn error_display_api() {
        let e = AlarmError::Api {
            platform: "holodex".into(),
            message: "rate limited".into(),
        };
        assert_eq!(e.to_string(), "API error (holodex): rate limited");
    }

    #[test]
    fn error_display_circuit_open() {
        let e = AlarmError::CircuitOpen {
            platform: "chzzk".into(),
        };
        assert_eq!(e.to_string(), "circuit breaker open for chzzk");
    }

    #[test]
    fn error_from_serde_json() {
        let raw = serde_json::from_str::<serde_json::Value>("{invalid}");
        assert!(raw.is_err());
        let e = AlarmError::Serialization(raw.unwrap_err());
        assert!(e.to_string().starts_with("serialization error:"));
    }
}
