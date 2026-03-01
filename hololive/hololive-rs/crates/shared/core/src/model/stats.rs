use std::sync::OnceLock;

use chrono::{DateTime, Duration, Months, Utc};
use regex::Regex;
use serde::{Deserialize, Serialize};

static STATS_NUMERIC_PATTERN: OnceLock<Option<Regex>> = OnceLock::new();

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum StatsPeriod {
    Today,
    Week,
    Month,
    Quarter,
    Year,
    Days(i64),
    Weeks(i64),
    Months(i64),
    Quarters(i64),
    Years(i64),
    Hours(i64),
}

impl StatsPeriod {
    pub fn from_normalized(token: &str) -> Option<Self> {
        match token {
            "today" => Some(Self::Today),
            "week" => Some(Self::Week),
            "month" => Some(Self::Month),
            "quarter" => Some(Self::Quarter),
            "year" => Some(Self::Year),
            _ => parse_prefixed_period(token),
        }
    }

    pub fn start_at(&self, now: DateTime<Utc>) -> DateTime<Utc> {
        match self {
            Self::Today => now - Duration::hours(24),
            Self::Week => now - Duration::days(7),
            Self::Month => now
                .checked_sub_months(Months::new(1))
                .unwrap_or(now - Duration::days(30)),
            Self::Quarter => now
                .checked_sub_months(Months::new(3))
                .unwrap_or(now - Duration::days(90)),
            Self::Year => now
                .checked_sub_months(Months::new(12))
                .unwrap_or(now - Duration::days(365)),
            Self::Days(days) => now - Duration::days(*days),
            Self::Weeks(weeks) => now - Duration::days(7 * weeks),
            Self::Months(months) => now
                .checked_sub_months(Months::new((*months).try_into().unwrap_or(0)))
                .unwrap_or(now - Duration::days(30 * months)),
            Self::Quarters(quarters) => now
                .checked_sub_months(Months::new((3 * quarters).try_into().unwrap_or(0)))
                .unwrap_or(now - Duration::days(90 * quarters)),
            Self::Years(years) => now
                .checked_sub_months(Months::new((12 * years).try_into().unwrap_or(0)))
                .unwrap_or(now - Duration::days(365 * years)),
            Self::Hours(hours) => now - Duration::hours(*hours),
        }
    }

