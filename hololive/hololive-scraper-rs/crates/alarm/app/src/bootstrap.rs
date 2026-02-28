use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

use alarm_core::error::AlarmError;
use alarm_infra::{
    chzzk::HttpChzzkClient,
    config::AlarmAppConfig,
    holodex::HttpHolodexClient,
    repository::{PgAlarmRepository, create_pool},
    twitch::HttpTwitchClient,
    valkey::{FredValkeyClient, ValkeyClient},
};
use alarm_service::{
    checker::YouTubeChecker,
    chzzk_checker::ChzzkChecker,
    dedup::DedupService,
    notifier::Notifier,
    queue::QueuePublisher,
    scheduler::{AlarmScheduler, SchedulerRuntimeHealth, SchedulerTimingConfig},
    tier::TieredScheduler,
    twitch_checker::TwitchChecker,
};
use anyhow::{Context, Result};
use tokio::task::JoinHandle;
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

pub struct RuntimeInit {
    pub valkey_connected: bool,
    pub db_connected: bool,
    pub scheduler_running: Arc<AtomicBool>,
    pub scheduler_runtime_health: Option<Arc<SchedulerRuntimeHealth>>,
    pub scheduler_handle: Option<JoinHandle<Result<(), AlarmError>>>,
}

pub async fn initialize_runtime(
    config: &AlarmAppConfig,
    shutdown_token: &CancellationToken,
) -> RuntimeInit {
    let (valkey, valkey_connected) = init_valkey(config).await;
    let db_connected = init_db(config).await;

    let scheduler_running = Arc::new(AtomicBool::new(false));
    let mut scheduler_runtime_health: Option<Arc<SchedulerRuntimeHealth>> = None;

    let scheduler_handle = if let Some(ref vc) = valkey {
        match build_scheduler(Arc::clone(vc), config) {
            Ok(scheduler) => {
                scheduler_runtime_health = Some(scheduler.runtime_health_handle());
                let token = shutdown_token.child_token();
                let running_flag = Arc::clone(&scheduler_running);
                running_flag.store(true, Ordering::Relaxed);
                Some(tokio::spawn(async move {
                    let result = scheduler.run(token).await;
                    running_flag.store(false, Ordering::Relaxed);
                    result
                }))
            }
            Err(err) => {
                warn!(error = %err, "스케줄러 초기화 실패 — 스케줄러 비활성화");
                None
            }
        }
    } else {
        warn!("Valkey 미연결 — 스케줄러 비활성화");
        None
    };

    RuntimeInit {
        valkey_connected,
        db_connected,
        scheduler_running,
        scheduler_runtime_health,
        scheduler_handle,
    }
}

pub async fn join_scheduler(handle: Option<JoinHandle<Result<(), AlarmError>>>) {
    if let Some(handle) = handle {
        match handle.await {
            Ok(Ok(())) => info!("스케줄러 종료"),
            Ok(Err(err)) => error!(error = %err, "스케줄러 비정상 종료"),
            Err(err) => error!(error = %err, "스케줄러 join 실패"),
        }
    }
}

/// Valkey 클라이언트 초기화 — 실패 시 None 반환 (degraded 모드)
async fn init_valkey(config: &AlarmAppConfig) -> (Option<Arc<dyn ValkeyClient>>, bool) {
    match FredValkeyClient::new(&config.valkey.url).await {
        Ok(client) => {
            let client: Arc<dyn ValkeyClient> = Arc::new(client);
            if let Err(e) = client.ping().await {
                warn!(error = %e, "Valkey ping 실패 — degraded 모드");
                (None, false)
            } else {
                (Some(client), true)
            }
        }
        Err(e) => {
            warn!(error = %e, "Valkey 연결 실패 — degraded 모드");
            (None, false)
        }
    }
}

/// DB 연결 확인 — 실패 시 false 반환
async fn init_db(config: &AlarmAppConfig) -> bool {
    match create_pool(&config.database).await {
        Ok(pool) => {
            let repo = PgAlarmRepository::new(pool);
            if let Err(e) = repo.health_check().await {
                warn!(error = %e, "DB ping 실패 — degraded 모드");
                false
            } else {
                true
            }
        }
        Err(e) => {
            warn!(error = %e, "DB 연결 실패 — degraded 모드");
            false
        }
    }
}

/// 스케줄러 조립 — 체커/notifier/클라이언트 생성 후 AlarmScheduler 반환
fn build_scheduler(
    valkey: Arc<dyn ValkeyClient>,
    config: &AlarmAppConfig,
) -> Result<Arc<AlarmScheduler>> {
    let holodex =
        Arc::new(HttpHolodexClient::new(&config.holodex).context("Holodex 클라이언트 생성 실패")?);
    let chzzk =
        Arc::new(HttpChzzkClient::new(&config.chzzk).context("Chzzk 클라이언트 생성 실패")?);
    let twitch =
        Arc::new(HttpTwitchClient::new(&config.twitch).context("Twitch 클라이언트 생성 실패")?);

    let tier_sched = Arc::new(TieredScheduler::new());
    let queue = Arc::new(QueuePublisher::new(Arc::clone(&valkey)));
    let dedup = Arc::new(DedupService::new(
        Arc::clone(&valkey),
        config.alarm.target_minutes.clone(),
    ));

    let youtube_checker = Arc::new(YouTubeChecker::new(
        holodex as Arc<dyn alarm_infra::holodex::HolodexClient>,
        Arc::clone(&valkey),
        Arc::clone(&tier_sched),
        Arc::clone(&dedup),
        config.alarm.target_minutes.clone(),
    ));

    let chzzk_checker = Arc::new(ChzzkChecker::new(
        chzzk as Arc<dyn alarm_infra::chzzk::ChzzkClient>,
        Arc::clone(&valkey),
    ));

    let twitch_checker = Arc::new(TwitchChecker::new(
        twitch as Arc<dyn alarm_infra::twitch::TwitchClient>,
        Arc::clone(&valkey),
    ));

    let notifier = Arc::new(Notifier::new(queue, dedup, tier_sched));

    Ok(Arc::new(AlarmScheduler::new(
        youtube_checker,
        chzzk_checker,
        twitch_checker,
        notifier,
        valkey,
        SchedulerTimingConfig {
            twitch_enabled: config.alarm.twitch_enabled,
            chzzk_poll_secs: config.alarm.chzzk_poll_secs,
            twitch_poll_secs: config.alarm.twitch_poll_secs,
            youtube_check_timeout_secs: config.alarm.youtube_check_timeout_secs,
            chzzk_check_timeout_secs: config.alarm.chzzk_check_timeout_secs,
            twitch_check_timeout_secs: config.alarm.twitch_check_timeout_secs,
        },
    )))
}
