# Phase 1: MajorEvent Scraper

> [← 메인 계획서](IMPLEMENTATION_PLAN.md)

## 1. Crate 구조 및 모듈 매핑

| Go 파일 (majorevent/) | 줄 수 | Rust 모듈 (crate) | 예상 줄 수 |
|------------------------|-------|-------------------|-----------|
| `domain/major_event.go` | 170 | `scraper-core/model.rs` | ~150 |
| `rss_parser.go` | 92 | `scraper-service/rss_parser.rs` | ~100 |
| `date_extractor.go` | 577 | `scraper-service/date_extractor.rs` | ~550 |
| `scraper.go` | 695 | `scraper-service/scraper.rs` | ~600 |
| `link_checker.go` | 320 | `scraper-service/link_checker.rs` | ~300 |
| `scraper_scheduler.go` | 300 | `scraper-service/scheduler.rs` | ~280 |
| `repository.go` (scraper 관련) | ~250 | `scraper-infra/repository.rs` | ~220 |

## 2. 도메인 모델 (scraper-core)

**파일**: `crates/scraper/core/src/model.rs`

```rust
use chrono::{DateTime, NaiveDate, Utc};
use serde::{Deserialize, Serialize};

/// 대형 행사 상태
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, sqlx::Type)]
#[sqlx(type_name = "VARCHAR", rename_all = "lowercase")]
pub enum MajorEventStatus {
    Active,
    Ended,
    Canceled,
}

impl MajorEventStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Active => "active",
            Self::Ended => "ended",
            Self::Canceled => "canceled",
        }
    }
}

impl std::fmt::Display for MajorEventStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// 행사/뉴스 유형
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, sqlx::Type)]
#[sqlx(type_name = "VARCHAR", rename_all = "lowercase")]
pub enum MajorEventType {
    Event,
    News,
}

impl MajorEventType {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Event => "event",
            Self::News => "news",
        }
    }
}

impl std::fmt::Display for MajorEventType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// 링크 검증 상태
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, sqlx::Type)]
#[sqlx(type_name = "VARCHAR", rename_all = "lowercase")]
pub enum MajorEventLinkStatus {
    Unchecked,
    Ok,
    Failed,
    Blocked,
}

impl MajorEventLinkStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Unchecked => "unchecked",
            Self::Ok => "ok",
            Self::Failed => "failed",
            Self::Blocked => "blocked",
        }
    }
}

impl std::fmt::Display for MajorEventLinkStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// 홀로라이브 대형 행사/뉴스 정보
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MajorEvent {
    pub id: i32,
    pub external_id: String,
    pub event_type: MajorEventType,
    pub title: String,
    pub link: String,
    pub description: Option<String>,
    pub members: Vec<String>,
    pub pub_date: Option<DateTime<Utc>>,
    pub event_start_date: Option<NaiveDate>,
    pub event_end_date: Option<NaiveDate>,
    /// 파싱 시 임시 저장 (DB 미저장)
    #[serde(skip)]
    pub event_dates: Vec<NaiveDate>,
    pub status: MajorEventStatus,
    pub link_status: MajorEventLinkStatus,
    pub link_checked_at: Option<DateTime<Utc>>,
    pub notified_at: Option<DateTime<Utc>>,
    pub notified_week: Option<String>,
    pub notified_month: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl MajorEvent {
    /// 파싱된 event_dates를 event_start_date/event_end_date로 변환
    pub fn set_event_dates_from_parsed(&mut self) {
        if self.event_dates.is_empty() {
            return;
        }
        self.event_dates.sort();
        self.event_start_date = Some(self.event_dates[0]);
        self.event_end_date = Some(*self.event_dates.last().unwrap());
    }

    /// start_date 없으면 pub_date로 fallback
    pub fn apply_fallback_event_date(&mut self) {
        if self.event_start_date.is_some() {
            if self.event_end_date.is_none() {
                self.event_end_date = self.event_start_date;
            }
            return;
        }
        if let Some(pub_date) = self.pub_date {
            let date = pub_date.date_naive();
            self.event_start_date = Some(date);
            self.event_end_date = Some(date);
        }
    }
}

/// DB에서 조회 시 사용하는 row 타입 (sqlx::FromRow)
#[derive(Debug, sqlx::FromRow)]
pub struct MajorEventRow {
    pub id: i32,
    pub external_id: String,
    #[sqlx(rename = "type")]
    pub event_type: String,
    pub title: String,
    pub link: String,
    pub description: Option<String>,
    pub members: Option<Vec<String>>,
    pub pub_date: Option<DateTime<Utc>>,
    pub event_start_date: Option<NaiveDate>,
    pub event_end_date: Option<NaiveDate>,
    pub status: String,
    pub link_status: String,
    pub link_checked_at: Option<DateTime<Utc>>,
    pub notified_at: Option<DateTime<Utc>>,
    pub notified_week: Option<String>,
    pub notified_month: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<MajorEventRow> for MajorEvent {
    fn from(row: MajorEventRow) -> Self {
        Self {
            id: row.id,
            external_id: row.external_id,
            event_type: match row.event_type.as_str() {
                "news" => MajorEventType::News,
                _ => MajorEventType::Event,
            },
            title: row.title,
            link: row.link,
            description: row.description,
            members: row.members.unwrap_or_default(),
            pub_date: row.pub_date,
            event_start_date: row.event_start_date,
            event_end_date: row.event_end_date,
            event_dates: Vec::new(),
            status: match row.status.as_str() {
                "ended" => MajorEventStatus::Ended,
                "canceled" => MajorEventStatus::Canceled,
                _ => MajorEventStatus::Active,
            },
            link_status: match row.link_status.as_str() {
                "ok" => MajorEventLinkStatus::Ok,
                "failed" => MajorEventLinkStatus::Failed,
                "blocked" => MajorEventLinkStatus::Blocked,
                _ => MajorEventLinkStatus::Unchecked,
            },
            link_checked_at: row.link_checked_at,
            notified_at: row.notified_at,
            notified_week: row.notified_week,
            notified_month: row.notified_month,
            created_at: row.created_at,
            updated_at: row.updated_at,
        }
    }
}
```

**파일**: `crates/scraper/core/src/error.rs`

