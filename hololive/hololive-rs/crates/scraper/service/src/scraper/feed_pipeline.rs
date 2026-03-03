use futures::{StreamExt, stream};
use scraper_core::{
    error::ScraperError,
    model::MajorEventType,
};
use tracing::{info, warn};

use super::{FeedSource, Scraper, incremental::dedup_events_by_canonical_link};
use crate::repository::FeedRepository;

impl Scraper {
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
    pub async fn scrape_feeds<R>(
        &self,
        repository: &R,
        sources: &[FeedSource],
    ) -> Result<usize, ScraperError>
    where
        R: FeedRepository + ?Sized,
    {
        let mut all_events = Vec::new();
        let mut attempted_feeds = 0usize;
        let mut failed_feeds = 0usize;
        let mut total_skipped_pages = 0usize;

        // sources를 Vec으로 owned clone: stream::iter의 HRTB(&'a FeedSource) 제약 해소
        let owned_sources: Vec<FeedSource> = sources.to_vec();
        let mut feed_tasks = stream::iter(owned_sources.into_iter().map(|source| {
            let scraper = self.clone();
            async move {
                let normalized_url = source.feed_url.trim().to_owned();
                if normalized_url.is_empty() {
                    return None;
                }

                let result = scraper
                    .scrape_all_pages(repository, &normalized_url, source.event_type.clone())
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
    pub async fn scrape_and_store<R>(&self, repository: &R) -> Result<usize, ScraperError>
    where
        R: FeedRepository + ?Sized,
    {
        let sources = self.build_default_feed_sources();
        self.scrape_feeds(repository, &sources).await
    }
}
