use crate::link_checker::LinkChecker;
use crate::scheduler::format_kst;
use chrono::Utc;
use scraper_core::error::ScraperError;
use scraper_infra::repository::Repository;
use std::time::Duration;
use tokio_util::sync::CancellationToken;
use tracing::{info, warn};

const DEFAULT_EXPIRED_HOUR_KST: u8 = 5;
const DEFAULT_LINK_CHECK_INTERVAL_HOURS: u64 = 12;
const MIN_LINK_CHECK_INTERVAL_HOURS: u64 = 1;
const MAX_LINK_CHECK_INTERVAL_HOURS: u64 = 168; // 1주

#[derive(Debug, Clone)]
pub struct MaintenanceConfig {
    pub expired_hour_kst: u8,
    pub link_check_interval_hours: u64,
}

impl MaintenanceConfig {
    /// link_check_interval_hours를 안전 범위(1~168)로 clamp하여 생성
    pub fn new(expired_hour_kst: u8, link_check_interval_hours: u64) -> Self {
        Self {
            expired_hour_kst,
            link_check_interval_hours: link_check_interval_hours
                .clamp(MIN_LINK_CHECK_INTERVAL_HOURS, MAX_LINK_CHECK_INTERVAL_HOURS),
        }
    }
}

impl Default for MaintenanceConfig {
    fn default() -> Self {
        Self::new(DEFAULT_EXPIRED_HOUR_KST, DEFAULT_LINK_CHECK_INTERVAL_HOURS)
    }
}

#[derive(Clone)]
pub struct MaintenanceScheduler {
    repository: Repository,
    link_checker: LinkChecker,
    config: MaintenanceConfig,
}

impl MaintenanceScheduler {
    pub fn new(
        repository: Repository,
        link_checker: LinkChecker,
        config: MaintenanceConfig,
    ) -> Self {
        Self {
            repository,
            link_checker,
            config,
        }
    }

    pub async fn run(&self, shutdown: CancellationToken) -> Result<(), ScraperError> {
        use crate::scheduler::calculate_next_regular_run_for_hour;

        let link_check_interval = Duration::from_secs(self.config.link_check_interval_hours * 3600);
        let mut link_check_timer = tokio::time::interval(link_check_interval);
        // 첫 tick은 즉시 발생 → 건너뜀 (스타트업 직후 link check 방지)
        link_check_timer.tick().await;

        loop {
            let now = Utc::now();
            let next_expired_run =
                calculate_next_regular_run_for_hour(now, self.config.expired_hour_kst);
            let wait_duration = next_expired_run
                .signed_duration_since(now)
                .to_std()
                .unwrap_or(Duration::ZERO);

            info!(
                next_expired_kst = %format_kst(next_expired_run),
                link_check_interval_hours = self.config.link_check_interval_hours,
                "maintenance scheduler waiting"
            );

            tokio::select! {
                _ = shutdown.cancelled() => {
                    info!("maintenance scheduler received shutdown signal");
                    return Ok(());
                }
                _ = tokio::time::sleep(wait_duration) => {
                    self.run_expired_update().await;
                }
                _ = link_check_timer.tick() => {
                    self.run_link_check().await;
                }
            }
        }
    }

    /// --run-once용: expired update + link check 순차 실행
    pub async fn run_once(&self) -> Result<(), ScraperError> {
        self.run_expired_update().await;
        self.run_link_check().await;
        Ok(())
    }

    async fn run_expired_update(&self) {
        match self.repository.update_expired_events().await {
            Ok(updated) => {
                if updated > 0 {
                    info!(updated_expired = updated, "expired events updated");
                }
            }
            Err(err) => {
                warn!(error = %err, "failed to update expired events");
            }
        }
    }

    async fn run_link_check(&self) {
        match self.link_checker.check_stale_links(&self.repository).await {
            Ok(result) => {
                if result.checked > 0 {
                    info!(
                        checked = result.checked,
                        ok = result.ok,
                        failed = result.failed,
                        blocked = result.blocked,
                        "stale link check completed"
                    );
                }
            }
            Err(err) => {
                warn!(error = %err, "failed to check stale links");
            }
        }
    }
}
