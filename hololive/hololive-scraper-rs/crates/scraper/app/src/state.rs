use axum::{Json, extract::State, http::StatusCode, response::IntoResponse};
use chrono::{DateTime, Utc};
use serde::Serialize;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use tracing::instrument;

#[derive(Debug, Clone)]
pub struct AppState {
    pub version: &'static str,
    pub db_connected: Arc<AtomicBool>,
    pub feed_schedulers_active: bool,
    pub started_at: DateTime<Utc>,
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    version: &'static str,
    db_connected: bool,
    feed_schedulers_active: bool,
    started_at: DateTime<Utc>,
    last_scrape_at: Option<DateTime<Utc>>,
    next_scrape_at: Option<DateTime<Utc>>,
}

#[instrument(name = "http.health", level = "info", skip(state))]
pub async fn health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let db_connected = state.db_connected.load(Ordering::Relaxed);
    let response = HealthResponse {
        status: "alive",
        version: state.version,
        db_connected,
        feed_schedulers_active: state.feed_schedulers_active,
        started_at: state.started_at,
        last_scrape_at: None,
        next_scrape_at: None,
    };

    (StatusCode::OK, Json(response))
}

#[instrument(name = "http.ready", level = "info", skip(state))]
pub async fn ready(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let db_connected = state.db_connected.load(Ordering::Relaxed);
    let (status_code, status) = readiness_status(db_connected, state.feed_schedulers_active);

    let response = HealthResponse {
        status,
        version: state.version,
        db_connected,
        feed_schedulers_active: state.feed_schedulers_active,
        started_at: state.started_at,
        last_scrape_at: None,
        next_scrape_at: None,
    };

    (status_code, Json(response))
}

pub fn readiness_status(
    db_connected: bool,
    feed_schedulers_active: bool,
) -> (StatusCode, &'static str) {
    if !db_connected || !feed_schedulers_active {
        return (StatusCode::SERVICE_UNAVAILABLE, "degraded");
    }

    (StatusCode::OK, "ok")
}

#[cfg(test)]
mod tests {
    use super::readiness_status;
    use axum::http::StatusCode;

    #[test]
    fn readiness_status_ok_when_dependencies_ready() {
        let (status_code, status) = readiness_status(true, true);
        assert_eq!(status_code, StatusCode::OK);
        assert_eq!(status, "ok");
    }

    #[test]
    fn readiness_status_degraded_when_db_disconnected() {
        let (status_code, status) = readiness_status(false, true);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }

    #[test]
    fn readiness_status_degraded_when_feed_scheduler_inactive() {
        let (status_code, status) = readiness_status(true, false);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }
}
