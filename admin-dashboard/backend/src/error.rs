use axum::Json;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use utoipa::ToSchema;

#[derive(Debug, thiserror::Error)]
pub enum AppError {
    #[error(transparent)]
    Auth(#[from] AuthError),
    #[error(transparent)]
    Docker(#[from] DockerError),
    #[error(transparent)]
    Proxy(#[from] ProxyError),
    #[error("internal error: {0}")]
    Internal(#[from] anyhow::Error),
}

#[derive(Debug, thiserror::Error)]
#[allow(dead_code)]
pub enum AuthError {
    #[error("unauthorized")]
    Unauthorized,
    #[error("session expired")]
    SessionExpired,
    #[error("session absolute expired")]
    AbsoluteExpired,
    #[error("rate limited")]
    RateLimited { retry_after_secs: u64 },
    #[error("csrf violation")]
    CsrfViolation,
    #[error("session store unavailable")]
    StoreUnavailable,
}

#[derive(Debug, thiserror::Error)]
pub enum DockerError {
    #[error("docker unavailable")]
    Unavailable,
    #[error("container not managed: {0}")]
    NotManaged(String),
    #[error("docker error: {0}")]
    Internal(String),
}

#[derive(Debug, thiserror::Error)]
pub enum ProxyError {
    #[error("upstream unavailable")]
    Unavailable,
    #[error("upstream returned {status}")]
    Upstream {
        status: StatusCode,
        body: ErrorResponse,
    },
}

#[derive(Debug, Clone, Serialize, Deserialize, ToSchema, PartialEq, Eq)]
pub struct ErrorResponse {
    pub error: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub code: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub absolute_expired: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub retry_after: Option<u64>,
}

impl ErrorResponse {
    pub fn simple(message: impl Into<String>) -> Self {
        Self {
            error: message.into(),
            code: None,
            details: None,
            absolute_expired: None,
            retry_after: None,
        }
    }

    pub fn from_value(value: Value, fallback: impl Into<String>) -> Self {
        let fallback = fallback.into();
        match value {
            Value::Object(mut object) => {
                let error = object
                    .remove("error")
                    .and_then(|value| value.as_str().map(str::to_string))
                    .unwrap_or_else(|| fallback.clone());
                let code = object
                    .remove("code")
                    .and_then(|value| value.as_str().map(str::to_string));
                let details = match object.remove("details") {
                    Some(details) => Some(details),
                    None if !object.is_empty() => Some(Value::Object(object)),
                    None => None,
                };

                Self {
                    error,
                    code,
                    details,
                    absolute_expired: None,
                    retry_after: None,
                }
            }
            Value::String(text) => Self::simple(text),
            Value::Null => Self::simple(fallback),
            other => Self {
                error: fallback,
                code: None,
                details: Some(other),
                absolute_expired: None,
                retry_after: None,
            },
        }
    }
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, body) = match &self {
            Self::Auth(e) => match e {
                AuthError::Unauthorized | AuthError::SessionExpired => {
                    (StatusCode::UNAUTHORIZED, json!({"error": "Unauthorized"}))
                }
                AuthError::AbsoluteExpired => (
                    StatusCode::UNAUTHORIZED,
                    json!({"error": "Session expired", "absolute_expired": true}),
                ),
                AuthError::RateLimited { retry_after_secs } => (
                    StatusCode::TOO_MANY_REQUESTS,
                    json!({"error": "Too many login attempts", "retry_after": retry_after_secs}),
                ),
                AuthError::CsrfViolation => (StatusCode::FORBIDDEN, json!({"error": "Forbidden"})),
                AuthError::StoreUnavailable => (
                    StatusCode::SERVICE_UNAVAILABLE,
                    json!({"error": "Session store unavailable"}),
                ),
            },
            Self::Docker(e) => match e {
                DockerError::Unavailable => (
                    StatusCode::SERVICE_UNAVAILABLE,
                    json!({"error": "Docker service not available"}),
                ),
                DockerError::NotManaged(_name) => (
                    StatusCode::NOT_FOUND,
                    json!({"error": "container not found"}),
                ),
                DockerError::Internal(_) => (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    json!({"error": "An internal error occurred"}),
                ),
            },
            Self::Proxy(e) => match e {
                ProxyError::Unavailable => (
                    StatusCode::BAD_GATEWAY,
                    json!({"error": "Service unavailable"}),
                ),
                ProxyError::Upstream { status, body } => {
                    return (*status, Json(body)).into_response();
                }
            },
            Self::Internal(e) => {
                tracing::error!(error = %e, "internal error");
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    json!({"error": "An internal error occurred"}),
                )
            }
        };
        (status, Json(body)).into_response()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::to_bytes;
    use axum::http::StatusCode;
    use serde_json::json;

    #[test]
    fn test_auth_error_unauthorized_status() {
        let err = AppError::Auth(AuthError::Unauthorized);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    }

    #[test]
    fn test_session_expired_status() {
        let err = AppError::Auth(AuthError::SessionExpired);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    }

    #[test]
    fn test_absolute_expired_status() {
        let err = AppError::Auth(AuthError::AbsoluteExpired);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    }

    #[test]
    fn test_rate_limited_status() {
        let err = AppError::Auth(AuthError::RateLimited {
            retry_after_secs: 900,
        });
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::TOO_MANY_REQUESTS);
    }

    #[test]
    fn test_csrf_violation_status() {
        let err = AppError::Auth(AuthError::CsrfViolation);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::FORBIDDEN);
    }

    #[test]
    fn test_store_unavailable_status() {
        let err = AppError::Auth(AuthError::StoreUnavailable);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
    }

    #[test]
    fn test_docker_unavailable_status() {
        let err = AppError::Docker(DockerError::Unavailable);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
    }

    #[test]
    fn test_docker_not_managed_status() {
        let err = AppError::Docker(DockerError::NotManaged("foo".into()));
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }

    #[test]
    fn test_docker_internal_status() {
        let err = AppError::Docker(DockerError::Internal("boom".into()));
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::INTERNAL_SERVER_ERROR);
    }

    #[test]
    fn test_proxy_unavailable_status() {
        let err = AppError::Proxy(ProxyError::Unavailable);
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::BAD_GATEWAY);
    }

    #[tokio::test]
    async fn test_proxy_upstream_client_preserves_status_and_body() {
        let err = AppError::Proxy(ProxyError::Upstream {
            status: StatusCode::BAD_REQUEST,
            body: ErrorResponse {
                error: "invalid filter".into(),
                code: Some("bad_filter".into()),
                details: Some(json!({"field": "org"})),
                absolute_expired: None,
                retry_after: None,
            },
        });
        let response = err.into_response();
        assert_eq!(response.status(), StatusCode::BAD_REQUEST);

        let body = to_bytes(response.into_body(), usize::MAX)
            .await
            .expect("response body");
        let parsed: serde_json::Value = serde_json::from_slice(&body).expect("json body");
        assert_eq!(
            parsed,
            json!({"error": "invalid filter", "code": "bad_filter", "details": {"field": "org"}})
        );
    }

    #[test]
    fn test_error_response_from_value_promotes_unknown_fields_to_details() {
        let response =
            ErrorResponse::from_value(json!({"code": "bad_filter", "field": "org"}), "Bad Request");

        assert_eq!(response.error, "Bad Request");
        assert_eq!(response.code.as_deref(), Some("bad_filter"));
        assert_eq!(response.details, Some(json!({"field": "org"})));
    }
}
