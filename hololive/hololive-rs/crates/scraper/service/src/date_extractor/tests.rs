use super::*;

fn date_strings(dates: &[NaiveDate]) -> Vec<String> {
    dates
        .iter()
        .map(|date| date.format("%Y-%m-%d").to_string())
        .collect()
}

#[test]
fn extract_supports_japanese_slash_hyphen_and_dot_formats() {
    let extractor = DateExtractor::new();

    let dates = extractor.extract("<p>2026年1月10日 / 2026/2/15 / 2026-03-16 / 2026. 4.17</p>");
    assert_eq!(
        date_strings(&dates),
        vec!["2026-01-10", "2026-02-15", "2026-03-16", "2026-04-17"]
    );
}

#[test]
fn extract_deduplicates_same_date() {
    let extractor = DateExtractor::new();

    let dates = extractor.extract("<p>2026年1月10日 開催日: 2026年1月10日</p>");
    assert_eq!(date_strings(&dates), vec!["2026-01-10"]);
}

#[test]
fn extract_returns_empty_when_no_dates() {
    let extractor = DateExtractor::new();
    let dates = extractor.extract("<p>TBA</p>");
    assert!(dates.is_empty());
}

#[test]
fn extract_with_context_supports_short_japanese_date() {
    let extractor = DateExtractor::new();

    let dates = extractor.extract_with_context("<p>3月6日（金）</p>", 2026);
    assert_eq!(date_strings(&dates), vec!["2026-03-06"]);
}

#[test]
fn parse_capture_ymd_rejects_invalid_calendar_dates() {
    let captures = JAPANESE_DATE_PATTERN
        .captures("2027年2月30日")
        .expect("capture should exist");
    assert!(parse_capture_ymd(&captures, 1, 2, 3).is_none());

    let captures = JAPANESE_DATE_PATTERN
        .captures("2028年2月29日")
        .expect("capture should exist");
    assert_eq!(
        parse_capture_ymd(&captures, 1, 2, 3)
            .expect("leap date should parse")
            .format("%Y-%m-%d")
            .to_string(),
        "2028-02-29"
    );
}

#[test]
fn extract_event_dates_filters_ticket_dates() {
    let extractor = DateExtractor::new();

    let html = r#"<h6>開催日時</h6><p>2027年2月21日（土）</p><h6>チケット販売</h6><p>2026年11月25日より</p>"#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-02-21"]);
}

#[test]
fn extract_event_dates_supports_multi_day_range() {
    let extractor = DateExtractor::new();

    let html = r#"<p>開催期間：2027年3月6日（金）～8日（日）</p>"#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-03-06", "2027-03-08"]);
}

#[test]
fn extract_event_dates_handles_year_rollover_multi_day_range() {
    let extractor = DateExtractor::new();

    let html = r#"<p>開催期間：2027年12月30日～1月2日</p>"#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-12-30", "2028-01-02"]);
}

#[test]
fn extract_event_dates_falls_back_to_cluster_without_keywords() {
    let extractor = DateExtractor::new();

    let html = r#"<p>2027年5月1日 2027年5月2日 2027年5月3日</p>"#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(
        date_strings(&dates),
        vec!["2027-05-01", "2027-05-02", "2027-05-03"]
    );
}

#[test]
fn extract_event_dates_strips_script_and_style_blocks() {
    let extractor = DateExtractor::new();

    let html = r#"<p>開催日時 2027年7月1日</p><script>2025年1月1日</script><style>.x{}</style>"#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-07-01"]);
}

#[test]
fn extract_event_dates_applies_max_context_distance() {
    let extractor = DateExtractor::new();

    let padding = "あ".repeat(200);
    let html = format!("<p>開催日時{padding}</p><p>チケット販売 2027年5月1日</p>");
    let dates = extractor.extract_event_dates(&html);

    assert_eq!(date_strings(&dates), vec!["2027-05-01"]);
}

#[test]
fn extract_event_dates_treats_small_distance_difference_as_tie() {
    let extractor = DateExtractor::new();

    let html = "<p>開催チケット2027年6月15日</p>";
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-06-15"]);
}

#[test]
fn extract_event_dates_prefers_event_dates_over_archive_deadline() {
    let extractor = DateExtractor::new();

    let html = r#"
            <h6>開催日時</h6><p>2027年1月17日 2027年1月18日</p>
            <h6>アーカイブ視聴期限</h6><p>2027年2月18日</p>
        "#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-01-17", "2027-01-18"]);
}

#[test]
fn extract_event_dates_prefers_dot_style_event_date() {
    let extractor = DateExtractor::new();

    let html = r#"
            <h6>開催日時</h6><p>2027.1.17 Sat - 2027.1.18 Sun</p>
            <h6>アーカイブ視聴期限</h6><p>2027年2月18日</p>
        "#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-01-17", "2027-01-18"]);
}

#[test]
fn extract_event_dates_section_scoring_prefers_event_section() {
    let extractor = DateExtractor::new();

    let html = r#"
            <h6>開催日時</h6><p>2027年5月10日</p>
            <h6>アーカイブ視聴期限</h6><p>2027年6月10日</p>
        "#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-05-10"]);
}

#[test]
fn extract_event_dates_handles_nested_header_tags_and_forward_mapping() {
    let extractor = DateExtractor::new();

    let html = r#"
            <h6><span class="title">開催日時</span></h6><p>2027年9月15日</p>
            <h6><span>チケット</span></h6><p>2027年7月1日</p>
            <h6>開催日時</h6><p>2027年9月16日</p>
        "#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-09-15", "2027-09-16"]);
}

#[test]
fn extract_event_dates_negative_keyword_expansion_suppresses_streaming_period() {
    let extractor = DateExtractor::new();

    let html = r#"
            <h6>開催日時</h6><p>2027年3月1日</p>
            <h6>配信期間</h6><p>2027年3月1日～2027年4月30日</p>
        "#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-03-01"]);
}

#[test]
fn extract_event_dates_ignores_invalid_dates() {
    let extractor = DateExtractor::new();

    let html = r#"<h6>開催日時</h6><p>2027年2月30日</p><p>2027年3月1日</p>"#;
    let dates = extractor.extract_event_dates(html);

    assert_eq!(date_strings(&dates), vec!["2027-03-01"]);
}

#[test]
fn extract_event_dates_real_html_supernova_fixture() {
    let extractor = DateExtractor::new();

    let fixture_path = format!(
        "{}/testdata/supernova_reboot_real.html",
        env!("CARGO_MANIFEST_DIR")
    );
    let html = std::fs::read_to_string(fixture_path).expect("fixture should be readable");

    let dates = extractor.extract_event_dates(&html);
    assert!(!dates.is_empty(), "fixture should yield at least one date");
    assert_eq!(
        dates[0].format("%Y-%m-%d").to_string(),
        "2026-02-21",
        "event date should be selected before ticket/archive dates"
    );
}
