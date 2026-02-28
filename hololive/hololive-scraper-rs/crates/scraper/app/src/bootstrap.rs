use anyhow::{Context, Result};
use scraper_core::error::ScraperError;
use scraper_core::model::MajorEventType;
use scraper_infra::{
    config::AppConfig,
    repository::{Repository, create_pool},
};
use scraper_service::{
    feed_scheduler::{FeedScheduler, FeedSchedulerConfig},
    link_checker::{LinkChecker, LinkCheckerConfig},
    maintenance_scheduler::{MaintenanceConfig, MaintenanceScheduler},
    scraper::{FeedSource, Scraper, ScraperConfig},
};
use std::{
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};
use tokio::task::{JoinHandle, JoinSet};
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

/// 스케줄러 spawn 결과 타입 별칭 (복잡 타입 재사용)
type SchedulerHandle = JoinHandle<Result<(), ScraperError>>;

pub struct RuntimeInit {
    pub db_connected: Arc<AtomicBool>,
    pub feed_schedulers_active: bool,
    pub feed_handles: Vec<SchedulerHandle>,
    pub maintenance_handle: Option<SchedulerHandle>,
    pub db_monitor_handle: Option<JoinHandle<()>>,
}

pub async fn run_once(config: &AppConfig) -> Result<()> {
    let (repository, db_connected) = init_repository(config).await;
    if !db_connected {
        anyhow::bail!("database unavailable; run-once mode requires a healthy database connection")
    }

    let repository = repository.context("repository unavailable in run-once mode")?;
    let http_client = build_http_client(config)?;
    let scraper = Scraper::new(
        http_client.clone(),
        ScraperConfig {
            user_agent: config.scraper.user_agent.clone(),
            ..ScraperConfig::default()
        },
    );

    let feed_schedulers = build_feed_schedulers(&scraper, &repository, config)?;
    let maintenance = build_maintenance_scheduler(&repository, config)?;

    info!("run-once: starting all feed scrapers and maintenance in parallel");

    let mut join_set = JoinSet::new();
    for fs in feed_schedulers {
        join_set.spawn(async move {
            let name = fs.config().name.clone();
            fs.run_cycle().await.map(|()| name)
        });
    }
    join_set.spawn(async move {
        maintenance
            .run_once()
            .await
            .map(|()| "maintenance".to_string())
    });

    let mut has_error = false;
    while let Some(result) = join_set.join_next().await {
        match result {
            Ok(Ok(name)) => info!(task = name, "run-once task completed"),
            Ok(Err(err)) => {
                error!(error = %err, "run-once task failed");
                has_error = true;
            }
            Err(err) => {
                error!(error = %err, "run-once task panicked");
                has_error = true;
            }
        }
    }

    if has_error {
        anyhow::bail!("run-once completed with errors");
    }

    info!("run-once completed successfully");
    Ok(())
}

pub async fn initialize_runtime(
    config: &AppConfig,
    shutdown_token: &CancellationToken,
) -> RuntimeInit {
    let (repository, db_connected) = init_repository(config).await;
    let db_connected = Arc::new(AtomicBool::new(db_connected));
    let db_monitor_handle = spawn_db_monitor(
        repository.clone(),
        Arc::clone(&db_connected),
        shutdown_token.clone(),
    );
    let (feed_handles, maintenance_handle, feed_schedulers_active) =
        spawn_schedulers(repository, config, shutdown_token.clone());

    RuntimeInit {
        db_connected,
        feed_schedulers_active,
        feed_handles,
        maintenance_handle,
        db_monitor_handle,
    }
}

pub async fn join_feed_schedulers(handles: Vec<SchedulerHandle>) {
    for handle in handles {
        match handle.await {
            Ok(Ok(())) => info!("feed scheduler stopped"),
            Ok(Err(err)) => error!(error = %err, "feed scheduler terminated with service error"),
            Err(err) => error!(error = %err, "feed scheduler join failed"),
        }
    }
}

pub async fn join_maintenance_scheduler(handle: Option<SchedulerHandle>) {
    if let Some(handle) = handle {
        match handle.await {
            Ok(Ok(())) => info!("maintenance scheduler stopped"),
            Ok(Err(err)) => error!(error = %err, "maintenance scheduler terminated with error"),
            Err(err) => error!(error = %err, "maintenance scheduler join failed"),
        }
    }
}

pub async fn join_db_monitor(handle: Option<JoinHandle<()>>) {
    if let Some(handle) = handle
        && let Err(err) = handle.await
    {
        warn!(error = %err, "db monitor join failed");
    }
}

async fn init_repository(config: &AppConfig) -> (Option<Repository>, bool) {
    match create_pool(&config.database).await {
        Ok(pool) => {
            let repository = Repository::new(pool);
            if let Err(err) = repository.health_check().await {
                warn!(error = %err, "database ping failed; running in degraded mode");
                (None, false)
            } else {
                (Some(repository), true)
            }
        }
        Err(err) => {
            warn!(error = %err, "database connection unavailable; running in degraded mode");
            (None, false)
        }
    }
}

