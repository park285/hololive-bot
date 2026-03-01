use std::{
    collections::{BTreeSet, HashSet},
    sync::LazyLock,
};

use aho_corasick::{AhoCorasick, AhoCorasickBuilder};
use chrono::NaiveDate;
use regex::Regex;
use unicode_normalization::UnicodeNormalization;

static JAPANESE_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})年(\d{1,2})月(\d{1,2})日").expect("valid regex"));

static SLASH_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})[/\-](\d{1,2})[/\-](\d{1,2})").expect("valid regex"));

static DOT_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})\.\s*(\d{1,2})\.(\d{1,2})").expect("valid regex"));

static SHORT_JAPANESE_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{1,2})月(\d{1,2})日").expect("valid regex"));

static MULTI_DAY_RANGE_PATTERN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(\d{4})年(\d{1,2})月(\d{1,2})日(?:\([^)]*\)|（[^）]*）)?\s*[～〜~\-]\s*(?:(\d{1,2})月)?(\d{1,2})日",
    )
    .expect("valid regex")
});

static HTML_TAG_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"<[^>]+>").expect("valid regex"));

static SCRIPT_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<script[^>]*>.*?</script>").expect("valid regex"));

static STYLE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<style[^>]*>.*?</style>").expect("valid regex"));

static SECTION_HEADER_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<h[456][^>]*>(.*?)</h[456]>").expect("valid regex"));

const CLUSTER_GAP: usize = 150;
const MAX_CONTEXT_DISTANCE: usize = 150;
const TIE_THRESHOLD: usize = 10;

const POSITIVE_KEYWORDS: &[&str] = &[
    "開催",
    "日時",
    "日程",
    "公演",
    "開演",
    "会期",
    "期間",
    "ライブ",
    "コンサート",
    "開場",
    "ステージ",
];

const NEGATIVE_KEYWORDS: &[&str] = &[
    "チケット",
    "先行",
    "抽選",
    "受付",
    "販売",
    "発売",
    "申込",
    "締切",
    "アーカイブ",
    "予約",
    "募集",
    "応募",
    "視聴期限",
    "購入",
    "配信期間",
];

const STRONG_POSITIVE_KEYWORDS: &[&str] = &["開催日時", "開催日程", "公演日時"];

static POSITIVE_KEYWORD_MATCHER: LazyLock<AhoCorasick> = LazyLock::new(|| {
    AhoCorasickBuilder::new()
        .ascii_case_insensitive(true)
        .build(POSITIVE_KEYWORDS)
        .expect("valid positive keyword matcher")
});

static NEGATIVE_KEYWORD_MATCHER: LazyLock<AhoCorasick> = LazyLock::new(|| {
    AhoCorasickBuilder::new()
        .ascii_case_insensitive(true)
        .build(NEGATIVE_KEYWORDS)
        .expect("valid negative keyword matcher")
});

static STRONG_POSITIVE_KEYWORD_MATCHER: LazyLock<AhoCorasick> = LazyLock::new(|| {
    AhoCorasickBuilder::new()
        .ascii_case_insensitive(true)
        .build(STRONG_POSITIVE_KEYWORDS)
        .expect("valid strong positive keyword matcher")
});

#[derive(Debug, Clone)]
struct HeaderInfo {
    text: String,
}

#[derive(Debug, Clone)]
struct SectionRange {
    id: usize,
    start_pos: usize,
    end_pos: usize,
    header: String,
}

#[derive(Debug, Clone)]
struct DateMatch {
    date: NaiveDate,
    position: usize,
    score: i32,
}

#[derive(Debug, Clone, Copy, Default)]
pub struct DateExtractor;

impl DateExtractor {
    pub fn new() -> Self {
        Self
    }

    pub fn extract_unique_dates(&self, raw_text: &str) -> Vec<NaiveDate> {
        self.extract_event_dates(raw_text)
    }

    pub fn extract(&self, html: &str) -> Vec<NaiveDate> {
        let normalized = html.nfkc().collect::<String>();
        let mut dates = BTreeSet::new();

        collect_simple_pattern_dates(&normalized, &JAPANESE_DATE_PATTERN, &mut dates);
        collect_simple_pattern_dates(&normalized, &SLASH_DATE_PATTERN, &mut dates);
        collect_simple_pattern_dates(&normalized, &DOT_DATE_PATTERN, &mut dates);

        dates.into_iter().collect()
    }