```rust
use thiserror::Error;

#[derive(Error, Debug)]
pub enum ScraperError {
    #[error("HTTP request failed: {0}")]
    Http(#[from] reqwest::Error),

    #[error("HTTP status {code}: {message}")]
    HttpStatus { code: u16, message: String },

    #[error("XML parse failed: {0}")]
    XmlParse(String),

    #[error("Database error: {0}")]
    Database(#[from] sqlx::Error),

    #[error("Config error: {0}")]
    Config(String),

    #[error("All feeds failed: {0}")]
    AllFeedsFailed(String),

    #[error("Link check blocked: {0}")]
    LinkBlocked(String),

    #[error("Link check failed: {0}")]
    LinkFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// HTTP 상태 에러가 재시도 가능한지 판별
impl ScraperError {
    pub fn is_retryable(&self) -> bool {
        match self {
            Self::HttpStatus { code, .. } => matches!(code, 502 | 503 | 504),
            Self::Http(e) => {
                if e.is_timeout() || e.is_connect() {
                    return true;
                }
                let msg = e.to_string().to_lowercase();
                is_transient_signature(&msg)
            }
            _ => false,
        }
    }
}

/// 일시 네트워크 에러 시그니처 (9개 패턴, Go 동치)
fn is_transient_signature(msg: &str) -> bool {
    const PATTERNS: &[&str] = &[
        "connection reset by peer",
        "connection reset",
        "connection refused",
        "broken pipe",
        "http2: timeout awaiting response headers",
        "timeout exceeded while awaiting headers",
        "client.timeout exceeded while awaiting headers",
        "client.timeout exceeded",
        "unexpected eof",
    ];
    PATTERNS.iter().any(|p| msg.contains(p))
}
```

## 3. RSS Parser (scraper-service)

**파일**: `crates/scraper/service/src/rss_parser.rs`

```rust
use chrono::{DateTime, Utc};
use quick_xml::de::from_str;
use serde::Deserialize;

use scraper_core::error::ScraperError;
use scraper_core::model::{
    MajorEvent, MajorEventLinkStatus, MajorEventStatus, MajorEventType,
};

/// RSS 피드 루트
#[derive(Debug, Deserialize)]
struct RssFeed {
    channel: RssChannel,
}

/// RSS 채널
#[derive(Debug, Deserialize)]
struct RssChannel {
    #[serde(default)]
    item: Vec<RssItem>,
}

/// RSS 아이템
#[derive(Debug, Deserialize)]
struct RssItem {
    #[serde(default)]
    title: String,
    #[serde(default)]
    link: String,
    #[serde(default, rename = "pubDate")]
    pub_date: String,
    #[serde(default, rename = "category")]
    categories: Vec<String>,
    /// content:encoded (namespace: http://purl.org/rss/1.0/modules/content/)
    #[serde(default, rename = "encoded")]
    description: String,
}

/// RSS 파서 (stateless)
pub struct RssParser;

impl RssParser {
    pub fn new() -> Self {
        Self
    }

    /// RSS XML 데이터를 지정된 타입으로 파싱
    pub fn parse(
        &self,
        data: &[u8],
        event_type: MajorEventType,
    ) -> Result<Vec<MajorEvent>, ScraperError> {
        let xml_str = std::str::from_utf8(data)
            .map_err(|e| ScraperError::XmlParse(e.to_string()))?;

        let feed: RssFeed = from_str(xml_str)
            .map_err(|e| ScraperError::XmlParse(e.to_string()))?;

        let events = feed
            .channel
            .item
            .into_iter()
            .map(|item| {
                let pub_date = parse_pub_date(&item.pub_date);
                MajorEvent {
                    id: 0,
                    external_id: item.link.clone(),
                    event_type: event_type.clone(),
                    title: item.title,
                    link: item.link,
                    description: if item.description.is_empty() {
                        None
                    } else {
                        Some(item.description)
                    },
                    members: item.categories,
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
                    created_at: Utc::now(),
                    updated_at: Utc::now(),
                }
            })
            .collect();

        Ok(events)
    }
}

/// PubDate 파싱 (RFC1123Z, RFC1123, 2개 변형 포맷)
fn parse_pub_date(date_str: &str) -> Option<DateTime<Utc>> {
    const FORMATS: &[&str] = &[
        "%a, %d %b %Y %H:%M:%S %z",   // RFC1123Z
        "%a, %d %b %Y %H:%M:%S %Z",   // RFC1123
        "%a, %d %b %Y %H:%M:%S %z",   // "Mon, 02 Jan 2006 15:04:05 -0700"
        "%a, %-d %b %Y %H:%M:%S %z",  // "Mon, 2 Jan 2006 15:04:05 -0700"
    ];

    for fmt in FORMATS {
        if let Ok(dt) = DateTime::parse_from_str(date_str, fmt) {
            return Some(dt.with_timezone(&Utc));
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    // Go rss_parser_test.go 6개 테스트 포팅 대상:
    // 1. TestRSSParser_Parse_BasicFeed
    // 2. TestRSSParser_Parse_EmptyFeed
    // 3. TestRSSParser_Parse_InvalidXML
    // 4. TestRSSParser_Parse_MissingPubDate
    // 5. TestRSSParser_ParseWithType_News
    // 6. TestRSSParser_Parse_Categories
}
```

**content:encoded 네임스페이스 처리 주의**:
quick-xml의 serde deserializer는 네임스페이스 프리픽스를 기본적으로 무시한다.
`content:encoded` 필드는 `rename = "encoded"`로 매핑하되, quick-xml config에서 네임스페이스 처리를 확인해야 한다.
필요 시 raw XML 파싱으로 fallback한다.

## 4. DateExtractor (scraper-service)

**CRITICAL**: 이 모듈이 전체 마이그레이션에서 가장 높은 복잡도와 파싱 불일치 리스크를 가진다.

**파일**: `crates/scraper/service/src/date_extractor.rs`

### 4.1 Regex 패턴 (Rust 문법)

```rust
use std::sync::LazyLock;
use regex::Regex;

static JAPANESE_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})年(\d{1,2})月(\d{1,2})日").unwrap());

static SLASH_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})[/\-](\d{1,2})[/\-](\d{1,2})").unwrap());

static DOT_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})\.\s*(\d{1,2})\.(\d{1,2})").unwrap());

static SHORT_JAPANESE_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{1,2})月(\d{1,2})日").unwrap());

/// 멀티데이: 2026年3月6日~8日, 2026年3月6日(金)~8日(日), 2026年3月6日~3月8日
/// NFKC 정규화 후 U+FF5E(~) -> U+007E(~)로 변환되므로 ASCII ~ 포함
static MULTI_DAY_RANGE_PATTERN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(\d{4})年(\d{1,2})月(\d{1,2})日(?:\([^)]*\)|（[^）]*）)?\s*[～〜~\-]\s*(?:(\d{1,2})月)?(\d{1,2})日"
    )
    .unwrap()
});

static HTML_TAG_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"<[^>]+>").unwrap());

static SCRIPT_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<script[^>]*>.*?</script>").unwrap());

static STYLE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<style[^>]*>.*?</style>").unwrap());

/// 섹션 헤더 패턴 (h4-h6)
static SECTION_HEADER_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)<h[456][^>]*>(.*?)</h[456]>").unwrap());
```

### 4.2 상수

```rust
const CLUSTER_GAP: usize = 150;
const MAX_CONTEXT_DISTANCE: usize = 150;
const TIE_THRESHOLD: usize = 10;
```

