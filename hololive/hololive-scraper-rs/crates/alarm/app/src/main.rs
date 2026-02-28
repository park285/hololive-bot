mod bootstrap;
mod cli;
mod observability;
mod shutdown;
mod state;

use std::{net::SocketAddr, sync::Arc};

use alarm_infra::config::AlarmAppConfig;
use anyhow::{Context, Result};
use axum::{Router, routing::get};
use chrono::Utc;
use clap::Parser;
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;
use tower_http::trace::TraceLayer;
use tracing::info;

use crate::{
    bootstrap::{initialize_runtime, join_scheduler},
    cli::Cli,
    observability::{init_metrics, init_tracing, resolve_telemetry_config},
    shutdown::wait_for_shutdown,
    state::{AppState, health, metrics, ready},
};

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    let config = AlarmAppConfig::load_from_path(&cli.config)
        .with_context(|| format!("설정 파일 로드 실패: {}", cli.config.display()))?;
    let telemetry = resolve_telemetry_config(&config.telemetry);
    let _tracing_runtime = init_tracing(&config.logging, &telemetry)?;
    let metrics_handle = init_metrics().context("metrics 초기화 실패")?;

    let shutdown_token = CancellationToken::new();
    let runtime = initialize_runtime(&config, &shutdown_token).await;

    let shared_state = Arc::new(AppState {
        version: env!("CARGO_PKG_VERSION"),
        valkey_connected: runtime.valkey_connected,
        db_connected: runtime.db_connected,
        scheduler_enabled: runtime.scheduler_handle.is_some(),
        scheduler_running: Arc::clone(&runtime.scheduler_running),
        scheduler_runtime_health: runtime.scheduler_runtime_health.clone(),
        metrics_handle,
        started_at: Utc::now(),
    });

    let app = Router::new()
        .route("/health", get(health))
        .route("/ready", get(ready))
        .route("/metrics", get(metrics))
        .with_state(shared_state)
        .layer(TraceLayer::new_for_http());

    let addr = SocketAddr::from(([0, 0, 0, 0], config.health.port));
    let listener = TcpListener::bind(addr)
        .await
        .with_context(|| format!("헬스 서버 바인드 실패: {}", addr))?;

    info!(address = %addr, "alarm-app 시작");

    axum::serve(listener, app)
        .with_graceful_shutdown(wait_for_shutdown())
        .await
        .context("헬스 서버 오류로 종료")?;

    shutdown_token.cancel();
    join_scheduler(runtime.scheduler_handle).await;

    Ok(())
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use axum::http::StatusCode;
    use clap::Parser;

    use super::{cli::Cli, state::readiness_status};

    /// CLI 기본값 확인
    #[test]
    fn cli_defaults() {
        let cli = Cli::try_parse_from(["alarm-app"]).expect("CLI 파싱 성공해야 함");
        assert_eq!(cli.config, PathBuf::from("alarm-config.toml"));
    }

    /// 커스텀 경로 지정 확인
    #[test]
    fn cli_accepts_custom_config_path() {
        let cli = Cli::try_parse_from(["alarm-app", "--config", "/tmp/my-alarm.toml"])
            .expect("CLI 파싱 성공해야 함");
        assert_eq!(cli.config, PathBuf::from("/tmp/my-alarm.toml"));
    }

    /// readiness: Valkey + scheduler 활성이면 ok
    #[test]
    fn readiness_status_ok_when_dependencies_ready() {
        let (status_code, status) = readiness_status(true, true, true, true);
        assert_eq!(status_code, StatusCode::OK);
        assert_eq!(status, "ok");
    }

    /// readiness: Valkey 미연결이면 degraded
    #[test]
    fn readiness_status_degraded_when_valkey_disconnected() {
        let (status_code, status) = readiness_status(false, true, true, true);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }

    /// readiness: scheduler 비활성이면 degraded
    #[test]
    fn readiness_status_degraded_when_scheduler_disabled() {
        let (status_code, status) = readiness_status(true, false, false, false);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }

    /// readiness: scheduler가 중단되면 degraded
    #[test]
    fn readiness_status_degraded_when_scheduler_not_running() {
        let (status_code, status) = readiness_status(true, true, false, false);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }

    /// readiness: runtime heartbeat가 stale이면 degraded
    #[test]
    fn readiness_status_degraded_when_scheduler_unhealthy() {
        let (status_code, status) = readiness_status(true, true, true, false);
        assert_eq!(status_code, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(status, "degraded");
    }
}
