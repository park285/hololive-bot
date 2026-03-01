use std::sync::{
    Arc, RwLock,
    atomic::{AtomicBool, Ordering},
};

use axum::{Json, extract::State, http::StatusCode, response::IntoResponse};
use chrono::{DateTime, Utc};
use serde::Serialize;
use shared_infra::telemetry::shutdown_telemetry;
use tracing::info;

#[derive(Debug)]
pub(crate) struct AppState {
    pub version: &'static str,
    pub started_at: DateTime<Utc>,
    pub valkey_connected: AtomicBool,
    pub last_error: RwLock<Option<String>>,
}

#[derive(Debug, Serialize)]
pub(crate) struct HealthResponse {
    pub status: &'static str,
    pub version: &'static str,
    pub valkey_connected: bool,
    pub started_at: DateTime<Utc>,
    pub last_error: Option<String>,
}

pub(crate) struct TelemetryGuard;

impl Drop for TelemetryGuard {
    fn drop(&mut self) {
        shutdown_telemetry();
    }
}

pub(crate) fn set_last_error(state: &AppState, message: String) {
    if let Ok(mut guard) = state.last_error.write() {
        *guard = Some(message);
    }
}

pub(crate) fn clear_last_error(state: &AppState) {
    if let Ok(mut guard) = state.last_error.write() {
        *guard = None;
    }
}

pub(crate) fn snapshot_last_error(state: &AppState) -> Option<String> {
    state.last_error.read().ok().and_then(|guard| guard.clone())
}

pub(crate) fn update_valkey_connection(state: &AppState, connected: bool) {
    let previous = state.valkey_connected.swap(connected, Ordering::Relaxed);
    if previous != connected {
        info!(
            valkey_connected = connected,
            "valkey connection state changed"
        );
    }
}

pub(crate) async fn health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let response = HealthResponse {
        status: "alive",
        version: state.version,
        valkey_connected: state.valkey_connected.load(Ordering::Relaxed),
        started_at: state.started_at,
        last_error: snapshot_last_error(state.as_ref()),
    };

    (StatusCode::OK, Json(response))
}

pub(crate) async fn ready(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let valkey_connected = state.valkey_connected.load(Ordering::Relaxed);
    let (status_code, status) = readiness_status(valkey_connected);
    let response = HealthResponse {
        status,
        version: state.version,
        valkey_connected,
        started_at: state.started_at,
        last_error: snapshot_last_error(state.as_ref()),
    };

    (status_code, Json(response))
}

fn readiness_status(valkey_connected: bool) -> (StatusCode, &'static str) {
    if valkey_connected {
        (StatusCode::OK, "ok")
    } else {
        (StatusCode::SERVICE_UNAVAILABLE, "degraded")
    }
}

#[cfg(test)]
mod tests {
    use axum::http::StatusCode;

    use super::readiness_status;

    #[test]
    fn readiness_is_ok_when_valkey_is_connected() {
        let (status_code, status) = readiness_status(true);
        assert_eq!(status_code, StatusCode::OK);
        assert_eq!(status, "ok");
    }

    #[test]
    fn readiness_is_degraded_when_valkey_is_disconnected() {
        let (status_code, status) = readiness_status(false);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }
}