### 4.3 키워드 리스트 (exact)

```rust
const POSITIVE_KEYWORDS: &[&str] = &[
    "開催", "日時", "日程", "公演", "開演", "会期", "期間",
    "ライブ", "コンサート", "開場", "ステージ",
];

const NEGATIVE_KEYWORDS: &[&str] = &[
    "チケット", "先行", "抽選", "受付", "販売", "発売",
    "申込", "締切", "アーカイブ", "予約", "募集", "応募",
    "視聴期限", "購入", "配信期間",
];

/// 강력 긍정 키워드: 섹션 헤더 보너스 판별
const STRONG_POSITIVE_KEYWORDS: &[&str] = &[
    "開催日時", "開催日程", "公演日時",
];
```

### 4.4 내부 타입

```rust
use chrono::NaiveDate;

/// HTML 헤더 정보
struct HeaderInfo {
    text: String,
}

/// plain text 좌표계 기반 섹션 범위
struct SectionRange {
    id: usize,
    start_pos: usize,
    end_pos: usize,
    header: String,
}

/// 날짜 매칭 결과
#[derive(Clone)]
struct DateMatch {
    date: NaiveDate,
    position: usize,
    raw: String,
    score: i32,
}
```

### 4.5 알고리즘 (step-by-step)

```rust
use unicode_normalization::UnicodeNormalization;

pub struct DateExtractor;

impl DateExtractor {
    pub fn new() -> Self {
        Self
    }

    /// 메인 진입점: HTML에서 이벤트 날짜 추출
    pub fn extract_event_dates(&self, html: &str) -> Vec<NaiveDate> {
        // Step 0: Unicode NFKC 정규화
        let normalized: String = html.nfkc().collect();

        // Step 1: HTML에서 헤더 추출 (strip 전)
        let headers = extract_headers(&normalized);

        // Step 2: HTML -> plain text
        let plain = strip_html(&normalized);

        // Step 3: plain text에서 섹션 매핑
        let sections = build_section_map(&plain, &headers);

        // Step 4: 모든 날짜 매칭 추출
        let mut all_matches = extract_all_matches(&plain);
        if all_matches.is_empty() {
            return Vec::new();
        }

        // Step 5: 섹션 ID 할당 + context score 계산
        for m in &mut all_matches {
            let section_id = find_section_for_position(&sections, m.position);
            if let Some(section) = sections.get(section_id) {
                let section_text = &plain[section.start_pos..section.end_pos];
                let relative_pos = m.position.saturating_sub(section.start_pos);
                m.score = calculate_section_context_score(section_text, relative_pos);
                m.score += section_header_bonus(&section.header);
            } else {
                m.score = calculate_context_score(&plain, m.position);
            }
        }

        // Step 6: positive -> best cluster 선택
        let positive_matches: Vec<_> =
            all_matches.iter().filter(|m| m.score > 0).cloned().collect();

        let selected = if !positive_matches.is_empty() {
            select_best_cluster(&positive_matches)
        } else {
            // non-negative fallback
            let non_negative: Vec<_> =
                all_matches.iter().filter(|m| m.score >= 0).cloned().collect();
            if !non_negative.is_empty() {
                select_best_cluster(&non_negative)
            } else {
                select_best_cluster(&all_matches)
            }
        };

        // Step 7: 중복 제거 + 정렬
        let mut dates = extract_unique_dates(&selected);
        dates.sort();
        dates
    }
}
```

### 4.6 Scoring 로직

```rust
/// 섹션 범위 내 키워드 거리 기반 scoring
fn calculate_section_context_score(section_text: &str, relative_pos: usize) -> i32 {
    let text_lower = section_text.to_lowercase();
    let pos_distance = find_nearest_keyword_distance(&text_lower, relative_pos, POSITIVE_KEYWORDS);
    let neg_distance = find_nearest_keyword_distance(&text_lower, relative_pos, NEGATIVE_KEYWORDS);

    score_from_distances(pos_distance, neg_distance)
}

/// 글로벌 (섹션 미할당) fallback scoring
fn calculate_context_score(plain: &str, position: usize) -> i32 {
    let plain_lower = plain.to_lowercase();
    let pos_distance = find_nearest_keyword_distance(&plain_lower, position, POSITIVE_KEYWORDS);
    let neg_distance = find_nearest_keyword_distance(&plain_lower, position, NEGATIVE_KEYWORDS);

    score_from_distances(pos_distance, neg_distance)
}

fn score_from_distances(pos_distance: Option<usize>, neg_distance: Option<usize>) -> i32 {
    let pos = pos_distance.filter(|&d| d <= MAX_CONTEXT_DISTANCE);
    let neg = neg_distance.filter(|&d| d <= MAX_CONTEXT_DISTANCE);

    match (pos, neg) {
        (None, None) => 0,
        (None, Some(_)) => -3,
        (Some(_), None) => 2,
        (Some(p), Some(n)) => {
            let diff = if p > n { p - n } else { n - p };
            if diff <= TIE_THRESHOLD {
                0
            } else if p < n {
                2
            } else {
                -3
            }
        }
    }
}

/// 섹션 헤더 보너스: strong positive +5, negative -5
fn section_header_bonus(header: &str) -> i32 {
    let header_lower = header.to_lowercase();
    for kw in STRONG_POSITIVE_KEYWORDS {
        if header_lower.contains(&kw.to_lowercase()) {
            return 5;
        }
    }
    for kw in NEGATIVE_KEYWORDS {
        if header_lower.contains(&kw.to_lowercase()) {
            return -5;
        }
    }
    0
}

/// 가장 가까운 키워드까지의 거리 (byte offset 기준)
fn find_nearest_keyword_distance(
    text_lower: &str,
    position: usize,
    keywords: &[&str],
) -> Option<usize> {
    let mut min_distance: Option<usize> = None;

    for kw in keywords {
        let kw_lower = kw.to_lowercase();
        let mut start = 0;
        while let Some(idx) = text_lower[start..].find(&kw_lower) {
            let kw_pos = start + idx;
            let distance = if position > kw_pos {
                position - kw_pos
            } else {
                kw_pos - position
            };
            min_distance = Some(min_distance.map_or(distance, |d: usize| d.min(distance)));
            start = kw_pos + kw_lower.len();
        }
    }

    min_distance
}
```

### 4.7 테스트 계획

Go `date_extractor_test.go`에서 포팅할 18개 테스트:

