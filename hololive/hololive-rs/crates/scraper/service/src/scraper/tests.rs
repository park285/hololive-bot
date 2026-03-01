use std::collections::HashSet;

use chrono::{TimeZone, Utc};
use scraper_core::model::{MajorEvent, MajorEventLinkStatus, MajorEventStatus, MajorEventType};

use super::{IncrementalCursor, canonical_event_link_key, should_stop_incremental_scan};

fn sample_event(
    external_id: &str,
    link: &str,
    pub_date: Option<chrono::DateTime<Utc>>,
) -> MajorEvent {
    let now = Utc::now();
    MajorEvent {
        id: 0,
        external_id: external_id.to_string(),
        event_type: MajorEventType::Event,
        title: "sample".to_string(),
        link: link.to_string(),
        description: None,
        members: Vec::new(),
        pub_date,
        event_start_date: None,
        event_end_date: None,
        event_dates: Vec::new(),
        status: MajorEventStatus::Active,
        link_status: MajorEventLinkStatus::Unchecked,
        link_checked_at: None,
        notified_at: None,
        notified_week: None,
        notified_month: None,
        created_at: now,
        updated_at: now,
    }
}

#[test]
fn canonical_link_normalizes_en_path_and_trailing_slash() {
    let normalized = canonical_event_link_key(
        "https://hololive.hololivepro.com/en/news/item-1/?utm_source=test",
    )
    .expect("canonical key should exist");

    assert_eq!(normalized, "hololive.hololivepro.com/news/item-1");
}

#[test]
fn canonical_link_handles_non_url_input() {
    let normalized =
        canonical_event_link_key("not-a-url-with-slash/").expect("canonical key should exist");
    assert_eq!(normalized, "not-a-url-with-slash");
}

#[test]
fn should_stop_incremental_scan_when_all_events_are_known() {
    let mut known_external_ids = HashSet::new();
    known_external_ids.insert("ext-1".to_string());

    let cursor = IncrementalCursor {
        known_external_ids,
        known_canonical_links: HashSet::new(),
        latest_pub_date: None,
    };

    let events = vec![sample_event("ext-1", "https://example.com/a", None)];
    assert!(should_stop_incremental_scan(&events, Some(&cursor)));
}

#[test]
fn should_not_stop_incremental_scan_when_any_event_is_new() {
    let mut known_external_ids = HashSet::new();
    known_external_ids.insert("ext-1".to_string());

    let cursor = IncrementalCursor {
        known_external_ids,
        known_canonical_links: HashSet::new(),
        latest_pub_date: None,
    };

    let events = vec![sample_event("ext-2", "https://example.com/a", None)];
    assert!(!should_stop_incremental_scan(&events, Some(&cursor)));
}

#[test]
fn should_stop_incremental_scan_by_pub_date_signal() {
    let latest_pub_date = Utc
        .with_ymd_and_hms(2026, 2, 22, 0, 0, 0)
        .single()
        .expect("valid datetime");
    let older_pub_date = Utc
        .with_ymd_and_hms(2026, 2, 21, 0, 0, 0)
        .single()
        .expect("valid datetime");

    let cursor = IncrementalCursor {
        known_external_ids: HashSet::new(),
        known_canonical_links: HashSet::new(),
        latest_pub_date: Some(latest_pub_date),
    };

    let events = vec![sample_event(
        "ext-x",
        "https://example.com/a",
        Some(older_pub_date),
    )];
    assert!(should_stop_incremental_scan(&events, Some(&cursor)));
}

#[test]
fn should_stop_incremental_scan_by_canonical_link_signal() {
    let mut known_canonical_links = HashSet::new();
    known_canonical_links.insert("hololive.hololivepro.com/news/item-1".to_string());

    let cursor = IncrementalCursor {
        known_external_ids: HashSet::new(),
        known_canonical_links,
        latest_pub_date: None,
    };

    let events = vec![sample_event(
        "new-id",
        "https://hololive.hololivepro.com/en/news/item-1/?utm_source=test",
        None,
    )];
    assert!(should_stop_incremental_scan(&events, Some(&cursor)));
}
