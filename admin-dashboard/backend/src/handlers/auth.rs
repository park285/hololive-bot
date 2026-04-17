use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use axum::Json;
use axum::extract::{ConnectInfo, Request, State};
use axum::http::HeaderMap;
use axum::response::IntoResponse;
use serde::{Deserialize, Serialize};
use tracing::{info, warn};
use utoipa::ToSchema;

use crate::auth::SessionId;
use crate::auth::session::{SessionProvider, SessionRefreshResult};
use crate::error::{AppError, AuthError};
use crate::state::AppState;

#[derive(Debug, Deserialize, ToSchema)]
pub struct LoginRequest {
    pub username: String,
    pub password: String,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct LoginResponse {
    pub status: String,
    pub message: String,
    pub csrf_token: String,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct SessionStatusResponse {
    pub status: String,
    pub authenticated: bool,
    pub username: String,
    pub absolute_expires_at: i64,
    pub session_policy: SessionPolicyResponse,
}

#[allow(clippy::struct_field_names)]
#[derive(Debug, Serialize, ToSchema)]
pub struct SessionPolicyResponse {
    pub heartbeat_interval_ms: u64,
    pub idle_timeout_ms: u64,
    pub idle_warning_timeout_ms: u64,
    pub idle_session_ttl_ms: u64,
    pub absolute_warning_window_ms: u64,
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/login",
    request_body = LoginRequest,
    responses(
        (status = 200, description = "Login successful", body = LoginResponse),
        (status = 401, description = "Authentication failed"),
        (status = 429, description = "Rate limited")
    ),
    tag = "auth"
)]
pub async fn handle_login(
    State(state): State<Arc<AppState>>,
    ConnectInfo(addr): ConnectInfo<SocketAddr>,
    headers: HeaderMap,
    Json(req): Json<LoginRequest>,
) -> Result<impl IntoResponse, AppError> {
    let ip = addr.ip().to_string();
    let forwarded_for = headers
        .get("x-forwarded-for")
        .and_then(|value| value.to_str().ok())
        .unwrap_or("-");

    let (allowed, remaining) = state.rate_limiter.is_allowed(&ip);
    if !allowed {
        warn!(
            ip = %ip,
            forwarded_for,
            retry_after_secs = remaining.as_secs(),
            "admin login rate limited"
        );
        return Err(AuthError::RateLimited {
            retry_after_secs: remaining.as_secs(),
        }
        .into());
    }

    if req.username != state.config.admin_user {
        let count = state.rate_limiter.record_failure(&ip);
        let delay = std::cmp::min(count as u64 * 500, 3000);
        warn!(
            ip = %ip,
            forwarded_for,
            delay_ms = delay,
            "admin login failed: invalid username"
        );
        tokio::time::sleep(Duration::from_millis(delay)).await;
        return Err(AuthError::Unauthorized.into());
    }

    let valid = bcrypt::verify(&req.password, &state.config.admin_pass_hash).unwrap_or(false);
    if !valid {
        let count = state.rate_limiter.record_failure(&ip);
        let delay = std::cmp::min(count as u64 * 500, 3000);
        warn!(
            ip = %ip,
            forwarded_for,
            username = %state.config.admin_user,
            delay_ms = delay,
            "admin login failed: invalid password"
        );
        tokio::time::sleep(Duration::from_millis(delay)).await;
        return Err(AuthError::Unauthorized.into());
    }

    state.rate_limiter.record_success(&ip);
    let session = state
        .sessions
        .create_session()
        .await
        .map_err(|_| AuthError::StoreUnavailable)?;

    let signed = crate::auth::sign_session_id(&session.id, &state.config.session_secret);
    let csrf_token =
        crate::middleware::csrf::new_csrf_token(&session.id, &state.config.session_secret);
    info!(
        ip = %ip,
        forwarded_for,
        username = %state.config.admin_user,
        "admin login succeeded"
    );

    let mut response = Json(LoginResponse {
        status: "ok".to_string(),
        message: "Login successful".to_string(),
        csrf_token: csrf_token.clone(),
    })
    .into_response();

    crate::auth::middleware::set_session_cookie(
        response.headers_mut(),
        "admin_session",
        &signed,
        state.config.session.expiry_duration.as_secs(),
        crate::auth::middleware::should_set_secure_cookie(
            &headers,
            state.config.security.force_https,
        ),
    );
    crate::auth::middleware::set_csrf_cookie(
        response.headers_mut(),
        &csrf_token,
        crate::auth::middleware::should_set_secure_cookie(
            &headers,
            state.config.security.force_https,
        ),
    );

    Ok(response)
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/logout",
    responses(
        (status = 200, description = "Logout successful")
    ),
    tag = "auth"
)]
pub async fn handle_logout(State(state): State<Arc<AppState>>, req: Request) -> impl IntoResponse {
    if let Some(session_id) = req.extensions().get::<SessionId>() {
        state.sessions.delete_session(&session_id.0).await;
    }

    let mut response = Json(serde_json::json!({"status": "ok"})).into_response();
    clear_auth_cookies(
        response.headers_mut(),
        crate::auth::middleware::should_set_secure_cookie(
            req.headers(),
            state.config.security.force_https,
        ),
    );
    response
}

