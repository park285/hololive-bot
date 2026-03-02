use std::{
    collections::{HashMap, HashSet},
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::Duration,
};

use backon::{ExponentialBuilder, Retryable};
use futures::{StreamExt, stream};
use parking_lot::RwLock;
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventType},
};
use scraper_infra::repository::Repository;
use tracing::{info, warn};

use crate::{date_extractor::DateExtractor, rss_parser::RssParser};

const DEFAULT_EVENT_FEED_URL: &str = "https://hololive.hololivepro.com/events/feed/";
const DEFAULT_NEWS_FEED_URL: &str = "https://hololive.hololivepro.com/news/feed/";
const DEFAULT_EN_NEWS_FEED_URL: &str = "https://hololive.hololivepro.com/en/news/feed/";

#[derive(Debug, Clone)]
pub struct ScraperConfig {
    pub user_agent: String,
    pub event_feed_url: String,
    pub news_feed_urls: Vec<String>,
    pub max_retries: usize,
    pub retry_delay: Duration,
    pub max_pages: usize,
    pub incremental_cursor_limit: i64,
    pub page_delay: Duration,
    pub feed_concurrency: usize,
    /// 개별 HTTP 요청 단위 타임아웃 (글로벌 client timeout과 이중 적용)
    pub request_timeout: Duration,
}

impl Default for ScraperConfig {
    fn default() -> Self {
        Self {
            user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36".to_owned(),
            event_feed_url: DEFAULT_EVENT_FEED_URL.to_owned(),
            news_feed_urls: vec![
                DEFAULT_NEWS_FEED_URL.to_owned(),
                DEFAULT_EN_NEWS_FEED_URL.to_owned(),
            ],
            max_retries: 4,
            retry_delay: Duration::from_secs(1),
            max_pages: 20,
            incremental_cursor_limit: 200,
            page_delay: Duration::from_secs(2),
            feed_concurrency: 3,
            request_timeout: Duration::from_secs(30),
        }
    }
}

#[derive(Debug, Clone, Default)]
struct FeedMetadata {
    e_tag: String,
    last_modified: String,
}

/// 피드 소스 정보 (이름, 이벤트 타입, URL)
#[derive(Debug, Clone)]
pub struct FeedSource {
    pub name: String,
    pub event_type: MajorEventType,
    pub feed_url: String,
}

#[derive(Debug, Clone)]
struct IncrementalCursor {
    known_external_ids: HashSet<String>,
    known_canonical_links: HashSet<String>,
    latest_pub_date: Option<chrono::DateTime<chrono::Utc>>,
}

#[derive(Clone)]
pub struct Scraper {
    client: reqwest::Client,
    parser: RssParser,
    date_extractor: DateExtractor,
    config: ScraperConfig,
    feed_metadata_by_page_url: Arc<RwLock<HashMap<String, FeedMetadata>>>,
}