    pub fn extract_with_context(&self, html: &str, context_year: i32) -> Vec<NaiveDate> {
        let mut dates = self.extract(html);
        let mut seen = dates
            .iter()
            .map(|date| date.format("%Y-%m-%d").to_string())
            .collect::<HashSet<_>>();

        for captures in SHORT_JAPANESE_DATE_PATTERN.captures_iter(html) {
            let Some(month) = parse_capture_part(&captures, 1) else {
                continue;
            };
            let Some(day) = parse_capture_part(&captures, 2) else {
                continue;
            };

            let Some(date) = NaiveDate::from_ymd_opt(context_year, month, day) else {
                continue;
            };

            let key = date.format("%Y-%m-%d").to_string();
            if seen.insert(key) {
                dates.push(date);
            }
        }

        dates.sort_unstable();
        dates
    }

    pub fn extract_event_dates(&self, html: &str) -> Vec<NaiveDate> {
        let normalized_html = html.nfkc().collect::<String>();

        let headers = extract_headers(&normalized_html);
        let plain = self.strip_html(&normalized_html);
        let sections = build_section_map(&plain, &headers);

        let mut all_matches = self.extract_all_matches(&plain);
        if all_matches.is_empty() {
            return Vec::new();
        }

        for matched in &mut all_matches {
            if let Some(section_id) = find_section_for_position(&sections, matched.position) {
                if let Some(section) = sections.get(section_id) {
                    let section_text = &plain[section.start_pos..section.end_pos];
                    matched.score = self.calculate_section_context_score(
                        section_text,
                        matched.position.saturating_sub(section.start_pos),
                    ) + section_header_bonus(&section.header);
                }
            } else {
                matched.score = self.calculate_context_score(&plain, matched.position);
            }
        }

        let positive_matches = filter_positive_matches(&all_matches);
        let selected_matches = if !positive_matches.is_empty() {
            self.select_best_cluster(&positive_matches)
        } else {
            let non_negative_matches = filter_non_negative_matches(&all_matches);
            if !non_negative_matches.is_empty() {
                self.select_best_cluster(&non_negative_matches)
            } else {
                self.select_best_cluster(&all_matches)
            }
        };

        let mut dates = extract_unique_dates_from_matches(&selected_matches);
        dates.sort_unstable();
        dates
    }

    fn strip_html(&self, html: &str) -> String {
        let without_script = SCRIPT_PATTERN.replace_all(html, " ");
        let sanitized = STYLE_PATTERN.replace_all(&without_script, " ").to_string();
        let document = ::scraper::Html::parse_document(&sanitized);
        let extracted = document.root_element().text().collect::<Vec<_>>().join(" ");

        let mut plain = if extracted.trim().is_empty() {
            HTML_TAG_PATTERN.replace_all(&sanitized, " ").to_string()
        } else {
            extracted
        };

        plain = plain.replace("&nbsp;", " ");
        plain = plain.replace("&amp;", "&");
        plain = plain.replace("&lt;", "<");
        plain = plain.replace("&gt;", ">");

        plain
    }

    fn extract_all_matches(&self, plain: &str) -> Vec<DateMatch> {
        let mut matches = Vec::new();
        let mut seen = HashSet::new();
        let mut multi_ranges = Vec::new();

        for captures in MULTI_DAY_RANGE_PATTERN.captures_iter(plain) {
            let Some(full_match) = captures.get(0) else {
                continue;
            };

            let Some(year) = parse_capture_year(&captures, 1) else {
                continue;
            };
            let Some(start_month) = parse_capture_part(&captures, 2) else {
                continue;
            };
            let Some(start_day) = parse_capture_part(&captures, 3) else {
                continue;
            };
            let end_month = parse_capture_part(&captures, 4).unwrap_or(start_month);
            let Some(end_day) = parse_capture_part(&captures, 5) else {
                continue;
            };

            let end_year = if end_month < start_month {
                year.saturating_add(1)
            } else {
                year
            };

            let Some(start_date) = NaiveDate::from_ymd_opt(year, start_month, start_day) else {
                continue;
            };
            let Some(end_date) = NaiveDate::from_ymd_opt(end_year, end_month, end_day) else {
                continue;
            };

            for date in [start_date, end_date] {
                let dedupe_key = make_dedupe_key(date, full_match.start());
                if seen.insert(dedupe_key) {
                    matches.push(DateMatch {
                        date,
                        position: full_match.start(),
                        score: 0,
                    });
                }
            }

            multi_ranges.push((full_match.start(), full_match.end()));
        }

        self.collect_ymd_matches(
            plain,
            &JAPANESE_DATE_PATTERN,
            Some(&multi_ranges),
            &mut seen,
            &mut matches,
        );
        self.collect_ymd_matches(plain, &SLASH_DATE_PATTERN, None, &mut seen, &mut matches);
        self.collect_ymd_matches(plain, &DOT_DATE_PATTERN, None, &mut seen, &mut matches);

        matches
    }

