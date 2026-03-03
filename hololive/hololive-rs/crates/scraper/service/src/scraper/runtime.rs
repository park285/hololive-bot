use std::{
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::Duration,
};

use backon::{ExponentialBuilder, Retryable};
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventType},
};
use tracing::{info, warn};

use super::{
    Scraper, incremental::should_stop_incremental_scan,
    incremental_cursor::load_incremental_cursor_from_repository,
};
use crate::repository::FeedRepository;

impl Scraper {
    pub(super) async fn scrape_all_pages<R>(
        &self,
        repository: &R,
        base_url: &str,
        event_type: MajorEventType,
    ) -> Result<(Vec<MajorEvent>, usize), ScraperError>
    where
        R: FeedRepository + ?Sized,
    {
        let cursor = load_incremental_cursor_from_repository(
            repository,
            event_type.clone(),
            self.config.incremental_cursor_limit,
        )
        .await?;

        let mut all_events = Vec::new();
        let mut failed_pages = Vec::new();
        let mut consecutive_failures = 0usize;

        for page in 1..=self.config.max_pages.max(1) {
            match self.scrape_page(base_url, page, event_type.clone()).await {
                Ok(events) => {
                    consecutive_failures = 0;

                    if events.is_empty() {
                        break;
                    }

                    if should_stop_incremental_scan(&events, cursor.as_ref()) {
                        info!(
                            page,
                            event_type = %event_type,
                            events = events.len(),
                            "incremental stop: known page reached"
                        );
                        break;
                    }

                    all_events.extend(events);
                }
                Err(err) => {
                    if page == 1 {
                        return Err(err);
                    }

                    consecutive_failures += 1;
                    failed_pages.push(page);
                    warn!(
                        page,
                        event_type = %event_type,
                        consecutive_failures,
                        error = %err,
                        "scrape page failed; skipping page"
                    );

                    if consecutive_failures >= 3 {
                        warn!(
                            page,
                            event_type = %event_type,
                            "stopping pagination due to consecutive failures"
                        );
                        break;
                    }
                }
            }

            if page < self.config.max_pages {
                tokio::time::sleep(self.config.page_delay).await;
            }
        }

        let (recovered, skipped_pages) = self
            .backfill_failed_pages(base_url, event_type, &failed_pages)
            .await;
        if !recovered.is_empty() {
            all_events.extend(recovered);
        }

        Ok((all_events, skipped_pages.len()))
    }

    async fn backfill_failed_pages(
        &self,
        base_url: &str,
        event_type: MajorEventType,
        failed_pages: &[usize],
    ) -> (Vec<MajorEvent>, Vec<usize>) {
        if failed_pages.is_empty() {
            return (Vec::new(), Vec::new());
        }

        info!(
            event_type = %event_type,
            failed_count = failed_pages.len(),
            "backfill: retrying failed pages"
        );

        let mut recovered_events = Vec::new();
        let mut skipped_pages = Vec::new();

        for &page in failed_pages {
            tokio::time::sleep(self.config.page_delay).await;

            match self.scrape_page(base_url, page, event_type.clone()).await {
                Ok(events) => {
                    if !events.is_empty() {
                        info!(
                            page,
                            event_type = %event_type,
                            events = events.len(),
                            "backfill page recovered"
                        );
                        recovered_events.extend(events);
                    }
                }
                Err(err) => {
                    warn!(
                        page,
                        event_type = %event_type,
                        error = %err,
                        "backfill page failed"
                    );
                    skipped_pages.push(page);
                }
            }
        }

        (recovered_events, skipped_pages)
    }

    async fn scrape_page(
        &self,
        base_url: &str,
        page: usize,
        event_type: MajorEventType,
    ) -> Result<Vec<MajorEvent>, ScraperError> {
        let max_attempts = self.config.max_retries.max(1);
        let attempt_counter = Arc::new(AtomicUsize::new(0));
        let attempt_for_notify = Arc::clone(&attempt_counter);
        let event_type_for_notify = event_type.clone();

        let max_interval = self
            .config
            .retry_delay
            .saturating_mul(2u32.saturating_pow(10));

        (|| {
            let attempt_counter = Arc::clone(&attempt_counter);
            let event_type = event_type.clone();
            async move {
                attempt_counter.fetch_add(1, Ordering::Relaxed);
                self.scrape_page_once(base_url, page, event_type).await
            }
        })
        .retry(
            ExponentialBuilder::default()
                .with_min_delay(self.config.retry_delay)
                .with_max_delay(max_interval)
                .with_factor(2.0)
                .with_max_times(max_attempts),
        )
        .when(|e: &ScraperError| e.is_retryable())
        .notify(|err: &ScraperError, dur: Duration| {
            let attempt = attempt_for_notify.load(Ordering::Relaxed);
            warn!(
                attempt,
                page,
                event_type = %event_type_for_notify,
                backoff_ms = dur.as_millis() as u64,
                error = %err,
                "retrying scrape page"
            );
        })
        .await
    }

    async fn scrape_page_once(
        &self,
        base_url: &str,
        page: usize,
        event_type: MajorEventType,
    ) -> Result<Vec<MajorEvent>, ScraperError> {
        let page_url = if page == 1 {
            base_url.to_owned()
        } else {
            format!("{base_url}?paged={page}")
        };

        let request = self
            .client
            .get(&page_url)
            .header(reqwest::header::USER_AGENT, &self.config.user_agent)
            // per-request 타임아웃: 글로벌 client timeout과 이중 적용
            .timeout(self.config.request_timeout);
        let request = self
            .feed_metadata_store
            .apply_conditional_request_headers(request, &page_url);

        let response = request
            .send()
            .await
            .map_err(|e| ScraperError::Http(e.to_string()))?;
        let status = response.status();

        if status == reqwest::StatusCode::NOT_MODIFIED || status == reqwest::StatusCode::NOT_FOUND {
            return Ok(Vec::new());
        }

        if !status.is_success() {
            return Err(ScraperError::HttpStatus {
                code: status.as_u16(),
                message: status.to_string(),
            });
        }

        self.feed_metadata_store.save_feed_metadata(
            &page_url,
            response.headers().get(reqwest::header::ETAG),
            response.headers().get(reqwest::header::LAST_MODIFIED),
        );

        let body = response
            .bytes()
            .await
            .map_err(|e| ScraperError::Http(e.to_string()))?;
        let mut events = self.parse_feed(&body, event_type)?;
        for event in &mut events {
            self.enrich_event_dates(event);
        }

        Ok(events)
    }
}