impl Scraper {
    pub fn new(client: reqwest::Client, config: ScraperConfig) -> Self {
        Self {
            client,
            parser: RssParser::new(),
            date_extractor: DateExtractor::new(),
            config,
            feed_metadata_by_page_url: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub fn config(&self) -> &ScraperConfig {
        &self.config
    }

    pub fn parse_feed(
        &self,
        data: &[u8],
        event_type: MajorEventType,
    ) -> Result<Vec<MajorEvent>, ScraperError> {
        self.parser.parse(data, event_type)
    }

    pub fn enrich_event_dates(&self, event: &mut MajorEvent) {
        if let Some(description) = &event.description {
            let dates = self.date_extractor.extract_event_dates(description);
            if !dates.is_empty() {
                event.event_dates = dates;
                event.set_event_dates_from_parsed();
            }
        }

        event.apply_fallback_event_date();
    }

    /// 설정에서 기본 피드 소스 목록을 생성
    fn build_default_feed_sources(&self) -> Vec<FeedSource> {
        let mut sources = vec![FeedSource {
            name: "event".to_owned(),
            event_type: MajorEventType::Event,
            feed_url: self.config.event_feed_url.clone(),
        }];

        for (index, feed_url) in self.config.news_feed_urls.iter().enumerate() {
            let name = if index == 1 { "en-news" } else { "news" };
            sources.push(FeedSource {
                name: name.to_owned(),
                event_type: MajorEventType::News,
                feed_url: feed_url.clone(),
            });
        }

        sources
    }

    /// 지정된 피드 소스 목록을 스크랩하여 이벤트를 저장; 저장된 이벤트 수 반환
    pub async fn scrape_feeds(
        &self,
        repository: &Repository,
        sources: &[FeedSource],
    ) -> Result<usize, ScraperError> {
        let mut all_events = Vec::new();
        let mut attempted_feeds = 0usize;
        let mut failed_feeds = 0usize;
        let mut total_skipped_pages = 0usize;

        // sources를 Vec으로 owned clone: stream::iter의 HRTB(&'a FeedSource) 제약 해소
        let owned_sources: Vec<FeedSource> = sources.to_vec();
        let mut feed_tasks = stream::iter(owned_sources.into_iter().map(|source| {
            let scraper = self.clone();
            let repository = repository.clone();
            async move {
                let normalized_url = source.feed_url.trim().to_owned();
                if normalized_url.is_empty() {
                    return None;
                }

                let result = scraper
                    .scrape_all_pages(&repository, &normalized_url, source.event_type.clone())
                    .await;

                Some((source, normalized_url, result))
            }
        }))
        .buffer_unordered(self.config.feed_concurrency.max(1));

        while let Some(task_result) = feed_tasks.next().await {
            let Some((source, normalized_url, result)) = task_result else {
                continue;
            };

            attempted_feeds += 1;
            match result {
                Ok((events, skipped_pages)) => {
                    total_skipped_pages += skipped_pages;
                    all_events.extend(events);
                }
                Err(err) => {
                    failed_feeds += 1;
                    warn!(
                        feed_name = &source.name,
                        feed = normalized_url,
                        event_type = %source.event_type,
                        error = %err,
                        retryable = err.is_retryable(),
                        "scrape feed failed"
                    );
                }
            }
        }

        if attempted_feeds > 0 && attempted_feeds == failed_feeds {
            return Err(ScraperError::AllFeedsFailed(
                "all configured feeds failed".to_owned(),
            ));
        }

        let deduped_events = dedup_events_by_canonical_link(all_events);
        let mut stored = 0usize;
        for event in &deduped_events {
            if let Err(err) = repository.upsert_event(event).await {
                warn!(
                    event_external_id = event.external_id,
                    event_title = event.title,
                    error = %err,
                    "event upsert failed"
                );
                continue;
            }
            stored += 1;
        }

        info!(
            total_scraped = deduped_events.len(),
            total_stored = stored,
            skipped_pages = total_skipped_pages,
            "scrape cycle finished"
        );

        Ok(stored)
    }

    /// 설정 기반 기본 피드 소스로 스크랩 실행 (하위호환 유지)
    pub async fn scrape_and_store(&self, repository: &Repository) -> Result<usize, ScraperError> {
        let sources = self.build_default_feed_sources();
        self.scrape_feeds(repository, &sources).await
    }

    async fn scrape_all_pages(
        &self,
        repository: &Repository,
        base_url: &str,
        event_type: MajorEventType,
    ) -> Result<(Vec<MajorEvent>, usize), ScraperError> {
        let cursor = self
            .load_incremental_cursor_from_repository(repository, event_type.clone())
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
        let request = self.apply_conditional_request_headers(request, &page_url);

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

        self.save_feed_metadata(
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

    async fn load_incremental_cursor_from_repository(
        &self,
        repository: &Repository,
        event_type: MajorEventType,
    ) -> Result<Option<IncrementalCursor>, ScraperError> {
        let cursor_data = repository
            .get_recent_external_ids(event_type, self.config.incremental_cursor_limit)
            .await?;

        if cursor_data.external_ids.is_empty() && cursor_data.latest_pub_date.is_none() {
            return Ok(None);
        }

        let mut known_external_ids = HashSet::with_capacity(cursor_data.external_ids.len());
        let mut known_canonical_links = HashSet::with_capacity(cursor_data.external_ids.len());

        for external_id in cursor_data.external_ids {
            let normalized = external_id.trim();
            if normalized.is_empty() {
                continue;
            }

            known_external_ids.insert(normalized.to_owned());
            if let Some(key) = canonical_event_link_key(normalized) {
                known_canonical_links.insert(key);
            }
        }

        Ok(Some(IncrementalCursor {
            known_external_ids,
            known_canonical_links,
            latest_pub_date: cursor_data.latest_pub_date,
        }))
    }

    fn apply_conditional_request_headers(
        &self,
        mut request: reqwest::RequestBuilder,
        page_url: &str,
    ) -> reqwest::RequestBuilder {
        let Some(metadata) = self.get_feed_metadata(page_url) else {
            return request;
        };

        if !metadata.e_tag.is_empty() {
            request = request.header(reqwest::header::IF_NONE_MATCH, metadata.e_tag);
        }
        if !metadata.last_modified.is_empty() {
            request = request.header(reqwest::header::IF_MODIFIED_SINCE, metadata.last_modified);
        }

        request
    }

    fn get_feed_metadata(&self, page_url: &str) -> Option<FeedMetadata> {
        let guard = self.feed_metadata_by_page_url.read();
        guard.get(page_url).cloned()
    }

    fn save_feed_metadata(
        &self,
        page_url: &str,
        e_tag: Option<&reqwest::header::HeaderValue>,
        last_modified: Option<&reqwest::header::HeaderValue>,
    ) {
        let normalize_header = |value: Option<&reqwest::header::HeaderValue>| -> String {
            value
                .and_then(|header| header.to_str().ok())
                .unwrap_or_default()
                .trim()
                .to_owned()
        };

        let normalized_e_tag = normalize_header(e_tag);
        let normalized_last_modified = normalize_header(last_modified);

        if normalized_e_tag.is_empty() && normalized_last_modified.is_empty() {
            return;
        }

        let mut guard = self.feed_metadata_by_page_url.write();
        let entry = guard.entry(page_url.to_owned()).or_default();

        if !normalized_e_tag.is_empty() {
            entry.e_tag = normalized_e_tag;
        }
        if !normalized_last_modified.is_empty() {
            entry.last_modified = normalized_last_modified;
        }
    }
}

mod incremental;
use incremental::{
    canonical_event_link_key, dedup_events_by_canonical_link, should_stop_incremental_scan,
};

#[cfg(test)]
mod tests;