| # | 테스트명 | 내용 |
|---|---------|------|
| 1 | basic_japanese_date | `2026年3月6日` 단일 날짜 |
| 2 | slash_date_format | `2026/03/06` |
| 3 | dot_date_format | `2026.03.06` |
| 4 | multi_day_range | `2026年3月6日～8日` |
| 5 | multi_day_cross_month | `2026年3月30日～4月2日` |
| 6 | multi_day_with_weekday | `2026年3月6日（金）～8日（日）` |
| 7 | positive_keyword_scoring | "開催日時" 근처 날짜 우선 |
| 8 | negative_keyword_filtering | "チケット販売" 근처 날짜 감점 |
| 9 | section_header_bonus | `<h4>開催日時</h4>` 섹션 +5 보너스 |
| 10 | section_header_negative | `<h5>チケット</h5>` 섹션 -5 페널티 |
| 11 | cluster_selection | 여러 클러스터 중 최고 score 선택 |
| 12 | dedup_dates | 동일 날짜 중복 제거 |
| 13 | nfkc_normalization | 호환 문자 정규화 |
| 14 | empty_input | 빈 입력 |
| 15 | no_dates_found | 날짜 없는 HTML |
| 16 | real_html_supernova_reboot | `testdata/supernova_reboot_real.html` 실제 페이지 |
| 17 | short_japanese_date_with_context | `3月6日` (연도 컨텍스트) |
| 18 | invalid_date_values | `13月45日` 등 무효 날짜 |

**testdata 공유**: Go `testdata/` 디렉토리의 파일을 `crates/scraper/service/testdata/`에 복사한다.

**리스크 검증 방법**: Go와 Rust 양쪽에 동일 입력을 주어 출력을 비교하는 cross-validation 스크립트를 작성한다 (Section 5 참조).

## 5. Scraper (scraper-service)

**파일**: `crates/scraper/service/src/scraper.rs`

### 5.1 Struct 정의

```rust
use std::collections::{HashMap, HashSet};
use std::sync::Arc;
use tokio::sync::RwLock;

use chrono::{DateTime, Utc};
use reqwest::Client;

use scraper_core::model::{MajorEvent, MajorEventType};
use scraper_core::error::ScraperError;
use crate::date_extractor::DateExtractor;
use crate::rss_parser::RssParser;
use scraper_infra::repository::Repository;

/// RSS Feed 조건부 요청 메타데이터
#[derive(Debug, Clone, Default)]
struct FeedMetadata {
    etag: Option<String>,
    last_modified: Option<String>,
}

/// Incremental scan 커서
struct IncrementalCursor {
    known_external_ids: HashSet<String>,
    known_canonical_links: HashSet<String>,
    latest_pub_date: Option<DateTime<Utc>>,
}

/// 스크래핑 설정 상수 (Go MajorEventConfig 동치)
pub struct ScraperConfig {
    pub event_rss_url: String,
    pub news_rss_urls: Vec<String>,
    pub max_retries: u32,
    pub retry_delay: std::time::Duration,
    pub max_pages: u32,
    pub incremental_cursor_limit: i64,
    pub page_delay: std::time::Duration,
    pub user_agent: String,
    pub max_response_body_bytes: usize,
}

impl Default for ScraperConfig {
    fn default() -> Self {
        Self {
            event_rss_url: "https://hololive.hololivepro.com/events/feed/".into(),
            news_rss_urls: vec![
                "https://hololive.hololivepro.com/news/feed/".into(),
                "https://hololive.hololivepro.com/en/news/feed/".into(),
            ],
            max_retries: 4,
            retry_delay: std::time::Duration::from_secs(1),
            max_pages: 20,
            incremental_cursor_limit: 200,
            page_delay: std::time::Duration::from_secs(2),
            user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36".into(),
            max_response_body_bytes: 2 * 1024 * 1024, // 2 MiB
        }
    }
}

pub struct Scraper {
    http_client: Client,
    rss_parser: RssParser,
    date_extractor: DateExtractor,
    repository: Arc<Repository>,
    config: ScraperConfig,
    feed_metadata: Arc<RwLock<HashMap<String, FeedMetadata>>>,
}
```

### 5.2 ScrapeAndStore 플로우 (async)

```rust
impl Scraper {
    pub async fn scrape_and_store(&self) -> Result<usize, ScraperError> {
        // 1. Feed source 목록 구성 (event + news_ja + news_en)
        // 2. 각 source에 대해 scrape_all_pages 호출
        // 3. 전체 실패 시 AllFeedsFailed 에러
        // 4. dedup_by_canonical_link
        // 5. 각 event에 set_event_dates_from_parsed + apply_fallback_event_date
        // 6. repository.upsert_event 순회
        // 7. stored 카운트 반환
        todo!()
    }

    async fn scrape_all_pages(
        &self,
        base_url: &str,
        event_type: MajorEventType,
    ) -> Result<(Vec<MajorEvent>, Vec<u32>), ScraperError> {
        // 1. load_incremental_cursor (repository에서 recent external IDs)
        // 2. 1차 패스: page 1..=max_pages
        //    - scrapePage (with retry)
        //    - page 1 실패 -> 즉시 반환
        //    - 연속 3회 실패 -> pagination 종료
        //    - shouldStopIncrementalScan 체크
        //    - page_delay (tokio::time::sleep)
        // 3. backfill_failed_pages (실패 페이지 1회 재시도)
        // 4. (events, skipped_pages) 반환
        todo!()
    }
}
```

### 5.3 Incremental Cursor 로직

```rust
/// 페이지의 모든 이벤트가 이미 알려진 것인지 판별
fn should_stop_incremental_scan(
    events: &[MajorEvent],
    cursor: Option<&IncrementalCursor>,
) -> bool {
    let cursor = match cursor {
        Some(c) if !c.known_external_ids.is_empty()
            || !c.known_canonical_links.is_empty()
            || c.latest_pub_date.is_some() => c,
        _ => return false,
    };

    if events.is_empty() {
        return false;
    }

    let mut has_known_signal = false;

    for event in events {
        let known_by_external_id = cursor
            .known_external_ids
            .contains(event.external_id.trim());

        let canonical_key = canonical_event_link_key(&event.link)
            .or_else(|| canonical_event_link_key(&event.external_id));
        let known_by_canonical_link = canonical_key
            .as_deref()
            .is_some_and(|k| cursor.known_canonical_links.contains(k));

        let known_by_pub_date = match (cursor.latest_pub_date, event.pub_date) {
            (Some(latest), Some(pub_date)) => pub_date < latest,
            _ => false,
        };

        if !known_by_external_id && !known_by_canonical_link && !known_by_pub_date {
            return false;
        }
        has_known_signal = true;
    }

    has_known_signal
}
```

### 5.4 Conditional HTTP Headers