#[utoipa::path(
    get,
    path = "/admin/api/auth/session",
    responses(
        (status = 200, description = "Session is valid", body = SessionStatusResponse),
        (status = 401, description = "Unauthorized"),
        (status = 503, description = "Session store unavailable")
    ),
    tag = "auth"
)]
pub async fn handle_session_status(
    State(state): State<Arc<AppState>>,
    req: Request,
) -> Result<impl IntoResponse, AppError> {
    let session_id = req
        .extensions()
        .get::<SessionId>()
        .ok_or(AuthError::Unauthorized)?
        .0
        .clone();

    let session = state
        .sessions
        .get_session(&session_id)
        .await
        .map_err(|_| AuthError::StoreUnavailable)?
        .ok_or(AuthError::Unauthorized)?;

    Ok(Json(SessionStatusResponse {
        status: "ok".to_string(),
        authenticated: true,
        username: state.config.admin_user.clone(),
        absolute_expires_at: session.absolute_expires_at.timestamp(),
        session_policy: SessionPolicyResponse {
            heartbeat_interval_ms: state.config.session.heartbeat_interval.as_millis() as u64,
            idle_timeout_ms: state.config.session.idle_timeout.as_millis() as u64,
            idle_warning_timeout_ms: state.config.session.idle_warning_timeout.as_millis() as u64,
            idle_session_ttl_ms: state.config.session.idle_session_ttl.as_millis() as u64,
            absolute_warning_window_ms: state.config.session.absolute_warning_window.as_millis()
                as u64,
        },
    }))
}

#[derive(Debug, Deserialize, ToSchema)]
pub struct HeartbeatRequest {
    #[serde(default)]
    pub idle: bool,
}

#[derive(Debug, Serialize, ToSchema)]
pub struct HeartbeatResponse {
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub rotated: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub absolute_expires_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub csrf_token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub idle_rejected: Option<bool>,
}

fn clear_auth_cookies(headers: &mut HeaderMap, secure_cookie: bool) {
    crate::auth::middleware::set_clear_cookie(headers, "admin_session", secure_cookie);
    crate::auth::middleware::set_clear_cookie(headers, "csrf_token", false);
}