    fn collect_ymd_matches(
        &self,
        plain: &str,
        pattern: &Regex,
        skip_overlap: Option<&[(usize, usize)]>,
        seen: &mut HashSet<String>,
        matches: &mut Vec<DateMatch>,
    ) {
        for captures in pattern.captures_iter(plain) {
            let Some(full_match) = captures.get(0) else {
                continue;
            };

            if skip_overlap.is_some_and(|ranges| is_position_covered(full_match.start(), ranges)) {
                continue;
            }

            let Some(date) = parse_capture_ymd(&captures, 1, 2, 3) else {
                continue;
            };

            let dedupe_key = make_dedupe_key(date, full_match.start());
            if seen.insert(dedupe_key) {
                matches.push(DateMatch {
                    date,
                    position: full_match.start(),
                    score: 0,
                });
            }
        }
    }

    fn calculate_context_score(&self, plain: &str, position: usize) -> i32 {
        let plain_lower = plain.to_lowercase();
        let pos_distance =
            self.find_nearest_keyword_distance(&plain_lower, position, &POSITIVE_KEYWORD_MATCHER);
        let neg_distance =
            self.find_nearest_keyword_distance(&plain_lower, position, &NEGATIVE_KEYWORD_MATCHER);

        score_from_distances(pos_distance, neg_distance)
    }

    fn calculate_section_context_score(&self, section_text: &str, relative_pos: usize) -> i32 {
        let section_lower = section_text.to_lowercase();
        let pos_distance = self.find_nearest_keyword_distance(
            &section_lower,
            relative_pos,
            &POSITIVE_KEYWORD_MATCHER,
        );
        let neg_distance = self.find_nearest_keyword_distance(
            &section_lower,
            relative_pos,
            &NEGATIVE_KEYWORD_MATCHER,
        );

        score_from_distances(pos_distance, neg_distance)
    }

    fn find_nearest_keyword_distance(
        &self,
        plain_lower: &str,
        position: usize,
        matcher: &AhoCorasick,
    ) -> Option<usize> {
        let mut min_distance: Option<usize> = None;

        for found in matcher.find_iter(plain_lower) {
            let distance = position.abs_diff(found.start());
            min_distance = Some(match min_distance {
                Some(current) => current.min(distance),
                None => distance,
            });
        }

        min_distance
    }

    fn select_best_cluster(&self, matches: &[DateMatch]) -> Vec<DateMatch> {
        if matches.is_empty() {
            return Vec::new();
        }

        let mut sorted = matches.to_vec();
        sorted.sort_unstable_by_key(|item| item.position);

        let mut clusters: Vec<Vec<DateMatch>> = Vec::new();
        let mut current_cluster = vec![sorted[0].clone()];

        for window in sorted.windows(2) {
            let previous = &window[0];
            let current = &window[1];

            if current.position.saturating_sub(previous.position) <= CLUSTER_GAP {
                current_cluster.push(current.clone());
            } else {
                clusters.push(current_cluster);
                current_cluster = vec![current.clone()];
            }
        }
        clusters.push(current_cluster);

        let mut best_cluster = clusters[0].clone();
        let mut best_score = cluster_score(&best_cluster);

        for cluster in clusters.into_iter().skip(1) {
            let score = cluster_score(&cluster);
            let is_better =
                score > best_score || (score == best_score && cluster.len() > best_cluster.len());

            if is_better {
                best_score = score;
                best_cluster = cluster;
            }
        }

        best_cluster
    }
}

mod helpers;
use helpers::{
    build_section_map, cluster_score, collect_simple_pattern_dates, extract_headers,
    extract_unique_dates_from_matches, filter_non_negative_matches, filter_positive_matches,
    find_section_for_position, is_position_covered, make_dedupe_key, parse_capture_part,
    parse_capture_year, parse_capture_ymd, score_from_distances, section_header_bonus,
};

#[cfg(test)]
mod tests;
