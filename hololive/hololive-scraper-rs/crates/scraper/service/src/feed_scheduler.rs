use std::{collections::VecDeque, sync::Arc, time::Duration};

use chrono::{DateTime, Utc};
use parking_lot::Mutex;
use scraper_core::error::ScraperError;
use scraper_infra::repository::Repository;
use tokio_util::sync::CancellationToken;
use tracing::{Instrument, info, info_span, warn};

use crate::{
    scheduler::{
        ScrapeTriggerType, build_retry_runs_from_delays, calculate_next_regular_run_for_hour,
        format_kst,
    },
    scraper::{FeedSource, Scraper},
};

#[derive(Debug, Clone)]
pub struct FeedSchedulerConfig {
    pub name: String,
    pub scrape_hour_kst: u8,
    pub retry_delays: Vec<Duration>,
}

impl FeedSchedulerConfig {
    pub fn with_defaults(name: impl Into<String>, scrape_hour_kst: u8) -> Self {
        Self {
            name: name.into(),
            scrape_hour_kst,
            retry_delays: vec![
                Duration::from_secs(30 * 60),
                Duration::from_secs(2 * 60 * 60),
                Duration::from_secs(6 * 60 * 60),
            ],
        }
    }
}

#[derive(Clone)]
pub struct FeedScheduler {
    scraper: Scraper,
    repository: Repository,
    sources: Vec<FeedSource>,
    config: FeedSchedulerConfig,
    retry_runs: Arc<Mutex<VecDeque<DateTime<Utc>>>>,
}

impl FeedScheduler {
    pub fn new(
        scraper: Scraper,
        repository: Repository,
        sources: Vec<FeedSource>,
        config: FeedSchedulerConfig,
    ) -> Self {
        Self {
            scraper,
            repository,
            sources,
            config,
            retry_runs: Arc::new(Mutex::new(VecDeque::new())),
        }
    }

    pub fn config(&self) -> &FeedSchedulerConfig {
        &self.config
    }

    pub async fn run(&self, shutdown: CancellationToken) -> Result<(), ScraperError> {
        loop {
            let now = Utc::now();
            let (next_run, trigger_type) = self.next_run_with_trigger(now);
            let wait_duration = next_run
                .signed_duration_since(now)
                .to_std()
                .unwrap_or(Duration::ZERO);

            info!(
                feed_name = &self.config.name,
                next_run_kst = %format_kst(next_run),
                trigger_type = ?trigger_type,
                wait_duration_ms = wait_duration.as_millis() as u64,
                "feed scheduler waiting"
            );

            tokio::select! {
                _ = shutdown.cancelled() => {
                    info!(feed_name = &self.config.name, "feed scheduler received shutdown signal");
                    return Ok(());
                }
                _ = tokio::time::sleep(wait_duration) => {
                    let scheduled_at = if trigger_type == ScrapeTriggerType::Retry {
                        self.pop_next_retry_run().unwrap_or(next_run)
                    } else {
                        next_run
                    };

                    let run_span = info_span!(
                        "feed_scrape_cycle",
                        feed_name = &self.config.name,
                        trigger_type = ?trigger_type,
                        scheduled_run_kst = %format_kst(scheduled_at)
                    );
                    let run_result = self.run_cycle().instrument(run_span).await;
                    let completed_at = Utc::now();
                    self.handle_scrape_result(
                        trigger_type,
                        scheduled_at,
                        completed_at,
                        run_result.as_ref().err(),
                    );

                    if let Err(err) = run_result {
                        warn!(
                            feed_name = &self.config.name,
                            error = %err,
                            retryable = err.is_retryable(),
                            "feed scrape cycle failed"
                        );
                    }
                }
            }
        }
    }