```rust
async fn scrape_page_once(
    &self,
    base_url: &str,
    page: u32,
    event_type: MajorEventType,
) -> Result<Vec<MajorEvent>, ScraperError> {
    let page_url = if page > 1 {
        format!("{}?paged={}", base_url, page)
    } else {
        base_url.to_string()
    };

    let mut request = self.http_client
        .get(&page_url)
        .header("User-Agent", &self.config.user_agent);

    // Conditional headers (ETag, Last-Modified)
    {
        let metadata = self.feed_metadata.read().await;
        if let Some(meta) = metadata.get(&page_url) {
            if let Some(etag) = &meta.etag {
                request = request.header("If-None-Match", etag);
            }
            if let Some(lm) = &meta.last_modified {
                request = request.header("If-Modified-Since", lm);
            }
        }
    }

    let resp = request.send().await?;

    match resp.status().as_u16() {
        304 => return Ok(Vec::new()),  // Not Modified
        404 => return Ok(Vec::new()),  // No more pages
        200 => { /* continue */ }
        code => {
            return Err(ScraperError::HttpStatus {
                code,
                message: format!("page {}", page),
            });
        }
    }

    // Save ETag/Last-Modified
    let etag = resp.headers().get("ETag").and_then(|v| v.to_str().ok().map(String::from));
    let last_modified = resp.headers().get("Last-Modified").and_then(|v| v.to_str().ok().map(String::from));
    if etag.is_some() || last_modified.is_some() {
        let mut metadata = self.feed_metadata.write().await;
        let entry = metadata.entry(page_url).or_default();
        if let Some(e) = etag { entry.etag = Some(e); }
        if let Some(lm) = last_modified { entry.last_modified = Some(lm); }
    }

    // Read body (size limited)
    let body = resp.bytes().await?;
    if body.len() > self.config.max_response_body_bytes {
        return Err(ScraperError::Http(
            reqwest::Error::from(std::io::Error::new(
                std::io::ErrorKind::Other,
                "response body too large"
            ))
        ));
    }

    // Parse RSS + extract dates
    let mut events = self.rss_parser.parse(&body, event_type)?;
    for event in &mut events {
        if let Some(desc) = &event.description {
            event.event_dates = self.date_extractor.extract_event_dates(desc);
        }
    }

    Ok(events)
}
```

### 5.5 Retry 로직

```rust
/// retry with exponential backoff + jitter
async fn scrape_page(
    &self,
    base_url: &str,
    page: u32,
    event_type: MajorEventType,
) -> Result<Vec<MajorEvent>, ScraperError> {
    let mut last_err = None;

    for attempt in 0..self.config.max_retries {
        match self.scrape_page_once(base_url, page, event_type.clone()).await {
            Ok(events) => return Ok(events),
            Err(e) if e.is_retryable() && attempt + 1 < self.config.max_retries => {
                tracing::debug!(
                    attempt = attempt + 1,
                    page,
                    error = %e,
                    "scrape page retry"
                );
                let jitter = self.config.retry_delay / 2;
                let delay = self.config.retry_delay
                    + std::time::Duration::from_millis(
                        rand::random::<u64>() % jitter.as_millis() as u64
                    );
                tokio::time::sleep(delay).await;
                last_err = Some(e);
            }
            Err(e) => return Err(e),
        }
    }

    Err(last_err.unwrap_or_else(|| {
        ScraperError::HttpStatus { code: 0, message: "exhausted retries".into() }
    }))
}
```

### 5.6 Dedup 로직

```rust
/// URL의 canonical key 추출 (Go canonicalEventLinkKey 동치)
fn canonical_event_link_key(raw: &str) -> Option<String> {
    let link = raw.trim();
    if link.is_empty() {
        return None;
    }

    let parsed = url::Url::parse(link).ok()?;
    let host = parsed.host_str()?.to_lowercase();
    let mut path = parsed.path().trim().to_string();
    if path.is_empty() {
        path = "/".into();
    }

    // hololive.hololivepro.com: /en/ 접두사 제거
    if host == "hololive.hololivepro.com" {
        if let Some(stripped) = path.strip_prefix("/en/") {
            path = format!("/{}", stripped);
        }
    }

    // 후행 슬래시 제거
    if path.len() > 1 && path.ends_with('/') {
        path.pop();
    }

    Some(format!("{}{}", host, path))
}

fn dedup_events_by_canonical_link(events: Vec<MajorEvent>) -> Vec<MajorEvent> {
    if events.len() <= 1 {
        return events;
    }

    let mut seen = HashSet::new();
    let mut deduped = Vec::with_capacity(events.len());

    for event in events {
        let key = canonical_event_link_key(&event.link)
            .unwrap_or_else(|| event.external_id.clone());
        if key.is_empty() || seen.insert(key) {
            deduped.push(event);
        }
    }

    deduped
}
```

## 6. Repository (scraper-infra)

**파일**: `crates/scraper/infra/src/repository.rs`

### 6.1 Pool 설정

```rust
use sqlx::postgres::{PgConnectOptions, PgPoolOptions};
use sqlx::PgPool;

use crate::config::DatabaseConfig;

pub async fn create_pool(config: &DatabaseConfig) -> Result<PgPool, sqlx::Error> {
    let options = PgConnectOptions::new()
        .host(&config.host)
        .port(config.port)
        .database(&config.name)
        .username(&config.user)
        .password(&config.password)
        .ssl_mode(sqlx::postgres::PgSslMode::Disable);

    PgPoolOptions::new()
        .max_connections(config.max_connections)
        .min_connections(1)
        .acquire_timeout(std::time::Duration::from_secs(5))
        .idle_timeout(std::time::Duration::from_secs(300))
        .connect_with(options)
        .await
}
```

### 6.2 UpsertEvent

```rust
use scraper_core::model::{MajorEvent, MajorEventType, MajorEventLinkStatus};
use chrono::{DateTime, NaiveDate, Utc};

pub struct Repository {
    pool: PgPool,
}

impl Repository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }

    /// 이벤트 upsert (external_id 기준, Go UpsertEvent 동치)
    pub async fn upsert_event(&self, event: &MajorEvent) -> Result<i32, sqlx::Error> {
        let event_type = event.event_type.as_str();
        let link_status = event.link_status.as_str();
        let status = event.status.as_str();

        let row: (i32,) = sqlx::query_as(
            r#"
            INSERT INTO major_events
                (external_id, type, title, link, description, members,
                 pub_date, event_start_date, event_end_date, status, link_status)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
            ON CONFLICT (external_id) DO UPDATE
            SET title = EXCLUDED.title,
                link = EXCLUDED.link,
                description = EXCLUDED.description,
                members = EXCLUDED.members,
                pub_date = EXCLUDED.pub_date,
                event_start_date = EXCLUDED.event_start_date,
                event_end_date = EXCLUDED.event_end_date,
                type = EXCLUDED.type,
                status = CASE
                    WHEN major_events.status = 'canceled' THEN major_events.status
                    WHEN major_events.status = 'ended'
                         AND EXCLUDED.event_start_date >= CURRENT_DATE THEN 'active'
                    ELSE major_events.status
                END,
                link_status = CASE
                    WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN 'unchecked'
                    ELSE major_events.link_status
                END,
                link_checked_at = CASE
                    WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN NULL
                    ELSE major_events.link_checked_at
                END,
                updated_at = NOW()
            RETURNING id
            "#,
        )
        .bind(&event.external_id)
        .bind(event_type)
        .bind(&event.title)
        .bind(&event.link)
        .bind(&event.description)
        .bind(&event.members)
        .bind(event.pub_date)
        .bind(event.event_start_date)
        .bind(event.event_end_date)
        .bind(status)
        .bind(link_status)
        .fetch_one(&self.pool)
        .await?;

        Ok(row.0)
    }
```

