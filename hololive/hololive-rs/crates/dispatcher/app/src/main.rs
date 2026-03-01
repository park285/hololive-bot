mod bootstrap;
mod cli;
mod config;
mod dispatch;
mod grouping;
mod render;
mod state;

use std::{
    net::SocketAddr,
    sync::{Arc, RwLock, atomic::AtomicBool},
    time::Duration,
};

use anyhow::{Context, Result};
use axum::{Router, routing::get};
use chrono::Utc;
use clap::Parser;
use shared_formatter::ResponseFormatter;
use shared_infra::{
    db::create_pool, logging::init_logging, shutdown::graceful_shutdown, telemetry::init_telemetry,
    valkey::FredValkeyClient,
};
use shared_notification::ValkeyQueueConsumer;
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;
use tower_http::trace::TraceLayer;
use tracing::{error, info, warn};

use crate::{
    bootstrap::build_renderer,
    cli::Cli,
    config::load_dispatcher_config,
    dispatch::{DispatchRuntime, run_dispatch_loop},
    state::{AppState, TelemetryGuard, health, ready},
};

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    let config = load_dispatcher_config(&cli.config)?;

    init_logging(&config.logging);
    init_telemetry(&config.telemetry).context("init telemetry")?;
    let _telemetry_guard = TelemetryGuard;

    let valkey_client = Arc::new(
        FredValkeyClient::new(&config.valkey.url)
            .await
            .with_context(|| format!("connect valkey: {}", config.valkey.url))?,
    );

    let consumer =
        ValkeyQueueConsumer::new(valkey_client).with_queue_key(&config.dispatcher.queue_key);

    let iris_client =
        shared_infra::iris::IrisClient::new(&config.iris.base_url, config.iris.bot_token.clone())
            .context("build iris client")?;

    let db_connection = match create_pool(&config.db).await {
        Ok(pool) => Some(pool),
        Err(error) => {
            warn!(
                error = %error,
                "dispatcher database connection failed; fallback templates will be used"
            );
            None
        }
    };

    let renderer = build_renderer(db_connection.as_ref()).await?;

    let runtime = DispatchRuntime {
        consumer,
        formatter: ResponseFormatter::new(""),
        renderer,
        iris_client,
        max_batch: config.dispatcher.max_batch.max(1),
        reconnect_backoff: Duration::from_millis(config.dispatcher.reconnect_backoff_ms.max(100)),
    };

    let state = Arc::new(AppState {
        version: env!("CARGO_PKG_VERSION"),
        started_at: Utc::now(),
        valkey_connected: AtomicBool::new(true),
        last_error: RwLock::new(None),
    });

    let shutdown_token = CancellationToken::new();
    let dispatch_handle = {
        let dispatch_state = Arc::clone(&state);
        let dispatch_token = shutdown_token.clone();
        tokio::spawn(
            async move { run_dispatch_loop(runtime, dispatch_state, dispatch_token).await },
        )
    };

    let app = Router::new()
        .route("/health", get(health))
        .route("/ready", get(ready))
        .with_state(Arc::clone(&state))
        .layer(TraceLayer::new_for_http());

    let addr: SocketAddr = format!("{}:{}", config.health.host, config.health.port)
        .parse()
        .context("parse health listen address")?;

    let listener = TcpListener::bind(addr)
        .await
        .with_context(|| format!("bind health server: {addr}"))?;

    info!(address = %addr, "dispatcher-app started");

    let shutdown_waiter = tokio::spawn(graceful_shutdown(shutdown_token.clone()));

    let server_result = axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_token.clone().cancelled_owned())
        .await
        .context("health server terminated with error");

    shutdown_token.cancel();

    if let Err(join_error) = shutdown_waiter.await {
        warn!(error = %join_error, "shutdown waiter join failed");
    }

    if let Err(join_error) = dispatch_handle.await {
        error!(error = %join_error, "dispatcher loop join failed");
        return Err(anyhow::Error::new(join_error).context("join dispatcher loop"));
    }

    server_result
}