fn build_feed_schedulers(
    scraper: &Scraper,
    repository: &Repository,
    config: &AppConfig,
) -> Result<Vec<FeedScheduler>> {
    let resolved_feeds = config.resolved_feeds();
    let mut schedulers = Vec::with_capacity(resolved_feeds.len());

    for feed_config in resolved_feeds {
        let event_type = match feed_config.event_type.as_str() {
            "event" => MajorEventType::Event,
            "news" => MajorEventType::News,
            other => anyhow::bail!(
                "unknown event_type '{}' in feed '{}'",
                other,
                feed_config.name
            ),
        };

        let sources: Vec<FeedSource> = feed_config
            .urls
            .iter()
            .map(|url| FeedSource {
                name: feed_config.name.clone(),
                event_type: event_type.clone(),
                feed_url: url.clone(),
            })
            .collect();

        let scrape_hour = feed_config
            .scrape_hour_kst
            .unwrap_or(config.scheduler.scrape_hour_kst);

        let scheduler_config = FeedSchedulerConfig::with_defaults(&feed_config.name, scrape_hour);
        schedulers.push(FeedScheduler::new(
            scraper.clone(),
            repository.clone(),
            sources,
            scheduler_config,
        ));
    }

    Ok(schedulers)
}

fn build_maintenance_scheduler(
    repository: &Repository,
    config: &AppConfig,
) -> Result<MaintenanceScheduler> {
    let link_checker_client = build_link_checker_client(config)?;
    let link_checker_max_concurrency = config.database.max_connections.max(1) as usize;
    let checker = LinkChecker::new(
        link_checker_client,
        LinkCheckerConfig {
            max_concurrency: link_checker_max_concurrency,
            ..LinkCheckerConfig::default()
        },
    );

    let maintenance_toml = config.maintenance.as_ref();
    let maintenance_config = MaintenanceConfig::new(
        maintenance_toml
            .and_then(|m| m.expired_hour_kst)
            .unwrap_or(5),
        maintenance_toml
            .and_then(|m| m.link_check_interval_hours)
            .unwrap_or(12),
    );

    Ok(MaintenanceScheduler::new(
        repository.clone(),
        checker,
        maintenance_config,
    ))
}

fn spawn_schedulers(
    repository: Option<Repository>,
    config: &AppConfig,
    shutdown_token: CancellationToken,
) -> (Vec<SchedulerHandle>, Option<SchedulerHandle>, bool) {
    let Some(repository) = repository else {
        return (Vec::new(), None, false);
    };

    let http_client = match build_http_client(config) {
        Ok(client) => client,
        Err(err) => {
            warn!(error = %err, "http client init failed; schedulers disabled");
            return (Vec::new(), None, false);
        }
    };

    let scraper = Scraper::new(
        http_client,
        ScraperConfig {
            user_agent: config.scraper.user_agent.clone(),
            ..ScraperConfig::default()
        },
    );

    let feed_schedulers = match build_feed_schedulers(&scraper, &repository, config) {
        Ok(schedulers) => schedulers,
        Err(err) => {
            warn!(error = %err, "feed schedulers init failed; feed schedulers disabled");
            Vec::new()
        }
    };
    if feed_schedulers.is_empty() {
        warn!("no feed scheduler active; health will report feed_schedulers_active=false");
    }

    let mut feed_handles = Vec::with_capacity(feed_schedulers.len());
    for scheduler in feed_schedulers {
        let token = shutdown_token.child_token();
        feed_handles.push(tokio::spawn(async move { scheduler.run(token).await }));
    }

    let maintenance_handle = match build_maintenance_scheduler(&repository, config) {
        Ok(maintenance) => {
            let token = shutdown_token.child_token();
            Some(tokio::spawn(async move { maintenance.run(token).await }))
        }
        Err(err) => {
            warn!(error = %err, "maintenance scheduler init failed; maintenance disabled");
            None
        }
    };

    let feed_schedulers_active = !feed_handles.is_empty();
    (feed_handles, maintenance_handle, feed_schedulers_active)
}

fn spawn_db_monitor(
    repository: Option<Repository>,
    db_connected: Arc<AtomicBool>,
    shutdown_token: CancellationToken,
) -> Option<JoinHandle<()>> {
    let repository = repository?;
    let token = shutdown_token.child_token();

    Some(tokio::spawn(async move {
        const DB_MONITOR_INTERVAL: Duration = Duration::from_secs(15);

        loop {
            tokio::select! {
                _ = token.cancelled() => {
                    info!("db monitor stopped");
                    break;
                }
                _ = tokio::time::sleep(DB_MONITOR_INTERVAL) => {
                    let healthy = repository.health_check().await.is_ok();
                    db_connected.store(healthy, Ordering::Relaxed);
                }
            }
        }
    }))
}

fn build_http_client(config: &AppConfig) -> Result<reqwest::Client> {
    let mut builder = reqwest::Client::builder()
        .user_agent(config.scraper.user_agent.clone())
        // RSS fetch용: redirect 허용 (피드 URL이 redirect 반환 가능)
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(60));

    if !config.proxy.socks5_url.trim().is_empty() {
        let proxy = reqwest::Proxy::all(&config.proxy.socks5_url)
            .with_context(|| format!("invalid proxy url: {}", config.proxy.socks5_url))?;
        builder = builder.proxy(proxy);
    }

    builder.build().context("failed to build reqwest client")
}

fn build_link_checker_client(config: &AppConfig) -> Result<reqwest::Client> {
    // SSRF 우회 방지: redirect 비활성화 (3xx는 is_success_status에서 OK 판정)
    let mut builder = reqwest::Client::builder()
        .user_agent(config.scraper.user_agent.clone())
        .redirect(reqwest::redirect::Policy::none())
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(60));

    if !config.proxy.socks5_url.trim().is_empty() {
        let proxy = reqwest::Proxy::all(&config.proxy.socks5_url)
            .with_context(|| format!("invalid proxy url: {}", config.proxy.socks5_url))?;
        builder = builder.proxy(proxy);
    }

    builder
        .build()
        .context("failed to build link checker reqwest client")
}