    pub fn label(&self) -> String {
        match self {
            Self::Today => "오늘".to_owned(),
            Self::Week => "최근 7일".to_owned(),
            Self::Month => "최근 1개월".to_owned(),
            Self::Quarter => "최근 1분기".to_owned(),
            Self::Year => "최근 1년".to_owned(),
            Self::Days(value) => format_relative_label(*value, "일"),
            Self::Weeks(value) => format_relative_label(*value, "주"),
            Self::Months(value) => format_relative_label(*value, "개월"),
            Self::Quarters(value) => format_relative_label(*value, "분기"),
            Self::Years(value) => format_relative_label(*value, "년"),
            Self::Hours(value) => format_relative_label(*value, "시간"),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TimestampedStats {
    pub channel_id: String,
    pub member_name: String,
    pub subscriber_count: u64,
    pub video_count: u64,
    pub view_count: u64,
    pub timestamp: DateTime<Utc>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum MilestoneType {
    #[serde(rename = "subscribers")]
    Subscribers,
    #[serde(rename = "videos")]
    Videos,
    #[serde(rename = "views")]
    Views,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Milestone {
    pub channel_id: String,
    pub member_name: String,
    pub r#type: MilestoneType,
    pub value: u64,
    pub achieved_at: DateTime<Utc>,
    pub notified: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StatsChange {
    pub channel_id: String,
    pub member_name: String,
    pub subscriber_change: i64,
    pub video_change: i64,
    pub view_change: i64,
    pub previous_stats: Option<TimestampedStats>,
    pub current_stats: Option<TimestampedStats>,
    pub detected_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DailySummary {
    pub date: DateTime<Utc>,
    pub total_changes: i32,
    pub milestones_achieved: i32,
    pub new_videos_detected: i32,
    pub top_gainers: Vec<RankEntry>,
    pub top_uploaders: Vec<RankEntry>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RankEntry {
    pub channel_id: String,
    pub member_name: String,
    pub value: i64,
    pub current_subscribers: u64,
    pub rank: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TrendData {
    pub channel_id: String,
    pub member_name: String,
    pub period: String,
    pub subscriber_growth: i64,
    pub video_upload_rate: f64,
    pub avg_views_per_video: u64,
    pub updated_at: DateTime<Utc>,
}

pub fn normalize_stats_period_token(raw: &str) -> String {
    let token = raw.trim();
    if token.is_empty() {
        return String::new();
    }

    let lower = token.trim().to_lowercase();

    for (prefix, unit) in [
        ("days:", "days"),
        ("weeks:", "weeks"),
        ("months:", "months"),
        ("quarters:", "quarters"),
        ("years:", "years"),
        ("hours:", "hours"),
    ] {
        if let Some(value) = lower.strip_prefix(prefix)
            && let Some(parsed) = parse_positive_int(value)
        {
            return format!("{unit}:{parsed}");
        }
    }

    if let Some(normalized) = match lower.as_str() {
        "오늘" | "today" => Some("today"),
        "주간" | "week" | "weekly" => Some("week"),
        "월간" | "month" | "monthly" => Some("month"),
        "분기" | "quarter" | "quarterly" => Some("quarter"),
        "연간" | "년간" | "year" | "yearly" | "annual" | "annually" => Some("year"),
        _ => None,
    } {
        return (*normalized).to_owned();
    }

    if let Some(regex) = stats_numeric_pattern()
        && let Some(captures) = regex.captures(token)
        && let (Some(value_match), Some(unit_match)) = (captures.get(1), captures.get(2))
        && let Some(value) = parse_positive_int(value_match.as_str())
        && let Some(unit) = normalize_period_unit(unit_match.as_str())
    {
        return format!("{unit}:{value}");
    }

    String::new()
}

pub fn resolve_stats_period(now: DateTime<Utc>, raw: &str) -> (DateTime<Utc>, String) {
    let normalized = normalize_stats_period_token(raw);
    let fallback = StatsPeriod::Days(10);

    let period = if normalized.is_empty() {
        fallback
    } else {
        StatsPeriod::from_normalized(&normalized).unwrap_or(fallback)
    };

    (period.start_at(now), period.label())
}

fn parse_prefixed_period(token: &str) -> Option<StatsPeriod> {
    for (prefix, ctor) in [
        ("days:", StatsPeriod::Days as fn(i64) -> StatsPeriod),
        ("weeks:", StatsPeriod::Weeks),
        ("months:", StatsPeriod::Months),
        ("quarters:", StatsPeriod::Quarters),
        ("years:", StatsPeriod::Years),
        ("hours:", StatsPeriod::Hours),
    ] {
        if let Some(raw) = token.strip_prefix(prefix)
            && let Some(value) = parse_positive_int(raw)
        {
            return Some(ctor(value));
        }
    }

    None
}

fn stats_numeric_pattern() -> Option<&'static Regex> {
    STATS_NUMERIC_PATTERN
        .get_or_init(|| {
            Regex::new(
                r"(?i)^(?:최근|last)?\s*(\d+)\s*(일|days?|d|주|weeks?|w|개월|달|months?|m|분기|quarters?|q|년|연|years?|y|시간|hours?|h)(?:\s*(?:간|동안))?$",
            )
            .ok()
        })
        .as_ref()
}

fn normalize_period_unit(raw: &str) -> Option<&'static str> {
    match raw.trim().to_lowercase().as_str() {
        "일" | "day" | "days" | "d" => Some("days"),
        "주" | "week" | "weeks" | "w" => Some("weeks"),
        "개월" | "달" | "month" | "months" | "m" => Some("months"),
        "분기" | "quarter" | "quarters" | "q" => Some("quarters"),
        "년" | "연" | "year" | "years" | "y" => Some("years"),
        "시간" | "hour" | "hours" | "h" => Some("hours"),
        _ => None,
    }
}

fn parse_positive_int(raw: &str) -> Option<i64> {
    raw.trim().parse::<i64>().ok().filter(|value| *value > 0)
}

fn format_relative_label(value: i64, unit: &str) -> String {
    if value == 1 {
        return format!("최근 1{unit}");
    }

    format!("최근 {value}{unit}")
}