    pub async fn run_cycle(&self) -> Result<(), ScraperError> {
        let scrape_result = self
            .scraper
            .scrape_feeds(&self.repository, &self.sources)
            .await;
        match &scrape_result {
            Ok(stored) => info!(
                feed_name = &self.config.name,
                stored, "feed scrape completed"
            ),
            Err(err) => warn!(feed_name = &self.config.name, error = %err, "feed scrape failed"),
        }

        scrape_result.map(|_| ())
    }

    fn calculate_next_regular_run(&self, now: DateTime<Utc>) -> DateTime<Utc> {
        calculate_next_regular_run_for_hour(now, self.config.scrape_hour_kst)
    }

    fn build_retry_runs(
        &self,
        base_run: DateTime<Utc>,
        failed_at: DateTime<Utc>,
    ) -> Vec<DateTime<Utc>> {
        build_retry_runs_from_delays(base_run, failed_at, &self.config.retry_delays)
    }

    fn next_run_with_trigger(&self, now: DateTime<Utc>) -> (DateTime<Utc>, ScrapeTriggerType) {
        let next_regular = self.calculate_next_regular_run(now);

        if let Some(next_retry) = self.peek_next_retry_run()
            && next_retry <= next_regular
        {
            return (next_retry, ScrapeTriggerType::Retry);
        }

        (next_regular, ScrapeTriggerType::Regular)
    }

    fn handle_scrape_result(
        &self,
        trigger_type: ScrapeTriggerType,
        scheduled_at: DateTime<Utc>,
        completed_at: DateTime<Utc>,
        scrape_error: Option<&ScraperError>,
    ) {
        if scrape_error.is_none() {
            let cleared = self.clear_retry_runs();
            if cleared > 0 {
                info!(
                    feed_name = &self.config.name,
                    trigger_type = ?trigger_type,
                    cleared_retries = cleared,
                    "feed scrape succeeded; cleared retry queue"
                );
            }
            return;
        }

        if trigger_type != ScrapeTriggerType::Regular {
            warn!(
                feed_name = &self.config.name,
                trigger_type = ?trigger_type,
                scheduled_run_kst = %format_kst(scheduled_at),
                failed_at_kst = %format_kst(completed_at),
                remaining_retries = self.retry_run_count(),
                error = %scrape_error.expect("error should exist"),
                "feed retry scrape failed"
            );
            return;
        }

        let retry_runs = self.build_retry_runs(scheduled_at, completed_at);
        self.set_retry_runs(retry_runs.clone());

        if retry_runs.is_empty() {
            warn!(
                feed_name = &self.config.name,
                scheduled_run_kst = %format_kst(scheduled_at),
                failed_at_kst = %format_kst(completed_at),
                error = %scrape_error.expect("error should exist"),
                "feed scrape failed; no same-day retries queued"
            );
            return;
        }

        warn!(
            feed_name = &self.config.name,
            scheduled_run_kst = %format_kst(scheduled_at),
            failed_at_kst = %format_kst(completed_at),
            retry_count = retry_runs.len(),
            retry_runs_kst = ?retry_runs.iter().map(|run| format_kst(*run)).collect::<Vec<_>>(),
            error = %scrape_error.expect("error should exist"),
            "feed scrape failed; retry queue updated"
        );
    }

    fn set_retry_runs(&self, runs: Vec<DateTime<Utc>>) {
        let mut guard = self.retry_runs.lock();
        if runs.is_empty() {
            guard.clear();
            return;
        }
        *guard = runs.into_iter().collect();
    }

    fn clear_retry_runs(&self) -> usize {
        let mut guard = self.retry_runs.lock();
        let cleared = guard.len();
        guard.clear();
        cleared
    }

    fn peek_next_retry_run(&self) -> Option<DateTime<Utc>> {
        let guard = self.retry_runs.lock();
        guard.front().copied()
    }

    fn pop_next_retry_run(&self) -> Option<DateTime<Utc>> {
        let mut guard = self.retry_runs.lock();
        guard.pop_front()
    }

    fn retry_run_count(&self) -> usize {
        let guard = self.retry_runs.lock();
        guard.len()
    }
}
