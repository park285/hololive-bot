use std::collections::HashSet;

use scraper_core::{error::ScraperError, model::MajorEventType};

use super::incremental::canonical_event_link_key;
use crate::repository::FeedRepository;

#[derive(Debug, Clone)]
pub(super) struct IncrementalCursor {
    pub(super) known_external_ids: HashSet<String>,
    pub(super) known_canonical_links: HashSet<String>,
    pub(super) latest_pub_date: Option<chrono::DateTime<chrono::Utc>>,
}

pub(super) async fn load_incremental_cursor_from_repository<R>(
    repository: &R,
    event_type: MajorEventType,
    incremental_cursor_limit: i64,
) -> Result<Option<IncrementalCursor>, ScraperError>
where
    R: FeedRepository + ?Sized,
{
    let cursor_data = repository
        .get_recent_external_ids(event_type, incremental_cursor_limit)
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