### 6.3 GetRecentExternalIDs

```rust
    /// 최근 external_id + latest pub_date 조회
    pub async fn get_recent_external_ids(
        &self,
        event_type: MajorEventType,
        limit: i64,
    ) -> Result<(Vec<String>, Option<DateTime<Utc>>), sqlx::Error> {
        let limit = if limit <= 0 { 1 } else { limit };

        let rows: Vec<(String, Option<DateTime<Utc>>)> = sqlx::query_as(
            r#"
            SELECT external_id, pub_date
            FROM major_events
            WHERE type = $1
            ORDER BY pub_date DESC NULLS LAST, updated_at DESC
            LIMIT $2
            "#,
        )
        .bind(event_type.as_str())
        .bind(limit)
        .fetch_all(&self.pool)
        .await?;

        let mut external_ids = Vec::with_capacity(rows.len());
        let mut latest_pub_date = None;

        for (external_id, pub_date) in rows {
            if !external_id.is_empty() {
                external_ids.push(external_id);
            }
            if latest_pub_date.is_none() {
                latest_pub_date = pub_date;
            }
        }

        Ok((external_ids, latest_pub_date))
    }
```

### 6.4 UpdateExpiredEvents

```rust
    /// 종료된 이벤트 상태 업데이트
    pub async fn update_expired_events(&self) -> Result<u64, sqlx::Error> {
        let result = sqlx::query(
            r#"
            UPDATE major_events
            SET status = 'ended',
                updated_at = NOW()
            WHERE status = 'active'
              AND (
                event_end_date < CURRENT_DATE
                OR (event_end_date IS NULL AND event_start_date < CURRENT_DATE)
              )
            "#,
        )
        .execute(&self.pool)
        .await?;

        Ok(result.rows_affected())
    }
}
```

## 7. ScraperScheduler (scraper-service)

**파일**: `crates/scraper/service/src/scheduler.rs`

```rust
use std::sync::Mutex;

use chrono::{DateTime, Duration, FixedOffset, Timelike, Utc};
use tokio_util::sync::CancellationToken;

use crate::scraper::Scraper;
use crate::link_checker::LinkChecker;
use scraper_infra::repository::Repository;

const KST_OFFSET: i32 = 9 * 3600; // +09:00

/// 스크래핑 트리거 유형
#[derive(Debug, Clone, Copy, PartialEq)]
enum ScrapeTrigger {
    Regular,
    Retry,
}

/// 스케줄러 설정
pub struct SchedulerConfig {
    pub scrape_hour_kst: u32,
    pub retry_delays: Vec<Duration>,
}

impl Default for SchedulerConfig {
    fn default() -> Self {
        Self {
            scrape_hour_kst: 6,
            retry_delays: vec![
                Duration::minutes(30),
                Duration::hours(2),
                Duration::hours(6),
            ],
        }
    }
}

pub struct ScraperScheduler {
    scraper: Scraper,
    repository: Repository,
    link_checker: LinkChecker,
    config: SchedulerConfig,
    retry_runs: Mutex<Vec<DateTime<Utc>>>,
    cancel_token: CancellationToken,
}

impl ScraperScheduler {
    pub fn new(
        scraper: Scraper,
        repository: Repository,
        link_checker: LinkChecker,
        config: SchedulerConfig,
        cancel_token: CancellationToken,
    ) -> Self {
        Self {
            scraper,
            repository,
            link_checker,
            config,
            retry_runs: Mutex::new(Vec::new()),
            cancel_token,
        }
    }

    /// 메인 스케줄러 루프
    pub async fn run(&self) {
        loop {
            let (next_run, trigger) = self.next_run_with_trigger(Utc::now());
            let wait_duration = (next_run - Utc::now())
                .to_std()
                .unwrap_or(std::time::Duration::ZERO);

            tracing::info!(
                next_run = %next_run,
                trigger = ?trigger,
                wait_ms = wait_duration.as_millis(),
                "scheduler waiting"
            );

            tokio::select! {
                _ = tokio::time::sleep(wait_duration) => {
                    if trigger == ScrapeTrigger::Retry {
                        self.pop_next_retry_run();
                    }

                    let scrape_err = self.run_scrape().await;
                    self.handle_scrape_result(trigger, next_run, Utc::now(), scrape_err);
                }
                _ = self.cancel_token.cancelled() => {
                    tracing::info!("scheduler stopped by cancellation");
                    return;
                }
            }
        }
    }

    async fn run_scrape(&self) -> Option<String> {
        // 1. update_expired_events
        match self.repository.update_expired_events().await {
            Ok(n) if n > 0 => tracing::info!(count = n, "updated expired events"),
            Err(e) => tracing::error!(error = %e, "failed to update expired events"),
            _ => {}
        }

        // 2. scrape_and_store
        let scrape_result = self.scraper.scrape_and_store().await;
        let scrape_err = match scrape_result {
            Ok(stored) => {
                tracing::info!(stored, "scrape completed");
                None
            }
            Err(e) => {
                tracing::error!(error = %e, "scrape failed");
                Some(e.to_string())
            }
        };

        // 3. check_stale_links
        match self.link_checker.check_stale_links().await {
            Ok(result) if result.checked > 0 => {
                tracing::info!(
                    checked = result.checked,
                    ok = result.ok,
                    failed = result.failed,
                    blocked = result.blocked,
                    "link check completed"
                );
            }
            Err(e) => tracing::error!(error = %e, "link check failed"),
            _ => {}
        }

        scrape_err
    }

    /// 다음 실행 시간 계산 (regular vs retry)
    fn next_run_with_trigger(&self, now: DateTime<Utc>) -> (DateTime<Utc>, ScrapeTrigger) {
        let next_regular = self.calculate_next_regular_run(now);

        if let Some(&next_retry) = self.retry_runs.lock().unwrap().first() {
            if next_retry <= next_regular {
                return (next_retry, ScrapeTrigger::Retry);
            }
        }

        (next_regular, ScrapeTrigger::Regular)
    }

    /// 다음 정규 실행: daily HH:00 KST
    fn calculate_next_regular_run(&self, now: DateTime<Utc>) -> DateTime<Utc> {
        let kst = FixedOffset::east_opt(KST_OFFSET).unwrap();
        let now_kst = now.with_timezone(&kst);

        let today_target = now_kst
            .date_naive()
            .and_hms_opt(self.config.scrape_hour_kst, 0, 0)
            .unwrap()
            .and_local_timezone(kst)
            .unwrap();

        if today_target > now_kst {
            today_target.with_timezone(&Utc)
        } else {
            (today_target + Duration::days(1)).with_timezone(&Utc)
        }
    }

    /// 실패 시 retry 큐 생성 (같은 날만)
    fn build_retry_runs(
        &self,
        base_run: DateTime<Utc>,
        failed_at: DateTime<Utc>,
    ) -> Vec<DateTime<Utc>> {
        let kst = FixedOffset::east_opt(KST_OFFSET).unwrap();
        let base_kst = base_run.with_timezone(&kst);
        let failed_kst = failed_at.with_timezone(&kst);

        self.config
            .retry_delays
            .iter()
            .filter_map(|delay| {
                let candidate = base_kst + *delay;
                // 같은 날짜에만 retry
                if candidate.date_naive() != base_kst.date_naive() {
                    return None;
                }
                // 이미 지난 시간이면 skip
                if candidate <= failed_kst {
                    return None;
                }
                Some(candidate.with_timezone(&Utc))
            })
            .collect()
    }

    fn handle_scrape_result(
        &self,
        trigger: ScrapeTrigger,
        scheduled_at: DateTime<Utc>,
        completed_at: DateTime<Utc>,
        scrape_err: Option<String>,
    ) {
        match scrape_err {
            None => {
                // 성공: retry 큐 비우기
                let cleared = {
                    let mut runs = self.retry_runs.lock().unwrap();
                    let n = runs.len();
                    runs.clear();
                    n
                };
                if cleared > 0 {
                    tracing::info!(cleared, "scrape succeeded; cleared retry queue");
                }
            }
            Some(err) if trigger == ScrapeTrigger::Regular => {
                let retries = self.build_retry_runs(scheduled_at, completed_at);
                let count = retries.len();
                *self.retry_runs.lock().unwrap() = retries;
                tracing::warn!(
                    retry_count = count,
                    error = %err,
                    "scheduled scrape failed; retry queue updated"
                );
            }
            Some(err) => {
                let remaining = self.retry_runs.lock().unwrap().len();
                tracing::warn!(
                    remaining,
                    error = %err,
                    "retry scrape failed"
                );
            }
        }
    }

    fn pop_next_retry_run(&self) -> Option<DateTime<Utc>> {
        let mut runs = self.retry_runs.lock().unwrap();
        if runs.is_empty() {
            None
        } else {
            Some(runs.remove(0))
        }
    }
}
```