#[utoipa::path(
    post,
    path = "/admin/api/auth/heartbeat",
    request_body = HeartbeatRequest,
    responses(
        (status = 200, description = "Heartbeat processed", body = HeartbeatResponse),
        (status = 401, description = "Session expired or unauthorized")
    ),
    tag = "auth"
)]
pub async fn handle_heartbeat(
    State(state): State<Arc<AppState>>,
    req: Request,
) -> Result<impl IntoResponse, AppError> {
    let secure_cookie = crate::auth::middleware::should_set_secure_cookie(
        req.headers(),
        state.config.security.force_https,
    );
    let session_id = req
        .extensions()
        .get::<SessionId>()
        .map(|s| s.0.clone())
        .ok_or(AuthError::Unauthorized)?;

    let body = axum::body::to_bytes(req.into_body(), 1024)
        .await
        .map_err(|e| anyhow::anyhow!("body read failed: {e}"))?;
    let hb: HeartbeatRequest =
        serde_json::from_slice(&body).unwrap_or(HeartbeatRequest { idle: false });

    let refresh_result = state
        .sessions
        .refresh_session_with_validation(&session_id, hb.idle)
        .await
        .map_err(|_| AuthError::StoreUnavailable)?;

    match refresh_result {
        SessionRefreshResult::Refreshed => {}
        SessionRefreshResult::IdleShortened => {
            return Ok(Json(HeartbeatResponse {
                status: "idle".to_string(),
                idle_rejected: Some(true),
                rotated: None,
                absolute_expires_at: None,
                csrf_token: None,
            })
            .into_response());
        }
        SessionRefreshResult::AbsoluteExpired => {
            return Ok(
                crate::auth::middleware::auth_error_response_with_cookie_clear(
                    AuthError::AbsoluteExpired,
                    secure_cookie,
                ),
            );
        }
        SessionRefreshResult::Missing | SessionRefreshResult::NotRefreshable => {
            return Ok(
                crate::auth::middleware::auth_error_response_with_cookie_clear(
                    AuthError::Unauthorized,
                    secure_cookie,
                ),
            );
        }
    }

    if state.config.session.token_rotation_enabled
        && let Ok(Some(new_session)) = state.sessions.rotate_session(&session_id).await
    {
        let new_signed =
            crate::auth::sign_session_id(&new_session.id, &state.config.session_secret);
        let new_csrf =
            crate::middleware::csrf::new_csrf_token(&new_session.id, &state.config.session_secret);

        let mut response = Json(HeartbeatResponse {
            status: "ok".to_string(),
            rotated: Some(true),
            absolute_expires_at: Some(new_session.absolute_expires_at.timestamp()),
            csrf_token: Some(new_csrf.clone()),
            idle_rejected: None,
        })
        .into_response();

        crate::auth::middleware::set_session_cookie(
            response.headers_mut(),
            "admin_session",
            &new_signed,
            state.config.session.expiry_duration.as_secs(),
            secure_cookie,
        );
        crate::auth::middleware::set_csrf_cookie(response.headers_mut(), &new_csrf, secure_cookie);
        return Ok(response);
    }

    Ok(Json(HeartbeatResponse {
        status: "ok".to_string(),
        rotated: None,
        absolute_expires_at: None,
        csrf_token: None,
        idle_rejected: None,
    })
    .into_response())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::net::SocketAddr;
    use std::sync::{Arc, Mutex, OnceLock};

    use axum::body::{Body, to_bytes};
    use axum::http::{Request as HttpRequest, StatusCode, header};
    use axum::response::IntoResponse;
    use deadpool_redis::Runtime;
    use tokio::io::{AsyncBufReadExt, AsyncRead, AsyncReadExt, AsyncWriteExt, BufReader};
    use tokio::net::{TcpListener, TcpStream};
    use tokio::task::JoinHandle;

    use crate::auth::SessionId;
    use crate::auth::rate_limiter::LoginRateLimiter;
    use crate::auth::session::{Session, ValkeySessionStore, session_key};
    use crate::config::{Config, SecurityConfig, SecurityMode, SessionConfig};
    use crate::holo::client::HoloApiClient;
    use crate::state::AppState;
    use crate::status::{StatusCollector, SystemStats};

    #[derive(Clone, Default)]
    struct FakeValkeyState {
        entries: HashMap<String, String>,
        commands: Vec<String>,
    }

    struct FakeValkey {
        addr: SocketAddr,
        state: Arc<Mutex<FakeValkeyState>>,
        _server: JoinHandle<()>,
    }

    enum RespValue {
        Array(Vec<RespValue>),
        Bulk(Option<Vec<u8>>),
        Simple(String),
        Integer(i64),
    }

    impl FakeValkey {
        async fn start() -> Self {
            let listener = TcpListener::bind(("127.0.0.1", 0))
                .await
                .expect("bind fake valkey");
            let addr = listener.local_addr().expect("fake valkey addr");
            let state = Arc::new(Mutex::new(FakeValkeyState::default()));
            let server_state = Arc::clone(&state);
            let server = tokio::spawn(async move {
                while let Ok((stream, _)) = listener.accept().await {
                    let connection_state = Arc::clone(&server_state);
                    tokio::spawn(async move {
                        let _ = handle_fake_valkey_connection(stream, connection_state).await;
                    });
                }
            });

            Self {
                addr,
                state,
                _server: server,
            }
        }

        fn url(&self) -> String {
            self.addr.to_string()
        }

        fn insert_session(&self, session: &Session) {
            let mut state = self.state.lock().expect("fake valkey lock");
            state.entries.insert(
                session_key(&session.id),
                serde_json::to_string(session).expect("serialize session"),
            );
        }

        fn commands(&self) -> Vec<String> {
            self.state
                .lock()
                .expect("fake valkey lock")
                .commands
                .clone()
        }
    }

    async fn handle_fake_valkey_connection(
        stream: TcpStream,
        state: Arc<Mutex<FakeValkeyState>>,
    ) -> std::io::Result<()> {
        let (reader, mut writer) = stream.into_split();
        let mut reader = BufReader::new(reader);

        while let Some(frame) = read_resp_value(&mut reader).await? {
            let RespValue::Array(items) = frame else {
                write_simple_string(&mut writer, "OK").await?;
                continue;
            };

            let Some(command) = items.first().and_then(resp_to_string) else {
                write_error(&mut writer, "ERR empty command").await?;
                continue;
            };
            let command = command.to_ascii_uppercase();
            let args: Vec<String> = items.iter().skip(1).filter_map(resp_to_string).collect();

            state
                .lock()
                .expect("fake valkey lock")
                .commands
                .push(if args.is_empty() {
                    command.clone()
                } else {
                    format!("{} {}", command, args.join(" "))
                });

            match command.as_str() {
                "HELLO" => write_hello(&mut writer).await?,
                "CLIENT" | "PING" | "SETINFO" | "SELECT" => {
                    write_simple_string(&mut writer, "OK").await?
                }
                "QUIT" => {
                    write_simple_string(&mut writer, "OK").await?;
                    return Ok(());
                }
                "GET" => {
                    let value = args.first().and_then(|key| {
                        state
                            .lock()
                            .expect("fake valkey lock")
                            .entries
                            .get(key)
                            .cloned()
                    });
                    write_bulk_string(&mut writer, value.as_deref()).await?;
                }
                "SETEX" => {
                    if let [key, _, value, ..] = args.as_slice() {
                        state
                            .lock()
                            .expect("fake valkey lock")
                            .entries
                            .insert(key.clone(), value.clone());
                        write_simple_string(&mut writer, "OK").await?;
                    } else {
                        write_error(&mut writer, "ERR wrong number of arguments for SETEX").await?;
                    }
                }
                "DEL" => {
                    let removed = args
                        .iter()
                        .filter(|key| {
                            state
                                .lock()
                                .expect("fake valkey lock")
                                .entries
                                .remove(*key)
                                .is_some()
                        })
                        .count();
                    write_integer(&mut writer, removed as i64).await?;
                }
                "SCRIPT" => {
                    if args.first().map(String::as_str) == Some("LOAD") {
                        write_bulk_string(&mut writer, Some("test-script-sha")).await?;
                    } else {
                        write_error(&mut writer, "ERR unsupported SCRIPT command").await?;
                    }
                }
                "EVAL" | "EVALSHA" => {
                    write_eval_result(&mut writer, &state, &args).await?;
                }
                _ => {
                    write_error(&mut writer, &format!("ERR unsupported command {command}")).await?
                }
            }
        }

        Ok(())
    }

    async fn write_eval_result(
        writer: &mut tokio::net::tcp::OwnedWriteHalf,
        state: &Arc<Mutex<FakeValkeyState>>,
        args: &[String],
    ) -> std::io::Result<()> {
        if args.len() < 8 {
            return write_error(writer, "ERR invalid eval arguments").await;
        }

        let old_key = &args[2];
        let new_key = &args[3];
        let new_data = &args[4];
        let old_marker_data = &args[5];

        let old_value = {
            state
                .lock()
                .expect("fake valkey lock")
                .entries
                .get(old_key)
                .cloned()
        };

        match old_value {
            Some(old_value) => {
                {
                    let mut locked = state.lock().expect("fake valkey lock");
                    locked.entries.insert(new_key.clone(), new_data.clone());
                    locked
                        .entries
                        .insert(old_key.clone(), old_marker_data.clone());
                }
                write_bulk_string(writer, Some(&old_value)).await
            }
            None => write_bulk_string(writer, None).await,
        }
    }

    async fn read_resp_value<R>(reader: &mut BufReader<R>) -> std::io::Result<Option<RespValue>>
    where
        R: AsyncRead + Unpin,
    {
        let mut prefix = [0u8; 1];
        match reader.read_exact(&mut prefix).await {
            Ok(_) => {}
            Err(err) if err.kind() == std::io::ErrorKind::UnexpectedEof => return Ok(None),
            Err(err) => return Err(err),
        }

        if prefix[0] != b'*' {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("unsupported RESP prefix: {}", prefix[0] as char),
            ));
        }

        let len = read_resp_line(reader)
            .await?
            .parse::<usize>()
            .expect("array len");
        let mut items = Vec::with_capacity(len);
        for _ in 0..len {
            let mut item_prefix = [0u8; 1];
            reader.read_exact(&mut item_prefix).await?;
            let item = match item_prefix[0] {
                b'$' => {
                    let bulk_len = read_resp_line(reader)
                        .await?
                        .parse::<i64>()
                        .expect("bulk len");
                    if bulk_len < 0 {
                        RespValue::Bulk(None)
                    } else {
                        let mut buf = vec![0u8; bulk_len as usize];
                        reader.read_exact(&mut buf).await?;
                        let mut crlf = [0u8; 2];
                        reader.read_exact(&mut crlf).await?;
                        RespValue::Bulk(Some(buf))
                    }
                }
                b'+' => RespValue::Simple(read_resp_line(reader).await?),
                b':' => RespValue::Integer(
                    read_resp_line(reader)
                        .await?
                        .parse::<i64>()
                        .expect("integer"),
                ),
                b'-' => RespValue::Simple(read_resp_line(reader).await?),
                other => {
                    return Err(std::io::Error::new(
                        std::io::ErrorKind::InvalidData,
                        format!("unsupported RESP item prefix: {}", other as char),
                    ));
                }
            };
            items.push(item);
        }

        Ok(Some(RespValue::Array(items)))
    }

    async fn read_resp_line<R>(reader: &mut BufReader<R>) -> std::io::Result<String>
    where
        R: AsyncRead + Unpin,
    {
        let mut line = Vec::new();
        reader.read_until(b'\n', &mut line).await?;
        if line.ends_with(b"\r\n") {
            line.truncate(line.len() - 2);
        }
        Ok(String::from_utf8(line).expect("utf8 resp line"))
    }

    fn resp_to_string(value: &RespValue) -> Option<String> {
        match value {
            RespValue::Bulk(Some(bytes)) => {
                Some(String::from_utf8(bytes.clone()).expect("utf8 bulk"))
            }
            RespValue::Simple(text) => Some(text.clone()),
            RespValue::Integer(number) => Some(number.to_string()),
            RespValue::Bulk(None) | RespValue::Array(_) => None,
        }
    }

    async fn write_hello(writer: &mut tokio::net::tcp::OwnedWriteHalf) -> std::io::Result<()> {
        writer
            .write_all(
                b"%7\r\n+server\r\n+redis\r\n+version\r\n+7.2.0\r\n+proto\r\n:3\r\n+id\r\n:1\r\n+mode\r\n+standalone\r\n+role\r\n+master\r\n+modules\r\n*0\r\n",
            )
            .await
    }

    async fn write_simple_string(
        writer: &mut tokio::net::tcp::OwnedWriteHalf,
        value: &str,
    ) -> std::io::Result<()> {
        writer.write_all(format!("+{value}\r\n").as_bytes()).await
    }

    async fn write_error(
        writer: &mut tokio::net::tcp::OwnedWriteHalf,
        value: &str,
    ) -> std::io::Result<()> {
        writer.write_all(format!("-{value}\r\n").as_bytes()).await
    }

    async fn write_integer(
        writer: &mut tokio::net::tcp::OwnedWriteHalf,
        value: i64,
    ) -> std::io::Result<()> {
        writer.write_all(format!(":{value}\r\n").as_bytes()).await
    }

    async fn write_bulk_string(
        writer: &mut tokio::net::tcp::OwnedWriteHalf,
        value: Option<&str>,
    ) -> std::io::Result<()> {
        match value {
            Some(value) => {
                writer
                    .write_all(format!("${}\r\n{}\r\n", value.len(), value).as_bytes())
                    .await
            }
            None => writer.write_all(b"$-1\r\n").await,
        }
    }

    fn test_admin_pass_hash() -> &'static str {
        static HASH: OnceLock<String> = OnceLock::new();
        HASH.get_or_init(|| bcrypt::hash("testpass", bcrypt::DEFAULT_COST).expect("bcrypt hash"))
            .as_str()
    }

    fn test_state_with_session_config(
        valkey_url: String,
        session_config: SessionConfig,
    ) -> Arc<AppState> {
        let config = Config {
            port: 30190,
            env: "test".to_string(),
            log_level: "info".to_string(),
            admin_user: "admin".to_string(),
            admin_pass_hash: test_admin_pass_hash().to_string(),
            session_secret: "test-secret-key-minimum-length".to_string(),
            valkey_url,
            docker_host: "tcp://127.0.0.1:2375".to_string(),
            holo_admin_api_url: "http://127.0.0.1:30006".to_string(),
            holo_bot_api_key: String::new(),
            enable_openapi: true,
            enable_swagger_ui: true,
            log_dir: "/tmp/admin-dashboard-test-logs".to_string(),
            security: SecurityConfig {
                allowed_origins: vec!["http://localhost:5173".to_string()],
                allow_localhost_in_prod: true,
                csrf_mode: SecurityMode::Enforce,
                ws_origin_mode: SecurityMode::Enforce,
                force_https: false,
                tls_enabled: false,
                tls_cert_path: "/tmp/test.crt".to_string(),
                tls_key_path: "/tmp/test.key".to_string(),
            },
            session: session_config,
        };

        let pool = deadpool_redis::Config::from_url(format!("redis://{}", config.valkey_url))
            .create_pool(Some(Runtime::Tokio1))
            .expect("valkey pool creation failed");

        let sessions = ValkeySessionStore::new(pool, config.session.clone());
        let rate_limiter = Arc::new(LoginRateLimiter::new());
        let status_collector =
            StatusCollector::new(vec![], env!("CARGO_PKG_VERSION")).expect("status collector init");
        let (stats_tx, _) = tokio::sync::broadcast::channel::<SystemStats>(16);
        let holo_api = Arc::new(
            HoloApiClient::new(&config.holo_admin_api_url, None)
                .expect("holo api client init failed"),
        );

        Arc::new(AppState {
            config,
            sessions,
            rate_limiter,
            holo_api,
            docker_svc: None,
            status_collector,
            stats_tx,
        })
    }

    fn test_state(valkey_url: String) -> Arc<AppState> {
        test_state_with_session_config(valkey_url, SessionConfig::default())
    }

    fn build_session(
        session_id: &str,
        absolute_expires_at: chrono::DateTime<chrono::Utc>,
    ) -> Session {
        let now = chrono::Utc::now();
        Session {
            id: session_id.to_string(),
            created_at: now - chrono::Duration::hours(1),
            expires_at: now + chrono::Duration::minutes(30),
            absolute_expires_at,
            last_rotated_at: now - chrono::Duration::minutes(20),
            rotated_to: None,
        }
    }

    async fn call_heartbeat(
        state: Arc<AppState>,
        session_id: &str,
        body: &str,
    ) -> axum::response::Response {
        let mut req = HttpRequest::post("/admin/api/auth/heartbeat")
            .header(header::CONTENT_TYPE, "application/json")
            .body(Body::from(body.to_string()))
            .expect("heartbeat request");
        req.extensions_mut()
            .insert(SessionId(session_id.to_string()));

        handle_heartbeat(State(state), req)
            .await
            .expect("heartbeat response")
            .into_response()
    }

    #[tokio::test]
    async fn test_idle_heartbeat_returns_idle_rejected_success_contract() {
        let fake_valkey = FakeValkey::start().await;
        let state = test_state(fake_valkey.url());
        let session = build_session(
            "idle-heartbeat",
            chrono::Utc::now() + chrono::Duration::hours(1),
        );
        fake_valkey.insert_session(&session);

        let response = call_heartbeat(Arc::clone(&state), &session.id, r#"{"idle":true}"#).await;

        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), 4096)
            .await
            .expect("heartbeat body");
        let json: serde_json::Value = serde_json::from_slice(&body).expect("heartbeat json");
        assert_eq!(json["status"], "idle");
        assert_eq!(json["idle_rejected"], true);
        assert!(json.get("rotated").is_none());
        assert!(json.get("csrf_token").is_none());
    }

    #[tokio::test]
    async fn test_session_status_includes_warning_policy_and_absolute_expiry() {
        let fake_valkey = FakeValkey::start().await;
        let state = test_state(fake_valkey.url());
        let session = build_session(
            "session-status-policy",
            chrono::Utc::now() + chrono::Duration::hours(1),
        );
        let expected_absolute_expiry = session.absolute_expires_at.timestamp();
        fake_valkey.insert_session(&session);

        let mut req = HttpRequest::get("/admin/api/auth/session")
            .body(Body::empty())
            .expect("session status request");
        req.extensions_mut()
            .insert(SessionId(session.id.to_string()));

        let response = handle_session_status(State(Arc::clone(&state)), req)
            .await
            .expect("session status response")
            .into_response();

        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), 4096)
            .await
            .expect("session status body");
        let json: serde_json::Value = serde_json::from_slice(&body).expect("session status json");
        assert_eq!(json["status"], "ok");
        assert_eq!(json["absolute_expires_at"], expected_absolute_expiry);
        assert_eq!(
            json["session_policy"]["idle_timeout_ms"].as_u64(),
            Some(SessionConfig::default().idle_timeout.as_millis() as u64)
        );
        assert_eq!(
            json["session_policy"]["idle_warning_timeout_ms"].as_u64(),
            Some(SessionConfig::default().idle_warning_timeout.as_millis() as u64)
        );
        assert_eq!(
            json["session_policy"]["absolute_warning_window_ms"].as_u64(),
            Some(SessionConfig::default().absolute_warning_window.as_millis() as u64)
        );
    }

    #[tokio::test]
    async fn test_idle_heartbeat_does_not_rotate_or_emit_new_session_cookies() {
        let fake_valkey = FakeValkey::start().await;
        let state = test_state(fake_valkey.url());
        let session = build_session(
            "idle-no-rotate",
            chrono::Utc::now() + chrono::Duration::hours(1),
        );
        fake_valkey.insert_session(&session);

        let response = call_heartbeat(Arc::clone(&state), &session.id, r#"{"idle":true}"#).await;

        assert_eq!(response.status(), StatusCode::OK);
        assert!(
            response
                .headers()
                .get_all(header::SET_COOKIE)
                .iter()
                .next()
                .is_none()
        );
        let commands = fake_valkey.commands();
        assert!(
            commands
                .iter()
                .all(|command| !command.starts_with("SCRIPT LOAD"))
        );
        assert!(
            commands
                .iter()
                .all(|command| !command.starts_with("EVALSHA"))
        );
        assert!(commands.iter().all(|command| !command.starts_with("EVAL ")));
    }

    #[tokio::test]
    async fn test_rotated_heartbeat_includes_absolute_expiry_in_response() {
        let fake_valkey = FakeValkey::start().await;
        let state = test_state(fake_valkey.url());
        let session = build_session(
            "rotated-heartbeat",
            chrono::Utc::now() + chrono::Duration::hours(1),
        );
        let expected_absolute_expiry = session.absolute_expires_at.timestamp();
        fake_valkey.insert_session(&session);

        let response = call_heartbeat(Arc::clone(&state), &session.id, r#"{"idle":false}"#).await;

        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), 4096)
            .await
            .expect("heartbeat body");
        let json: serde_json::Value = serde_json::from_slice(&body).expect("heartbeat json");
        assert_eq!(json["status"], "ok");
        assert_eq!(json["rotated"], true);
        assert_eq!(json["absolute_expires_at"], expected_absolute_expiry);
        assert!(json["csrf_token"].is_string());
    }

    #[tokio::test]
    async fn test_absolute_expired_heartbeat_returns_json_and_clears_cookies() {
        let fake_valkey = FakeValkey::start().await;
        let state = test_state(fake_valkey.url());
        let session = build_session(
            "absolute-expired",
            chrono::Utc::now() - chrono::Duration::seconds(1),
        );
        fake_valkey.insert_session(&session);

        let response = call_heartbeat(Arc::clone(&state), &session.id, r#"{"idle":false}"#).await;

        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
        let cookie_headers = response.headers().get_all(header::SET_COOKIE);
        assert!(cookie_headers.iter().count() >= 2);
        let body = to_bytes(response.into_body(), 4096)
            .await
            .expect("heartbeat body");
        let json: serde_json::Value = serde_json::from_slice(&body).expect("heartbeat json");
        assert_eq!(json["error"], "Session expired");
        assert_eq!(json["absolute_expired"], true);
    }

    #[tokio::test]
    async fn test_login_sets_session_cookie_max_age_from_session_expiry() {
        let fake_valkey = FakeValkey::start().await;
        let custom_session_config = SessionConfig {
            expiry_duration: Duration::from_secs(45 * 60),
            ..SessionConfig::default()
        };
        let state = test_state_with_session_config(fake_valkey.url(), custom_session_config);

        let response = handle_login(
            State(Arc::clone(&state)),
            ConnectInfo(SocketAddr::from(([127, 0, 0, 1], 12345))),
            HeaderMap::new(),
            Json(LoginRequest {
                username: state.config.admin_user.clone(),
                password: "testpass".to_string(),
            }),
        )
        .await
        .expect("login response")
        .into_response();

        assert_eq!(response.status(), StatusCode::OK);
        let session_cookie = response
            .headers()
            .get_all(header::SET_COOKIE)
            .iter()
            .find_map(|value| {
                value.to_str().ok().filter(|cookie| {
                    cookie.starts_with("admin_session=") && cookie.contains("Max-Age=")
                })
            })
            .expect("session cookie");
        assert!(session_cookie.contains("Max-Age=2700"));
    }

    #[test]
    fn test_login_request_deserialize() {
        let json = r#"{"username":"admin","password":"pass"}"#;
        let req: LoginRequest = serde_json::from_str(json).unwrap();
        assert_eq!(req.username, "admin");
    }

    #[test]
    fn test_login_response_serialize() {
        let resp = LoginResponse {
            status: "ok".to_string(),
            message: "Login successful".to_string(),
            csrf_token: "token123".to_string(),
        };
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("csrf_token"));
    }

    #[test]
    fn test_heartbeat_request_defaults() {
        let json = r"{}";
        let req: HeartbeatRequest = serde_json::from_str(json).unwrap();
        assert!(!req.idle);
    }

    #[test]
    fn test_heartbeat_response_skip_none() {
        let resp = HeartbeatResponse {
            status: "ok".to_string(),
            rotated: None,
            absolute_expires_at: None,
            csrf_token: None,
            idle_rejected: None,
        };
        let json = serde_json::to_string(&resp).unwrap();
        assert!(!json.contains("rotated"));
        assert!(!json.contains("csrf_token"));
    }

    #[test]
    fn test_session_status_response_serialize() {
        let resp = SessionStatusResponse {
            status: "ok".to_string(),
            authenticated: true,
            username: "admin".to_string(),
            absolute_expires_at: 1_735_568_988,
            session_policy: SessionPolicyResponse {
                heartbeat_interval_ms: 300_000,
                idle_timeout_ms: 600_000,
                idle_warning_timeout_ms: 540_000,
                idle_session_ttl_ms: 10_000,
                absolute_warning_window_ms: 300_000,
            },
        };
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("authenticated"));
        assert!(json.contains("username"));
        assert!(json.contains("absolute_expires_at"));
        assert!(json.contains("session_policy"));
    }
}
