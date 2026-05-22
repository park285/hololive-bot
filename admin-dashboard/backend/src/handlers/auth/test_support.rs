use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex, OnceLock};

use axum::body::Body;
use axum::extract::State;
use axum::http::{Request as HttpRequest, header};
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

use super::heartbeat::handle_heartbeat;

#[derive(Clone, Default)]
struct FakeValkeyState {
    entries: HashMap<String, String>,
    commands: Vec<String>,
}

pub struct FakeValkey {
    addr: SocketAddr,
    state: Arc<Mutex<FakeValkeyState>>,
    _server: JoinHandle<()>,
}

enum RespValue {
    Array(Vec<Self>),
    Bulk(Option<Vec<u8>>),
    Simple(String),
    Integer(i64),
}

impl FakeValkey {
    pub async fn start() -> Self {
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

    pub fn url(&self) -> String {
        self.addr.to_string()
    }

    pub fn insert_session(&self, session: &Session) {
        let mut state = self.state.lock().expect("fake valkey lock");
        state.entries.insert(
            session_key(&session.id),
            serde_json::to_string(session).expect("serialize session"),
        );
    }

    pub fn commands(&self) -> Vec<String> {
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
                write_simple_string(&mut writer, "OK").await?;
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
                write_error(&mut writer, &format!("ERR unsupported command {command}")).await?;
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
    match args.get(1).map(String::as_str) {
        Some("1") => write_refresh_cas_eval_result(writer, state, args).await,
        Some("2") => write_rotate_cas_eval_result(writer, state, args).await,
        _ => write_error(writer, "ERR invalid eval arguments").await,
    }
}

async fn write_refresh_cas_eval_result(
    writer: &mut tokio::net::tcp::OwnedWriteHalf,
    state: &Arc<Mutex<FakeValkeyState>>,
    args: &[String],
) -> std::io::Result<()> {
    if args.len() < 6 {
        return write_error(writer, "ERR invalid refresh eval arguments").await;
    }

    let key = &args[2];
    let expected_data = &args[3];
    let refreshed_data = &args[4];

    let result = {
        let mut locked = state.lock().expect("fake valkey lock");
        let current_data = locked.entries.get(key).cloned();
        match current_data {
            None => 0,
            Some(current_data) if current_data != expected_data.as_str() => -1,
            Some(_) => {
                locked.entries.insert(key.clone(), refreshed_data.clone());
                1
            }
        }
    };

    write_integer(writer, result).await
}

async fn write_rotate_cas_eval_result(
    writer: &mut tokio::net::tcp::OwnedWriteHalf,
    state: &Arc<Mutex<FakeValkeyState>>,
    args: &[String],
) -> std::io::Result<()> {
    if args.len() < 9 {
        return write_error(writer, "ERR invalid rotation eval arguments").await;
    }

    let old_key = &args[2];
    let new_key = &args[3];
    let new_data = &args[4];
    let old_marker_data = &args[5];
    let expected_old_data = &args[8];

    let old_value = {
        let mut locked = state.lock().expect("fake valkey lock");
        match locked.entries.get(old_key).cloned() {
            Some(old_value) if old_value.as_str() == expected_old_data.as_str() => {
                locked.entries.insert(new_key.clone(), new_data.clone());
                locked
                    .entries
                    .insert(old_key.clone(), old_marker_data.clone());
                Some(old_value)
            }
            _ => None,
        }
    };

    write_bulk_string(writer, old_value.as_deref()).await
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
            b'+' | b'-' => RespValue::Simple(read_resp_line(reader).await?),
            b':' => RespValue::Integer(
                read_resp_line(reader)
                    .await?
                    .parse::<i64>()
                    .expect("integer"),
            ),
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
            b"%7\r\n+server\r\n+valkey\r\n+version\r\n+7.2.0\r\n+proto\r\n:3\r\n+id\r\n:1\r\n+mode\r\n+standalone\r\n+role\r\n+master\r\n+modules\r\n*0\r\n",
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

pub fn test_state_with_session_config(
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

pub fn test_state(valkey_url: String) -> Arc<AppState> {
    test_state_with_session_config(valkey_url, SessionConfig::default())
}

pub fn build_session(
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

pub async fn call_heartbeat(
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

    match handle_heartbeat(State(state), req).await {
        Ok(response) => response.into_response(),
        Err(error) => error.into_response(),
    }
}
