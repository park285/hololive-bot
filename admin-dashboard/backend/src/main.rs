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
mod ssr;
mod state;
mod static_files;
mod status;

use std::net::SocketAddr;
use std::sync::Arc;

use tokio_util::sync::CancellationToken;

#[tokio::main]
async fn main() {
    dotenvy::dotenv().ok();

    let cfg = config::Config::load();
    let _tracing_guards = logging::init_tracing(&cfg);
    tracing::info!(port = %cfg.port, env = %cfg.env, "starting admin-dashboard");

    let pool = deadpool_redis::Config::from_url(format!("redis://{}", cfg.valkey_url))
        .create_pool(Some(deadpool_redis::Runtime::Tokio1))
        .expect("valkey pool creation failed");

    let docker_svc = docker::DockerService::new(&cfg.docker_host)
        .ok()
        .map(Arc::new);
    let holo_api = Arc::new(
        holo::client::HoloApiClient::new(
            &cfg.holo_bot_url,
            if cfg.holo_bot_api_key.is_empty() {
                None
            } else {
                Some(cfg.holo_bot_api_key.clone())
            },
        )
        .expect("holo api client init failed"),
    );

    let endpoints = vec![status::ServiceEndpoint {
        name: "hololive-bot".to_string(),
        url: cfg.holo_bot_url.clone(),
        health_path: "/health".to_string(),
    }];
    let status_collector =
        status::StatusCollector::new(endpoints.clone(), env!("CARGO_PKG_VERSION"));
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
        .expect("failed to bind");
    tracing::info!(%addr, "listening");

    axum::serve(
        listener,
        router.into_make_service_with_connect_info::<SocketAddr>(),
    )
    .with_graceful_shutdown(shutdown_signal())
    .await
    .expect("server error");

    cancel_token.cancel();
    rate_limiter.shutdown();
    tracing::info!("shutdown complete");
}

async fn shutdown_signal() {
    let ctrl_c = tokio::signal::ctrl_c();
    let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        .expect("sigterm handler");

    tokio::select! {
        _ = ctrl_c => tracing::info!("SIGINT received"),
        _ = sigterm.recv() => tracing::info!("SIGTERM received"),
    }
}