## 8. Configuration (scraper-infra)

**파일**: `crates/scraper/infra/src/config.rs`

```rust
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct AppConfig {
    pub database: DatabaseConfig,
    pub proxy: ProxyConfig,
    pub scheduler: SchedulerConfig,
    pub scraper: ScraperAppConfig,
    pub health: HealthConfig,
}

#[derive(Debug, Deserialize)]
pub struct DatabaseConfig {
    pub host: String,
    pub port: u16,
    pub name: String,
    pub user: String,
    pub password: String,
    #[serde(default = "default_sslmode")]
    pub sslmode: String,
    #[serde(default = "default_max_connections")]
    pub max_connections: u32,
}

fn default_sslmode() -> String { "disable".into() }
fn default_max_connections() -> u32 { 5 }

#[derive(Debug, Deserialize)]
pub struct ProxyConfig {
    pub socks5_url: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct SchedulerConfig {
    #[serde(default = "default_scrape_hour")]
    pub scrape_hour_kst: u32,
}

fn default_scrape_hour() -> u32 { 6 }

#[derive(Debug, Deserialize)]
pub struct ScraperAppConfig {
    #[serde(default = "default_user_agent")]
    pub user_agent: String,
}

fn default_user_agent() -> String {
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36".into()
}

#[derive(Debug, Deserialize)]
pub struct HealthConfig {
    #[serde(default = "default_health_port")]
    pub port: u16,
}

fn default_health_port() -> u16 { 30010 }

impl AppConfig {
    pub fn load(path: &str) -> Result<Self, config::ConfigError> {
        let cfg = config::Config::builder()
            .add_source(config::File::with_name(path).required(false))
            .add_source(
                config::Environment::with_prefix("SCRAPER")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?;
        cfg.try_deserialize()
    }
}
```

**런타임 설정 파일**: `hololive-scraper-rs/config.toml`

```toml
[database]
host = "holo-postgres"
port = 5432
name = "hololive"
user = "hololive_scraper"
password = ""  # 환경변수 SCRAPER__DATABASE__PASSWORD로 override
sslmode = "disable"
max_connections = 5

[proxy]
socks5_url = "socks5://vpn-scraper-proxy:1080"

[scheduler]
scrape_hour_kst = 6

[scraper]
user_agent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

[health]
port = 30010
```

> 환경변수 override: `SCRAPER__DATABASE__HOST`, `SCRAPER__PROXY__SOCKS5_URL` 등 `SCRAPER__` 접두사 + `__` 구분자로 모든 설정 override 가능.

## 9. Application Entry Point (scraper-app)

**파일**: `crates/scraper/app/src/main.rs`

