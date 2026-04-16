mod auth;
mod config;
mod docker;
mod error;
mod handlers;
mod holo;
mod logging;
mod middleware;
mod openapi;
mod routes;
mod state;
mod static_files;
mod status;

use anyhow::Context;
use std::net::SocketAddr;
use std::sync::Arc;

use tokio_util::sync::CancellationToken;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    run().await
}

async fn run() -> anyhow::Result<()> {
    dotenvy::dotenv().ok();

    let cfg = config::Config::load().context("admin-dashboard config load failed")?;
    let _tracing_guards = logging::init_tracing(&cfg);
    tracing::info!(port = %cfg.port, env = %cfg.env, "starting admin-dashboard");

    let pool = deadpool_redis::Config::from_url(format!("redis://{}", cfg.valkey_url))
        .create_pool(Some(deadpool_redis::Runtime::Tokio1))
        .context("valkey pool creation failed")?;

    let docker_svc = docker::DockerService::new(&cfg.docker_host)
        .ok()
        .map(Arc::new);
    let holo_api = Arc::new(
        holo::client::HoloApiClient::new(
            &cfg.holo_admin_api_url,
            if cfg.holo_bot_api_key.is_empty() {
                None
            } else {
                Some(cfg.holo_bot_api_key.clone())
            },
        )
        .context("holo api client init failed")?,
    );

    let endpoints = vec![status::ServiceEndpoint {
        name: "hololive-admin-api".to_string(),
        url: cfg.holo_admin_api_url.clone(),
        health_path: "/health".to_string(),
    }];
    let status_collector =
        status::StatusCollector::new(endpoints.clone(), env!("CARGO_PKG_VERSION"))
            .context("status collector init failed")?;
    let (stats_tx, _) = tokio::sync::broadcast::channel(16);
    let cancel_token = CancellationToken::new();
    status::SystemStatsCollector::start(stats_tx.clone(), endpoints, cancel_token.clone());

    let session_cfg = cfg.session.clone();
    let sessions = auth::session::ValkeySessionStore::new(pool, session_cfg);
    let rate_limiter = Arc::new(auth::rate_limiter::LoginRateLimiter::new());
    rate_limiter.start_cleanup_task();

    let app_state = Arc::new(state::AppState {
        config: cfg.clone(),
        sessions,
        rate_limiter: rate_limiter.clone(),
        holo_api,
        docker_svc,
        status_collector,
        stats_tx,
    });
    let router = routes::build_router(app_state);

    let addr = SocketAddr::from(([0, 0, 0, 0], cfg.port));
    let listener = tokio::net::TcpListener::bind(addr)
        .await
        .context("failed to bind")?;
    tracing::info!(%addr, "listening");

    axum::serve(
        listener,
        router.into_make_service_with_connect_info::<SocketAddr>(),
    )
    .with_graceful_shutdown(async {
        if let Err(err) = shutdown_signal().await {
            tracing::error!(error = %err, "shutdown signal handler failed");
        }
    })
    .await
    .context("server error")?;

    cancel_token.cancel();
    rate_limiter.shutdown();
    tracing::info!("shutdown complete");
    Ok(())
}

async fn shutdown_signal() -> anyhow::Result<()> {
    let ctrl_c = tokio::signal::ctrl_c();
    let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        .context("sigterm handler")?;

    tokio::select! {
        _ = ctrl_c => tracing::info!("SIGINT received"),
        _ = sigterm.recv() => tracing::info!("SIGTERM received"),
    }
    Ok(())
}
