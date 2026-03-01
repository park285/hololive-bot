use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

use alarm_service::scheduler::SchedulerRuntimeHealth;
use axum::{
    Json,
    extract::State,
    http::{StatusCode, header},
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use metrics_exporter_prometheus::PrometheusHandle;
use serde::Serialize;

#[derive(Debug, Clone)]
pub struct AppState {
    pub version: &'static str,
    pub valkey_connected: bool,
    pub db_connected: bool,
    pub scheduler_enabled: bool,
    pub scheduler_running: Arc<AtomicBool>,
    pub scheduler_runtime_health: Option<Arc<SchedulerRuntimeHealth>>,
    pub metrics_handle: PrometheusHandle,
    pub started_at: DateTime<Utc>,
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    version: &'static str,
    valkey_connected: bool,
    db_connected: bool,
    scheduler_enabled: bool,
    scheduler_running: bool,
    scheduler_healthy: bool,
    started_at: DateTime<Utc>,
}

/// GET /health — liveness 전용(프로세스 생존 확인), 항상 200 OK
pub async fn health(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let scheduler_running = state.scheduler_running.load(Ordering::Relaxed);
    let scheduler_healthy = scheduler_health(state.as_ref(), scheduler_running);

    let response = HealthResponse {
        status: "alive",
        version: state.version,
        valkey_connected: state.valkey_connected,
        db_connected: state.db_connected,
        scheduler_enabled: state.scheduler_enabled,
        scheduler_running,
        scheduler_healthy,
        started_at: state.started_at,
    };

    (StatusCode::OK, Json(response))
}

/// GET /ready — readiness 전용(의존성/스케줄러 준비 상태), 준비 안됨이면 503
pub async fn ready(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    let scheduler_running = state.scheduler_running.load(Ordering::Relaxed);
    let scheduler_healthy = scheduler_health(state.as_ref(), scheduler_running);
    let (status_code, status) = readiness_status(
        state.valkey_connected,
        state.scheduler_enabled,
        scheduler_running,
        scheduler_healthy,
    );

    let response = HealthResponse {
        status,
        version: state.version,
        valkey_connected: state.valkey_connected,
        db_connected: state.db_connected,
        scheduler_enabled: state.scheduler_enabled,
        scheduler_running,
        scheduler_healthy,
        started_at: state.started_at,
    };

    (status_code, Json(response))
}

/// GET /metrics — Prometheus metrics endpoint
pub async fn metrics(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    state.metrics_handle.run_upkeep();
    let rendered = state.metrics_handle.render();
    (
        [(
            header::CONTENT_TYPE,
            "text/plain; version=0.0.4; charset=utf-8",
        )],
        rendered,
    )
}

pub fn readiness_status(
    valkey_connected: bool,
    scheduler_enabled: bool,
    scheduler_running: bool,
    scheduler_healthy: bool,
) -> (StatusCode, &'static str) {
    if !valkey_connected || !scheduler_enabled || !scheduler_running || !scheduler_healthy {
        return (StatusCode::SERVICE_UNAVAILABLE, "degraded");
    }

    (StatusCode::OK, "ok")
}

fn scheduler_health(state: &AppState, scheduler_running: bool) -> bool {
    if !scheduler_running {
        return false;
    }

    state
        .scheduler_runtime_health
        .as_ref()
        .map(|health| {
            // disabled Twitch 루프는 SchedulerHealthSnapshot::overall_healthy 집계에서 제외된다.
            health.snapshot().overall_healthy()
        })
        .unwrap_or(false)
}
