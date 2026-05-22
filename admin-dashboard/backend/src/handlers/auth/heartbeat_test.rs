use std::sync::Arc;

use axum::body::to_bytes;
use axum::http::{StatusCode, header};

use crate::auth::session::Session;
use crate::config::SessionConfig;

use super::heartbeat::{HeartbeatRequest, HeartbeatResponse};
use super::test_support::{
    FakeValkey, build_session, call_heartbeat, test_state, test_state_with_session_config,
};

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
        commands.iter().all(|command| {
            !(command.starts_with("EVALSHA") && command.contains(" 2 session:admin:")
                || command.starts_with("EVAL ") && command.contains(" 2 session:admin:"))
        }),
        "idle heartbeat must not run the rotation Lua script: {commands:?}"
    );
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
async fn test_malformed_heartbeat_returns_400_and_does_not_refresh() {
    let fake_valkey = FakeValkey::start().await;
    let state = test_state(fake_valkey.url());
    let session = build_session(
        "malformed-heartbeat",
        chrono::Utc::now() + chrono::Duration::hours(1),
    );
    fake_valkey.insert_session(&session);

    let response =
        call_heartbeat(Arc::clone(&state), &session.id, r#"{"idle":"not-a-bool"}"#).await;

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    let body = to_bytes(response.into_body(), 4096)
        .await
        .expect("heartbeat body");
    let json: serde_json::Value = serde_json::from_slice(&body).expect("heartbeat json");
    assert_eq!(json["code"], "bad_request");
}

#[tokio::test]
async fn test_stale_rotated_heartbeat_reissues_replacement_cookie_without_clearing() {
    let fake_valkey = FakeValkey::start().await;
    let state = test_state(fake_valkey.url());
    let now = chrono::Utc::now();
    let replacement = Session {
        id: "replacement-session".to_string(),
        created_at: now - chrono::Duration::minutes(20),
        expires_at: now + chrono::Duration::minutes(30),
        absolute_expires_at: now + chrono::Duration::hours(1),
        last_rotated_at: now,
        rotated_to: None,
    };
    let stale_marker = Session {
        id: "stale-session".to_string(),
        created_at: replacement.created_at,
        expires_at: now + chrono::Duration::seconds(30),
        absolute_expires_at: replacement.absolute_expires_at,
        last_rotated_at: now,
        rotated_to: Some(replacement.id.clone()),
    };
    fake_valkey.insert_session(&replacement);
    fake_valkey.insert_session(&stale_marker);

    let response =
        call_heartbeat(Arc::clone(&state), &stale_marker.id, r#"{"idle":false}"#).await;

    assert_eq!(response.status(), StatusCode::OK);
    let set_cookie_headers: Vec<_> = response
        .headers()
        .get_all(header::SET_COOKIE)
        .iter()
        .filter_map(|value| value.to_str().ok())
        .collect();
    assert!(
        set_cookie_headers
            .iter()
            .any(|cookie| cookie.starts_with("admin_session=") && !cookie.contains("Max-Age=0"))
    );
    assert!(
        set_cookie_headers
            .iter()
            .all(|cookie| !cookie.contains("Max-Age=0")),
        "stale rotated heartbeat must not clear auth cookies: {set_cookie_headers:?}"
    );

    let body = to_bytes(response.into_body(), 4096)
        .await
        .expect("heartbeat body");
    let json: serde_json::Value = serde_json::from_slice(&body).expect("heartbeat json");
    assert_eq!(json["status"], "ok");
    assert_eq!(json["rotated"], true);
    assert_eq!(
        json["absolute_expires_at"],
        replacement.absolute_expires_at.timestamp()
    );
    assert!(json["csrf_token"].is_string());
}

#[tokio::test]
async fn test_active_unrotated_heartbeat_includes_absolute_expiry() {
    let fake_valkey = FakeValkey::start().await;
    let session_config = SessionConfig {
        token_rotation_enabled: false,
        ..SessionConfig::default()
    };
    let state = test_state_with_session_config(fake_valkey.url(), session_config);
    let session = build_session(
        "active-unrotated-heartbeat",
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
    assert_eq!(json["absolute_expires_at"], expected_absolute_expiry);
    assert!(json.get("csrf_token").is_none());
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
