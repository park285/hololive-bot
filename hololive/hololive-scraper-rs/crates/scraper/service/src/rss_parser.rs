use chrono::{DateTime, Utc};
use rss::Channel;
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventLinkStatus, MajorEventStatus, MajorEventType},
};

#[derive(Debug, Clone, Copy, Default)]
pub struct RssParser;

impl RssParser {
    pub fn new() -> Self {
        Self
    }

    pub fn parse(
        &self,
        data: &[u8],
        event_type: MajorEventType,
    ) -> Result<Vec<MajorEvent>, ScraperError> {
        let mut cursor = std::io::Cursor::new(data);
        let feed = Channel::read_from(&mut cursor)
            .map_err(|err| ScraperError::XmlParse(err.to_string()))?;

        let now = Utc::now();
        let events = feed
            .items()
            .iter()
            .map(|item| MajorEvent {
                id: 0,
                external_id: item.link().unwrap_or_default().to_string(),
                event_type: event_type.clone(),
                title: item.title().unwrap_or_default().to_string(),
                link: item.link().unwrap_or_default().to_string(),
                description: if item
                    .content()
                    .or_else(|| item.description())
                    .unwrap_or_default()
                    .is_empty()
                {
                    None
                } else {
                    Some(
                        item.content()
                            .or_else(|| item.description())
                            .unwrap_or_default()
                            .to_string(),
                    )
                },
                members: item
                    .categories()
                    .iter()
                    .map(|category| category.name().to_string())
                    .collect(),
                pub_date: parse_pub_date(item.pub_date().unwrap_or_default()),
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
            })
            .collect();

        Ok(events)
    }
}

pub fn parse_pub_date(date_str: &str) -> Option<DateTime<Utc>> {
    if let Ok(parsed) = DateTime::parse_from_rfc2822(date_str) {
        return Some(parsed.with_timezone(&Utc));
    }

    const FORMATS: &[&str] = &[
        "%a, %d %b %Y %H:%M:%S %z",
        "%a, %d %b %Y %H:%M:%S %Z",
        "%a, %-d %b %Y %H:%M:%S %z",
        "%a, %-d %b %Y %H:%M:%S %Z",
    ];

    for format in FORMATS {
        if let Ok(parsed) = DateTime::parse_from_str(date_str, format) {
            return Some(parsed.with_timezone(&Utc));
        }
    }

    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Datelike;
    use scraper_core::model::MajorEventType;

    fn fixture_events_feed() -> Vec<u8> {
        let path = format!("{}/testdata/events_feed.xml", env!("CARGO_MANIFEST_DIR"));
        std::fs::read(path).expect("fixture should be readable")
    }

    #[test]
    fn parse_basic_feed() {
        let parser = RssParser::new();
        let data = fixture_events_feed();

        let events = parser
            .parse(&data, MajorEventType::Event)
            .expect("parse should succeed");

        assert_eq!(events.len(), 3);
        assert_eq!(events[0].title, "hololive SUPER EXPO 2026");
        assert_eq!(
            events[0].link,
            "https://hololive.hololivepro.com/events/superexpo2026/"
        );
        assert_eq!(events[0].members.len(), 2);
        assert_eq!(events[0].members[0], "ときのそら");
        assert_eq!(events[0].event_type, MajorEventType::Event);
    }

    #[test]
    fn parse_empty_feed() {
        let parser = RssParser::new();
        let data = br#"<?xml version="1.0"?><rss><channel><title>Empty</title></channel></rss>"#;

        let events = parser
            .parse(data, MajorEventType::Event)
            .expect("parse should succeed");

        assert!(events.is_empty());
    }

    #[test]
    fn parse_invalid_xml() {
        let parser = RssParser::new();
        let data = b"not valid xml";
        let err = parser.parse(data, MajorEventType::Event).err();
        assert!(err.is_some());
    }

    #[test]
    fn parse_pub_date_formats() {
        let parsed = parse_pub_date("Thu, 09 Jan 2025 05:00:00 +0000");
        assert!(parsed.is_some());
        assert_eq!(parsed.expect("must parse").year(), 2025);

        let parsed = parse_pub_date("Fri, 12 Dec 2025 02:50:11 +0000");
        assert!(parsed.is_some());
        assert_eq!(parsed.expect("must parse").year(), 2025);

        let parsed = parse_pub_date("Fri, 12 Dec 2025 02:50:11 GMT");
        assert!(parsed.is_some());
        assert_eq!(parsed.expect("must parse").year(), 2025);

        assert!(parse_pub_date("2025-01-09").is_none());
    }

    #[test]
    fn parse_with_news_type() {
        let parser = RssParser::new();
        let data = r#"<?xml version="1.0"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
<title>NEWS</title>
<item>
<title>ホロライブ新商品発売</title>
<link>https://example.com/news/1</link>
<pubDate>Thu, 09 Jan 2025 05:00:00 +0000</pubDate>
</item>
</channel>
</rss>"#;

        let events = parser
            .parse(data.as_bytes(), MajorEventType::News)
            .expect("parse should succeed");

        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, MajorEventType::News);
    }

    #[test]
    fn parse_content_encoded_and_categories() {
        let parser = RssParser::new();
        let data = fixture_events_feed();

        let events = parser
            .parse(&data, MajorEventType::Event)
            .expect("parse should succeed");

        let first_description = events[0]
            .description
            .as_deref()
            .expect("description should be parsed");
        assert!(
            first_description.contains("2026年3月6日"),
            "content:encoded namespace parsing failed"
        );

        assert_eq!(events[1].members, vec!["星街すいせい".to_string()]);
        assert!(events[1].description.is_some());
    }
}
