mod auth;
mod config;
mod docker;
mod error;
mod handlers;
mod middleware;
mod openapi;
mod proxy;
mod routes;
mod ssr;
mod state;
mod static_files;
mod status;
mod stream_limiter;

use std::net::SocketAddr;
use std::sync::Arc;

use tokio_util::sync::CancellationToken;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() {
    dotenvy::dotenv().ok();

    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_env("LOG_LEVEL").unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .json()
        .init();

    let cfg = config::Config::load();
    tracing::info!(port = %cfg.port, env = %cfg.env, "starting admin-dashboard");

    let pool = deadpool_redis::Config::from_url(format!("redis://{}", cfg.valkey_url))
        .create_pool(Some(deadpool_redis::Runtime::Tokio1))
        .expect("valkey pool creation failed");

    let docker_svc = docker::DockerService::new(&cfg.docker_host).ok().map(Arc::new);
    let bot_proxy = proxy::BotProxy::new(&cfg.holo_bot_url, {
        let key = cfg.holo_bot_api_key.clone();
        if key.is_empty() { None } else { Some(key) }
    })
    .ok();

    let endpoints = vec![status::ServiceEndpoint {
        name: "hololive-bot".to_string(),
        url: cfg.holo_bot_url.clone(),
        health_path: "/health".to_string(),
    }];
    let status_collector = status::StatusCollector::new(endpoints, env!("CARGO_PKG_VERSION"));
    let (stats_tx, _) = tokio::sync::broadcast::channel(16);
    let cancel_token = CancellationToken::new();
    status::SystemStatsCollector::start(stats_tx.clone(), cancel_token.clone());

    let session_cfg = cfg.session.clone();
    let sessions = auth::session::ValkeySessionStore::new(pool, session_cfg);
    let rate_limiter = Arc::new(auth::rate_limiter::LoginRateLimiter::new());
    rate_limiter.start_cleanup_task();
    let ssr_injector = ssr::SsrInjector::new(&cfg.holo_bot_url);
    let stream_limiter = Arc::new(stream_limiter::StreamLimiter::new(
        cfg.security.global_stream_limit,
        cfg.security.per_session_stream_limit,
        cfg.security.stream_limit_mode,
    ));

    let app_state = Arc::new(state::AppState {
        config: cfg.clone(),
        sessions,
        rate_limiter: rate_limiter.clone(),
        bot_proxy,
        docker_svc,
        status_collector,
        stats_tx,
        stream_limiter,
        ssr_injector,
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
