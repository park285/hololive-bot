use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventLinkStatus, MajorEventType},
};

pub type BoxFuture<'a, T> = Pin<Box<dyn Future<Output = T> + Send + 'a>>;

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RecentExternalIds {
    pub external_ids: Vec<String>,
    pub latest_pub_date: Option<DateTime<Utc>>,
}

pub trait FeedRepository: Send + Sync {
    fn upsert_event<'a>(
        &'a self,
        event: &'a MajorEvent,
    ) -> BoxFuture<'a, Result<i32, ScraperError>>;

    fn get_recent_external_ids(
        &self,
        event_type: MajorEventType,
        limit: i64,
    ) -> BoxFuture<'_, Result<RecentExternalIds, ScraperError>>;
}

pub trait LinkCheckRepository: Send + Sync {
    fn list_events_needing_link_check(
        &self,
        stale_before: DateTime<Utc>,
        limit: i64,
    ) -> BoxFuture<'_, Result<Vec<MajorEvent>, ScraperError>>;

    fn update_event_link_status(
        &self,
        event_id: i32,
        status: MajorEventLinkStatus,
        checked_at: DateTime<Utc>,
    ) -> BoxFuture<'_, Result<(), ScraperError>>;
}

pub trait MaintenanceRepository: LinkCheckRepository + Send + Sync {
    fn update_expired_events(&self) -> BoxFuture<'_, Result<u64, ScraperError>>;
}