```rust
use std::sync::Arc;
use std::net::SocketAddr;

use axum::{Router, Json, routing::get};
use clap::Parser;
use serde::Serialize;
use tokio_util::sync::CancellationToken;
use tracing_subscriber::EnvFilter;

use scraper_infra::config::AppConfig;
use scraper_infra::repository::{self, Repository};
use scraper_service::scraper::{Scraper, ScraperConfig};
use scraper_service::link_checker::LinkChecker;
use scraper_service::scheduler::{ScraperScheduler, SchedulerConfig};

#[derive(Parser)]
#[command(name = "hololive-scraper-rs")]
struct Cli {
    /// Config file path
    #[arg(long, default_value = "config.toml")]
    config: String,
}

#[derive(Serialize)]
struct HealthResponse {
    status: &'static str,
    version: &'static str,
    db_connected: bool,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // 1. tracing 초기화
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .json()
        .init();

    // 2. config 로드 (TOML + 환경변수 override)
    let cli = Cli::parse();
    let config = AppConfig::load(&cli.config)?;

    // 3. DB pool 생성
    let pool = repository::create_pool(&config.database).await?;
    tracing::info!("database connected");

    let repository = Arc::new(Repository::new(pool.clone()));

    // 4. reqwest client (SOCKS5 proxy)
    let http_client = build_http_client(&config)?;

    // 5. 컴포넌트 조립
    let scraper_config = ScraperConfig::default();
    let scraper = Scraper::new(
        http_client.clone(),
        repository.clone(),
        scraper_config,
    );

    let link_checker = LinkChecker::new(
        http_client,
        repository.clone(),
    );

    let cancel_token = CancellationToken::new();

    let scheduler = ScraperScheduler::new(
        scraper,
        (*repository).clone(),
        link_checker,
        SchedulerConfig::default(),
        cancel_token.clone(),
    );

    // 6. Health endpoint (axum)
    let pool_health = pool.clone();
    let app = Router::new().route(
        "/health",
        get(move || async move {
            let db_ok = pool_health.acquire().await.is_ok();
            let status = if db_ok { "ok" } else { "degraded" };
            Json(HealthResponse {
                status,
                version: env!("CARGO_PKG_VERSION"),
                db_connected: db_ok,
            })
        }),
    );

    let addr = SocketAddr::from(([0, 0, 0, 0], config.health.port));
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("health endpoint listening on {}", addr);

    // 7. Graceful shutdown
    let cancel_for_shutdown = cancel_token.clone();
    tokio::spawn(async move {
        tokio::signal::ctrl_c().await.ok();
        tracing::info!("shutdown signal received");
        cancel_for_shutdown.cancel();
    });

    // 8. 병렬 실행: scheduler + health server
    tokio::select! {
        _ = scheduler.run() => {
            tracing::info!("scheduler exited");
        }
        result = axum::serve(listener, app) => {
            if let Err(e) = result {
                tracing::error!(error = %e, "health server error");
            }
        }
        _ = cancel_token.cancelled() => {
            tracing::info!("application shutting down");
        }
    }

    // 9. DB pool 정리
    pool.close().await;
    tracing::info!("shutdown complete");

    Ok(())
}

fn build_http_client(config: &AppConfig) -> anyhow::Result<reqwest::Client> {
    let mut builder = reqwest::Client::builder()
        .user_agent(&config.scraper.user_agent)
        .timeout(std::time::Duration::from_secs(30))
        .connect_timeout(std::time::Duration::from_secs(10));

    if let Some(proxy_url) = &config.proxy.socks5_url {
        let proxy = reqwest::Proxy::all(proxy_url)?;
        builder = builder.proxy(proxy);
    }

    Ok(builder.build()?)
}
```

## 10. Phase 1 체크리스트

- [ ] `scraper-core/model.rs` -- `MajorEvent`, `MajorEventStatus`, `MajorEventType`, `MajorEventLinkStatus` 정의
- [ ] `scraper-core/error.rs` -- `ScraperError` + `is_retryable()` + 9개 transient 패턴
- [ ] `scraper-service/rss_parser.rs` -- `quick-xml` + `serde` deserialization
- [ ] `scraper-service/rss_parser.rs` -- `content:encoded` 네임스페이스 핸들링 검증
- [ ] `scraper-service/rss_parser.rs` -- 6개 테스트 포팅 (`testdata/events_feed.xml`)
- [ ] `scraper-service/date_extractor.rs` -- 7개 regex 패턴 (Rust 문법)
- [ ] `scraper-service/date_extractor.rs` -- NFKC 정규화 (`unicode-normalization`)
- [ ] `scraper-service/date_extractor.rs` -- 헤더 추출 + 섹션 매핑
- [ ] `scraper-service/date_extractor.rs` -- context scoring (positive/negative/strong)
- [ ] `scraper-service/date_extractor.rs` -- cluster selection (gap=150)
- [ ] `scraper-service/date_extractor.rs` -- 18개 테스트 포팅 (`testdata/supernova_reboot_real.html` 포함)
- [ ] `scraper-service/scraper.rs` -- `Scraper` struct + `ScraperConfig`
- [ ] `scraper-service/scraper.rs` -- `scrape_and_store()` 비동기 파이프라인
- [ ] `scraper-service/scraper.rs` -- incremental cursor 로직
- [ ] `scraper-service/scraper.rs` -- conditional HTTP headers (ETag, Last-Modified)
- [ ] `scraper-service/scraper.rs` -- retry (최대 4회, jitter)
- [ ] `scraper-service/scraper.rs` -- backfill (실패 페이지 1회 재시도)
- [ ] `scraper-service/scraper.rs` -- `canonical_event_link_key` + dedup
- [ ] `scraper-service/scraper.rs` -- 연속 3회 실패 시 pagination 중단
- [ ] `scraper-infra/repository.rs` -- `create_pool()` (TCP)
- [ ] `scraper-infra/repository.rs` -- `upsert_event()` (status state machine 포함)
- [ ] `scraper-infra/repository.rs` -- `get_recent_external_ids()`
- [ ] `scraper-infra/repository.rs` -- `update_expired_events()`
- [ ] `scraper-service/scheduler.rs` -- tokio 기반 스케줄러 루프
- [ ] `scraper-service/scheduler.rs` -- KST 06:00 daily 정규 실행
- [ ] `scraper-service/scheduler.rs` -- retry 큐 ([30m, 2h, 6h], 같은 날만)
- [ ] `scraper-service/scheduler.rs` -- 성공 시 retry 큐 클리어
- [ ] `scraper-service/scheduler.rs` -- `CancellationToken` graceful shutdown
- [ ] `scraper-infra/config.rs` -- `AppConfig` + TOML + env override (`SCRAPER__` 접두사)
- [ ] `config.toml` -- 런타임 설정 파일 (기본값 + docker-compose override)
- [ ] `scraper-app/main.rs` -- Config 로드 (TOML + env)
- [ ] `scraper-app/main.rs` -- reqwest client (SOCKS5 proxy 지원)
- [ ] `scraper-app/main.rs` -- Health endpoint (`GET /health` :30010)
- [ ] `scraper-app/main.rs` -- SIGTERM/SIGINT shutdown 처리
- [ ] `cargo test --all` 전체 통과
- [ ] `cargo clippy` 0 warnings
- [ ] `docker compose build hololive-scraper` 성공
- [ ] 실제 DB 연결 후 `scrape_and_store()` 1회 수동 실행 검증

## 11. 검증 계획

### 11.1 Go vs Rust 출력 비교 전략

1. **단위 테스트 동치성**: 동일 testdata 입력에 대해 Go/Rust 출력이 일치하는지 파일별 검증
2. **DateExtractor cross-validation**:
   - Go에서 `date_extractor_test.go`의 모든 입력/기대값을 JSON fixture로 추출
   - Rust에서 동일 JSON fixture를 읽어 결과 비교
   - CI에서 자동 실행
3. **DB snapshot 비교**:
   - Go scraper 실행 후 `major_events` 테이블 덤프
   - Rust scraper 실행 후 동일 덤프
   - `external_id`, `event_start_date`, `event_end_date`, `status` 컬럼 diff
4. **Dual-run 모드** (권장):
   - Phase 1 완료 후 임시로 Go/Rust 양쪽 실행 (서로 다른 시간)
   - upsert이므로 양쪽이 동일 external_id를 업데이트해도 안전
   - 2~3일간 diff 없으면 Go scraper 비활성화
