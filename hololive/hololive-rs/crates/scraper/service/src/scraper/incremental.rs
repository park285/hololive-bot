use std::collections::HashSet;

use url::Url;

use super::{IncrementalCursor, MajorEvent};

pub(super) fn should_stop_incremental_scan(
    events: &[MajorEvent],
    cursor: Option<&IncrementalCursor>,
) -> bool {
    let Some(cursor) = cursor else {
        return false;
    };

    if events.is_empty() {
        return false;
    }

    if cursor.known_external_ids.is_empty()
        && cursor.known_canonical_links.is_empty()
        && cursor.latest_pub_date.is_none()
    {
        return false;
    }

    let mut has_known_signal = false;

    for event in events {
        let external_id = event.external_id.trim();
        let known_by_external_id =
            !external_id.is_empty() && cursor.known_external_ids.contains(external_id);

        let canonical_key = canonical_event_link_key(&event.link)
            .or_else(|| canonical_event_link_key(&event.external_id));
        let known_by_canonical_link = canonical_key
            .as_ref()
            .is_some_and(|key| cursor.known_canonical_links.contains(key));

        let known_by_pub_date = cursor
            .latest_pub_date
            .zip(event.pub_date)
            .is_some_and(|(latest, pub_date)| pub_date < latest);

        if !known_by_external_id && !known_by_canonical_link && !known_by_pub_date {
            return false;
        }

        has_known_signal = true;
    }

    has_known_signal
}

pub(super) fn dedup_events_by_canonical_link(events: Vec<MajorEvent>) -> Vec<MajorEvent> {
    if events.len() <= 1 {
        return events;
    }

    let mut deduped = Vec::with_capacity(events.len());
    let mut seen = HashSet::with_capacity(events.len());

    for event in events {
        let key =
            canonical_event_link_key(&event.link).unwrap_or_else(|| event.external_id.clone());
        if key.is_empty() {
            deduped.push(event);
            continue;
        }

        if seen.insert(key) {
            deduped.push(event);
        }
    }

    deduped
}

pub(super) fn canonical_event_link_key(raw_url: &str) -> Option<String> {
    let trimmed = raw_url.trim();
    if trimmed.is_empty() {
        return None;
    }

    let parsed = match Url::parse(trimmed) {
        Ok(url) => url,
        Err(_) => return Some(trimmed.trim_end_matches('/').to_string()),
    };

    let host = parsed.host_str()?.to_ascii_lowercase();
    let mut path = parsed.path().trim().to_string();

    if path.is_empty() {
        path = "/".to_string();
    }

    if host == "hololive.hololivepro.com"
        && let Some(stripped) = path.strip_prefix("/en/")
    {
        path = format!("/{stripped}");
    }

    if path != "/" {
        path = path.trim_end_matches('/').to_string();
        if path.is_empty() {
            path = "/".to_string();
        }
    }

    Some(format!("{host}{path}"))
}
